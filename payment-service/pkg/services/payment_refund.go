package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chrisfonseca08/distri/payment-service/pkg/businessmetrics"
	"github.com/chrisfonseca08/distri/payment-service/pkg/observability"
	"log/slog"
	"strings"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/payment-service/database/models"
	"github.com/chrisfonseca08/distri/payment-service/pkg/messages"
	"github.com/chrisfonseca08/distri/payment-service/pkg/messaging"
	"github.com/chrisfonseca08/distri/payment-service/pkg/outbox"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PaymentCommandHandler routes the payment service's commands by routing key.
type PaymentCommandHandler struct {
	initiate *PaymentInitiateHandler
	refund   *PaymentRefundHandler
}

// NewPaymentCommandHandler creates the dispatcher for payment.initiate and payment.refund.
func NewPaymentCommandHandler(initiate *PaymentInitiateHandler, refund *PaymentRefundHandler) *PaymentCommandHandler {
	return &PaymentCommandHandler{initiate: initiate, refund: refund}
}

// Handle sends a command to the handler that owns its routing key.
func (h *PaymentCommandHandler) Handle(ctx context.Context, delivery messaging.Delivery) error {
	switch delivery.RoutingKey {
	case messages.PaymentInitiateRoutingKey:
		return h.initiate.Handle(ctx, delivery)
	case messages.PaymentRefundRoutingKey:
		return h.refund.Handle(ctx, delivery)
	default:
		return messaging.Permanent(fmt.Errorf("unsupported payment routing key %q", delivery.RoutingKey))
	}
}

// PaymentRefundHandler records the obligation to refund the payment that funded an order
// whose checkout could not be completed after the customer paid.
//
// The saga asks for this only after it has stayed stuck for the configured time. The
// handler does not call the gateway: it writes a refund record that the compensation
// worker then submits with the same safeguards as every other refund (an operation id
// stored first, lookup before any resend, bounded retries, review for a person). It
// answers once the obligation is durably recorded, not when the money has moved.
type PaymentRefundHandler struct {
	db *gorm.DB
}

// NewPaymentRefundHandler creates the handler for payment.refund commands.
func NewPaymentRefundHandler(db *gorm.DB) *PaymentRefundHandler {
	return &PaymentRefundHandler{db: db}
}

// Handle records the refund obligation for an order, once per message and once per payment.
func (h *PaymentRefundHandler) Handle(ctx context.Context, delivery messaging.Delivery) error {
	if delivery.RoutingKey != messages.PaymentRefundRoutingKey {
		return messaging.Permanent(fmt.Errorf("unsupported payment routing key %q", delivery.RoutingKey))
	}
	var command messages.PaymentRefund
	if err := json.Unmarshal(delivery.Body, &command); err != nil {
		return messaging.Permanent(fmt.Errorf("decode payment.refund: %w", err))
	}
	if command.OrderID == uuid.Nil() {
		return messaging.Permanent(errors.New("payment.refund order_id is required"))
	}
	messageID := strings.TrimSpace(delivery.MessageId)
	if messageID == "" {
		return messaging.Permanent(errors.New("payment.refund AMQP MessageId is required"))
	}

	ctx = observability.WithFields(ctx, observability.Fields{OrderID: command.OrderID.String(), SagaID: command.OrderID.String()})
	err := observability.Transaction(ctx, h.db, "payment_refund", func(tx *gorm.DB) error {
		insert := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&models.ProcessedMessage{MessageID: messageID})
		if insert.Error != nil {
			return fmt.Errorf("record processed payment.refund: %w", insert.Error)
		}
		if insert.RowsAffected == 0 {
			return nil
		}

		outcome := messages.Succeeded
		var attempt models.PaymentAttempt
		switch err := tx.Where("order_id = ?", command.OrderID).Take(&attempt).Error; {
		case errors.Is(err, gorm.ErrRecordNotFound):
			slog.Error("payment.refund for an order that has no payment attempt", "order_id", command.OrderID)
			outcome = messages.Failed
		case err != nil:
			return fmt.Errorf("load payment attempt: %w", err)
		case attempt.Status == "SUCCEEDED" && attempt.GatewayPaymentID != nil:
			if err := recordFundingRefund(tx, attempt); err != nil {
				return err
			}
		case attempt.Status == "FAILED":
			// The payment failed, so there is no funding payment to give back; any money
			// captured later has its own refund record.
		default:
			// Not settled (created, pending, awaiting, unknown): nothing is known to be owed,
			// so do not guess. The saga stays stuck for a person.
			slog.Error("payment.refund while the payment is not settled", "order_id", command.OrderID, "status", attempt.Status)
			outcome = messages.Failed
		}
		return enqueueStepReply(tx, command.OrderID, messages.REFUND_PAYMENT, outcome)
	})
	if err == nil {
		return nil
	}
	wrapped := fmt.Errorf("process payment.refund: %w", err)
	if isPermanentDatabaseError(err) {
		return messaging.Permanent(wrapped)
	}
	return messaging.Transient(wrapped)
}

// recordFundingRefund writes the refund record for the payment that funded the attempt.
// It is idempotent per payment: a repeat finds the record and changes nothing.
func recordFundingRefund(tx *gorm.DB, attempt models.PaymentAttempt) error {
	record := models.CompensationRecord{
		PaymentAttemptID: attempt.ID, OrderID: attempt.OrderID, Gateway: attempt.Gateway,
		GatewayPaymentID: *attempt.GatewayPaymentID, AmountPaise: attempt.AmountPaise, Currency: attempt.Currency,
		Reason: reasonCheckoutFailedAfterPayment, Status: models.CompensationRequired,
		ObservedAt: time.Now(), Source: "saga",
	}
	result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&record)
	if result.Error != nil {
		return fmt.Errorf("record refund for the funding payment: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil
	}
	businessmetrics.RefundsRequired.WithLabelValues(reasonCheckoutFailedAfterPayment).Inc()
	return addCompensationEvent(tx, record.ID, "", models.CompensationRequired,
		fmt.Sprintf("recorded: %s, %d %s, requested by the saga", reasonCheckoutFailedAfterPayment, attempt.AmountPaise, attempt.Currency))
}

// enqueueStepReply queues a saga reply for step in the caller's transaction.
func enqueueStepReply(tx *gorm.DB, orderID uuid.UUID, step messages.Step, outcome messages.Outcome) error {
	body, err := json.Marshal(messages.Reply{OrderID: orderID, Step: step, Outcome: outcome})
	if err != nil {
		return fmt.Errorf("encode %s reply: %w", step, err)
	}
	return outbox.Enqueue(tx, outbox.Message{
		MessageID:  uuid.New(),
		RoutingKey: messages.PaymentReplyRoutingKey,
		Payload:    body,
	})
}
