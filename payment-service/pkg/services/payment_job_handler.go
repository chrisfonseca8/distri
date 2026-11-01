package services

import (
	"context"
	"errors"
	"fmt"
	"github.com/chrisfonseca08/distri/payment-service/pkg/observability"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/payment-service/database/models"
	"github.com/chrisfonseca08/distri/payment-service/pkg/payment_gateway"
	"gorm.io/gorm"
)

const paymentResultWriteTimeout = 5 * time.Second

type ProcessOutcome string

const (
	ProcessDone      ProcessOutcome = "Done"
	ProcessRetry     ProcessOutcome = "Retry"
	ProcessPermanent ProcessOutcome = "Permanent"
)

const defaultPaymentWindow = 5 * time.Minute

type PaymentJobHandler struct {
	db             *gorm.DB
	gateway        paymentgateway.Gateway
	gatewayTimeout time.Duration
	paymentWindow  time.Duration
	gatewayName    string
}

// JobHandlerOption customises a PaymentJobHandler.
type JobHandlerOption func(*PaymentJobHandler)

// WithPaymentWindow sets how long a payment may stay AWAITING_PAYMENT.
func WithPaymentWindow(window time.Duration) JobHandlerOption {
	return func(h *PaymentJobHandler) {
		if window > 0 {
			h.paymentWindow = window
		}
	}
}

// WithJobGatewayName makes the handler call its gateway only for attempts labelled
// with that environment. An attempt of another environment is never sent here: its
// own label decides where it may go, not the process that happened to claim the job.
func WithJobGatewayName(name string) JobHandlerOption {
	return func(h *PaymentJobHandler) { h.gatewayName = name }
}

// NewPaymentJobHandler creates the handler that charges the gateway for a payment job.
func NewPaymentJobHandler(
	db *gorm.DB,
	gateway paymentgateway.Gateway,
	gatewayTimeout time.Duration,
	options ...JobHandlerOption,
) *PaymentJobHandler {
	handler := &PaymentJobHandler{
		db:             db,
		gateway:        gateway,
		gatewayTimeout: gatewayTimeout,
		paymentWindow:  defaultPaymentWindow,
	}
	for _, option := range options {
		option(handler)
	}
	return handler
}

// ProcessForJob picks what to do with a payment based on its status and returns whether the job is done, should retry or is dead.
func (h *PaymentJobHandler) ProcessForJob(
	ctx context.Context,
	paymentID uuid.UUID,
	attempts int,
) (ProcessOutcome, error) {
	payment, err := h.loadPayment(ctx, paymentID)
	if err != nil {
		if errors.Is(err, ErrPaymentNotFound) {
			return ProcessPermanent, err
		}
		return ProcessRetry, err
	}

	ctx = observability.WithFields(ctx, observability.Fields{OrderID: payment.OrderID.String(), SagaID: payment.OrderID.String()})
	switch payment.Status {
	case "CREATED":
		return h.processCreated(ctx, payment)
	case "PENDING":
		if attempts <= 1 {
			return ProcessRetry, errors.New("payment is pending on its first job attempt")
		}
		if err := h.writeResult(ctx, payment.ID, map[string]any{
			"status":       "UNKNOWN",
			"failure_code": nil,
		}); err != nil {
			return ProcessRetry, err
		}
		return ProcessDone, nil
	case "AWAITING_PAYMENT", "SUCCEEDED", "FAILED", "UNKNOWN":
		return ProcessDone, nil
	default:
		return ProcessPermanent, fmt.Errorf("unknown payment status %q", payment.Status)
	}
}

// processCreated claims a CREATED payment, calls the gateway outside any transaction and stores the result.
func (h *PaymentJobHandler) processCreated(
	ctx context.Context,
	payment models.PaymentAttempt,
) (ProcessOutcome, error) {
	if h.gatewayName != "" && payment.Gateway != h.gatewayName {
		// Permanent, before the claim: the gateway is never called and the row stays CREATED,
		// so the dead-job path resolves it as "gateway never called".
		return ProcessPermanent, fmt.Errorf("payment %s belongs to gateway %q, this process serves %q",
			payment.ID, payment.Gateway, h.gatewayName)
	}
	claim := h.db.WithContext(ctx).
		Session(&gorm.Session{SkipDefaultTransaction: true}).
		Table("payments.payment_attempts").
		Where("id = ? AND status = ?", payment.ID, "CREATED").
		Update("status", "PENDING")
	if claim.Error != nil {
		return ProcessRetry, fmt.Errorf("claim payment for processing: %w", claim.Error)
	}
	if claim.RowsAffected != 1 {
		return ProcessRetry, errors.New("payment left CREATED before it could be claimed")
	}

	gatewayContext, cancelGateway := context.WithTimeout(ctx, h.gatewayTimeout)
	ref, gatewayErr := h.gateway.CreateCharge(
		gatewayContext,
		payment.OrderID,
		payment.AmountPaise,
	)
	cancelGateway()

	var notSent *paymentgateway.ErrNotSent
	if errors.As(gatewayErr, &notSent) {
		// The request never left this process, so nothing exists at the gateway and the
		// call is safe to try again: undo the claim and let the job retry with backoff.
		// If the gateway stays down the job dies after its attempts and the resolver
		// answers Failed for a payment that never reached it.
		revert := h.db.WithContext(context.Background()).
			Session(&gorm.Session{SkipDefaultTransaction: true}).
			Table("payments.payment_attempts").
			Where("id = ? AND status = ?", payment.ID, "PENDING").
			Update("status", "CREATED")
		if revert.Error != nil {
			// Left PENDING: the next claim turns it UNKNOWN and the reconciler looks it up.
			return ProcessRetry, fmt.Errorf("gateway unreachable and the claim could not be undone: %w", revert.Error)
		}
		return ProcessRetry, gatewayErr
	}

	updates := paymentResultUpdates(ref, gatewayErr)
	if updates["status"] == "AWAITING_PAYMENT" {
		updates["expires_at"] = time.Now().Add(h.paymentWindow)
	}
	if err := h.writeResult(ctx, payment.ID, updates); err != nil {
		return ProcessRetry, err
	}
	return ProcessDone, nil
}

