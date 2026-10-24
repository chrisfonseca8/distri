package services

import (
	"encoding/json"
	"fmt"
	"uuid"

	"github.com/chrisfonseca08/distri/payment-service/database/models"
	"github.com/chrisfonseca08/distri/payment-service/pkg/messages"
	"github.com/chrisfonseca08/distri/payment-service/pkg/outbox"
	"gorm.io/gorm"
)

// enqueuePaymentReply queues the saga reply for a finished payment in the same transaction as its status change.
func enqueuePaymentReply(tx *gorm.DB, payment models.PaymentAttempt) error {
	var outcome messages.Outcome
	switch payment.Status {
	case "SUCCEEDED":
		outcome = messages.Succeeded
	case "FAILED":
		outcome = messages.Failed
	case "UNKNOWN":
		outcome = messages.Ambiguous
	default:
		return fmt.Errorf("payment status %q has no reply outcome", payment.Status)
	}
	body, err := json.Marshal(messages.Reply{
		OrderID: payment.OrderID,
		Step:    messages.TAKE_PAYMENT,
		Outcome: outcome,
	})
	if err != nil {
		return fmt.Errorf("encode payment reply: %w", err)
	}
	return outbox.Enqueue(tx, outbox.Message{
		MessageID:  uuid.New(),
		RoutingKey: messages.PaymentReplyRoutingKey,
		Payload:    body,
	})
}
