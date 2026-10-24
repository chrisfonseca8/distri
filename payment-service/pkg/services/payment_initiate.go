package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"uuid"

	"github.com/chrisfonseca08/distri/payment-service/database/models"
	"github.com/chrisfonseca08/distri/payment-service/pkg/messages"
	"github.com/chrisfonseca08/distri/payment-service/pkg/messaging"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PaymentInitiateHandler struct {
	db      *gorm.DB
	gateway string
}

func NewPaymentInitiateHandler(db *gorm.DB, options ...GatewayOption) *PaymentInitiateHandler {
	return &PaymentInitiateHandler{db: db, gateway: gatewayNameFrom(options)}
}

func (h *PaymentInitiateHandler) Handle(ctx context.Context, delivery messaging.Delivery) error {
	if delivery.RoutingKey != messages.PaymentInitiateRoutingKey {
		return messaging.Permanent(fmt.Errorf("unsupported payment routing key %q", delivery.RoutingKey))
	}
	var command messages.PaymentInitiate
	if err := json.Unmarshal(delivery.Body, &command); err != nil {
		return messaging.Permanent(fmt.Errorf("decode payment.initiate: %w", err))
	}
	command.Currency = strings.ToUpper(strings.TrimSpace(command.Currency))
	if command.OrderID == uuid.Nil() || command.AmountPaise <= 0 || len(command.Currency) != 3 {
		return messaging.Permanent(errors.New("invalid payment.initiate payload"))
	}
	messageID := strings.TrimSpace(delivery.MessageId)
	if messageID == "" {
		return messaging.Permanent(errors.New("payment.initiate AMQP MessageId is required"))
	}

	err := h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		insert := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(
			&models.ProcessedMessage{MessageID: messageID},
		)
		if insert.Error != nil {
			return fmt.Errorf("record processed payment command: %w", insert.Error)
		}
		if insert.RowsAffected == 0 {
			return nil
		}

		result, err := createPayment(tx, CreatePaymentInput{
			OrderID: command.OrderID, AmountPaise: command.AmountPaise, Currency: command.Currency,
			Gateway: h.gateway,
		})
		if err != nil {
			return err
		}
		if !result.Created && isTerminalPaymentStatus(result.Attempt.Status) {
			return enqueuePaymentReply(tx, result.Attempt)
		}
		return nil
	})
	if err == nil {
		return nil
	}
	wrapped := fmt.Errorf("process payment.initiate: %w", err)
	if isPermanentDatabaseError(err) {
		return messaging.Permanent(wrapped)
	}
	return messaging.Transient(wrapped)
}

func isTerminalPaymentStatus(status string) bool {
	return status == "SUCCEEDED" || status == "FAILED" || status == "UNKNOWN"
}

type sqlStateError interface {
	SQLState() string
}

func isPermanentDatabaseError(err error) bool {
	var databaseError sqlStateError
	if !errors.As(err, &databaseError) {
		return false
	}
	class := databaseError.SQLState()
	return strings.HasPrefix(class, "22") || strings.HasPrefix(class, "23")
}
