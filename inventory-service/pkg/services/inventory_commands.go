package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"uuid"

	"github.com/chrisfonseca08/distri/inventory-service/database/models"
	"github.com/chrisfonseca08/distri/inventory-service/pkg/messages"
	"github.com/chrisfonseca08/distri/inventory-service/pkg/messaging"
	"github.com/chrisfonseca08/distri/inventory-service/pkg/outbox"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type InventoryCommandHandler struct {
	db *gorm.DB
}

// NewInventoryCommandHandler creates the handler for inventory commands received from RabbitMQ.
func NewInventoryCommandHandler(db *gorm.DB) *InventoryCommandHandler {
	return &InventoryCommandHandler{db: db}
}

// Handle sends a confirm or release command to handleChange based on its routing key.
func (h *InventoryCommandHandler) Handle(ctx context.Context, delivery messaging.Delivery) error {
	switch delivery.RoutingKey {
	case messages.InventoryConfirmRoutingKey:
		return h.handleChange(ctx, delivery, "CONFIRMED", confirmStock, messages.CONFIRM_STOCK)
	case messages.InventoryReleaseRoutingKey:
		return h.handleChange(ctx, delivery, "RELEASED", releaseStock, messages.RELEASE_STOCK)
	default:
		return messaging.Permanent(fmt.Errorf("unsupported inventory routing key %q", delivery.RoutingKey))
	}
}

// handleChange applies a confirm or release to an order's reservation once per message ID and queues a reply for the saga step.
func (h *InventoryCommandHandler) handleChange(
	ctx context.Context,
	delivery messaging.Delivery,
	targetStatus string,
	movement stockMovement,
	step messages.Step,
) error {
	orderID, err := decodeInventoryCommand(delivery.Body, movement)
	if err != nil {
		return messaging.Permanent(err)
	}
	messageID := strings.TrimSpace(delivery.MessageId)
	if messageID == "" {
		return messaging.Permanent(errors.New("inventory command AMQP MessageId is required"))
	}

	err = h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		insert := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(
			&models.ProcessedMessage{MessageID: messageID},
		)
		if insert.Error != nil {
			return fmt.Errorf("record processed inventory command: %w", insert.Error)
		}
		if insert.RowsAffected == 0 {
			return nil
		}

		// A repeat under a new message ID moves no stock but still replies with
		// the current outcome, so a lost reply is recovered.
		outcome := messages.Succeeded
		_, err := changeReservationStatusTx(tx, orderID, targetStatus, movement)
		switch {
		case err == nil:
		case errors.Is(err, ErrReservationStateConflict), errors.Is(err, ErrReservationNotFound):
			// Definitive business outcomes, not database errors, so the
			// transaction is still healthy and the reply commits with the
			// processed_messages row.
			outcome = messages.Failed
			slog.Error(
				"inventory command cannot be applied",
				"order_id", orderID,
				"step", step,
				"error", err,
			)
		default:
			return err
		}

		body, err := json.Marshal(messages.Reply{
			OrderID: orderID,
			Step:    step,
			Outcome: outcome,
		})
		if err != nil {
			return fmt.Errorf("encode inventory reply: %w", err)
		}
		return outbox.Enqueue(tx, outbox.Message{
			MessageID:  uuid.New(),
			RoutingKey: messages.InventoryReplyRoutingKey,
			Payload:    body,
		})
	})
	if err == nil {
		return nil
	}
	wrapped := fmt.Errorf("process inventory command: %w", err)
	if errors.Is(err, errStockInvariant) || isPermanentDatabaseError(err) {
		return messaging.Permanent(wrapped)
	}
	return messaging.Transient(wrapped)
}

// decodeInventoryCommand reads the order ID from a confirm or release command.
func decodeInventoryCommand(body []byte, movement stockMovement) (uuid.UUID, error) {
	var orderID uuid.UUID
	switch movement {
	case confirmStock:
		var command messages.InventoryConfirm
		if err := json.Unmarshal(body, &command); err != nil {
			return uuid.Nil(), fmt.Errorf("decode inventory.confirm: %w", err)
		}
		orderID = command.OrderID
	case releaseStock:
		var command messages.InventoryRelease
		if err := json.Unmarshal(body, &command); err != nil {
			return uuid.Nil(), fmt.Errorf("decode inventory.release: %w", err)
		}
		orderID = command.OrderID
	}
	if orderID == uuid.Nil() {
		return uuid.Nil(), errors.New("inventory command order_id is required")
	}
	return orderID, nil
}

type sqlStateError interface {
	SQLState() string
}

// isPermanentDatabaseError reports whether a database error can never succeed on retry.
func isPermanentDatabaseError(err error) bool {
	var databaseError sqlStateError
	if !errors.As(err, &databaseError) {
		return false
	}
	class := databaseError.SQLState()
	return strings.HasPrefix(class, "22") || strings.HasPrefix(class, "23")
}
