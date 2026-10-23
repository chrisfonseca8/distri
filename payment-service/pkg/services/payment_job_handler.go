package services

import (
	"context"
	"errors"
	"fmt"
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

type PaymentJobHandler struct {
	db             *gorm.DB
	gateway        paymentgateway.Gateway
	gatewayTimeout time.Duration
}

func NewPaymentJobHandler(
	db *gorm.DB,
	gateway paymentgateway.Gateway,
	gatewayTimeout time.Duration,
) *PaymentJobHandler {
	return &PaymentJobHandler{db: db, gateway: gateway, gatewayTimeout: gatewayTimeout}
}

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

	switch payment.Status {
	case "CREATED":
		return h.processCreated(ctx, payment)
	case "PENDING":
		if attempts <= 1 {
			return ProcessRetry, errors.New("payment is pending on its first job attempt")
		}
		if err := h.writeResult(payment.ID, map[string]any{
			"status":       "UNKNOWN",
			"failure_code": nil,
		}); err != nil {
			return ProcessRetry, err
		}
		return ProcessDone, nil
	case "SUCCEEDED", "FAILED", "UNKNOWN":
		return ProcessDone, nil
	default:
		return ProcessPermanent, fmt.Errorf("unknown payment status %q", payment.Status)
	}
}

func (h *PaymentJobHandler) processCreated(
	ctx context.Context,
	payment models.PaymentAttempt,
) (ProcessOutcome, error) {
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

	if err := h.writeResult(payment.ID, paymentResultUpdates(ref, gatewayErr)); err != nil {
		return ProcessRetry, err
	}
	return ProcessDone, nil
}

func (h *PaymentJobHandler) writeResult(
	paymentID uuid.UUID,
	updates map[string]any,
) error {
	writeContext, cancelWrite := context.WithTimeout(context.Background(), paymentResultWriteTimeout)
	defer cancelWrite()

	return h.db.WithContext(writeContext).Transaction(func(tx *gorm.DB) error {
		result := tx.Table("payments.payment_attempts").
			Where("id = ? AND status = ?", paymentID, "PENDING").
			Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("persist payment gateway result: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return nil
		}

		var payment models.PaymentAttempt
		if err := tx.Table("payments.payment_attempts").
			Where("id = ?", paymentID).
			Take(&payment).Error; err != nil {
			return fmt.Errorf("load payment result for reply: %w", err)
		}
		return enqueuePaymentReply(tx, payment)
	})
}

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

	return map[string]any{
		"status":       "UNKNOWN",
		"failure_code": nil,
	}
}