// writeResult stores a payment's result with its own short timeout.
func (h *PaymentJobHandler) writeResult(
	ctx context.Context,
	paymentID uuid.UUID,
	updates map[string]any,
) error {
	// The write must survive a cancelled request context, so it starts from a fresh
	// one; the correlation ids and trace are carried over so the reply row keeps them.
	writeContext, cancelWrite := context.WithTimeout(context.Background(), paymentResultWriteTimeout)
	defer cancelWrite()
	writeContext = observability.ContextFromCorrelation(writeContext, observability.CorrelationFromContext(ctx))

	return h.db.WithContext(writeContext).Transaction(func(tx *gorm.DB) error {
		_, err := writePaymentResult(tx, paymentID, "PENDING", updates)
		return err
	})
}

// writePaymentResult is the single place that records a payment's result: the
// guarded status update and its reply.payment outbox row, in the caller's
// transaction. It reports whether this call changed the payment; a payment that
// is no longer in the from status was resolved by someone else and gets no
// second reply.
func writePaymentResult(
	tx *gorm.DB,
	paymentID uuid.UUID,
	from string,
	updates map[string]any,
) (bool, error) {
	result := tx.Table("payments.payment_attempts").
		Where("id = ? AND status = ?", paymentID, from).
		Updates(updates)
	if result.Error != nil {
		return false, fmt.Errorf("persist payment result: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return false, nil
	}

	var payment models.PaymentAttempt
	if err := tx.Table("payments.payment_attempts").
		Where("id = ?", paymentID).
		Take(&payment).Error; err != nil {
		return false, fmt.Errorf("load payment result for reply: %w", err)
	}
	// An attempt that only awaits the customer has no outcome to report yet.
	if payment.Status == "AWAITING_PAYMENT" {
		return true, nil
	}
	if err := enqueuePaymentReply(tx, payment); err != nil {
		return false, err
	}
	return true, nil
}

// loadPayment reads one payment attempt, returning ErrPaymentNotFound if it does not exist.
func (h *PaymentJobHandler) loadPayment(
	ctx context.Context,
	paymentID uuid.UUID,
) (models.PaymentAttempt, error) {
	var payment models.PaymentAttempt
	result := h.db.WithContext(ctx).
		Table("payments.payment_attempts").
		Where("id = ?", paymentID).
		First(&payment)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return models.PaymentAttempt{}, ErrPaymentNotFound
	}
	if result.Error != nil {
		return models.PaymentAttempt{}, fmt.Errorf("load payment for processing: %w", result.Error)
	}
	return payment, nil
}

// paymentResultUpdates maps the gateway's answer to the payment status and failure code to store.
func paymentResultUpdates(ref string, gatewayErr error) map[string]any {
	if gatewayErr == nil {
		return map[string]any{
			"status":           "SUCCEEDED",
			"gateway_order_id": ref,
			"failure_code":     nil,
		}
	}

	var declined *paymentgateway.ErrDeclined
	if errors.As(gatewayErr, &declined) {
		return map[string]any{
			"status":       "FAILED",
			"failure_code": "DECLINED",
		}
	}

	var awaiting *paymentgateway.ErrAwaitingPayment
	if errors.As(gatewayErr, &awaiting) {
		return map[string]any{
			"status":           "AWAITING_PAYMENT",
			"gateway_order_id": awaiting.Ref,
			"failure_code":     nil,
		}
	}

	var rejected *paymentgateway.ErrRejected
	if errors.As(gatewayErr, &rejected) {
		return map[string]any{
			"status":       "FAILED",
			"failure_code": "GATEWAY_REJECTED",
		}
	}

	return map[string]any{
		"status":       "UNKNOWN",
		"failure_code": nil,
	}
}
