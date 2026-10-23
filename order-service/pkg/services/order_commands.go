package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/order-service/database/models"
	"github.com/chrisfonseca08/distri/order-service/pkg/messages"
	"github.com/chrisfonseca08/distri/order-service/pkg/messaging"
	"github.com/chrisfonseca08/distri/order-service/pkg/outbox"
	"github.com/chrisfonseca08/distri/order-service/pkg/utils/state"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type OrderCommandHandler struct {
	db            *gorm.DB
	createHandler *OrderCreateHandler
}

func NewOrderCommandHandler(db *gorm.DB) *OrderCommandHandler {
	return &OrderCommandHandler{db: db, createHandler: NewOrderCreateHandler(db)}
}

func (h *OrderCommandHandler) Handle(ctx context.Context, delivery messaging.Delivery) error {
	switch delivery.RoutingKey {
	case messages.OrderCreateRoutingKey:
		return h.createHandler.Handle(ctx, delivery)
	case messages.OrderMarkPaidRoutingKey:
		return h.handleTransition(ctx, delivery, state.OrderPaid, messages.MARK_ORDER_PAID)
	case messages.OrderCancelRoutingKey:
		return h.handleTransition(ctx, delivery, state.OrderCancelled, messages.MARK_ORDER_CANCELLED)
	default:
		return messaging.Permanent(fmt.Errorf("unsupported order routing key %q", delivery.RoutingKey))
	}
}

func (h *OrderCommandHandler) handleTransition(
	ctx context.Context,
	delivery messaging.Delivery,
	target state.OrderStatus,
	step messages.Step,
) error {
	orderID, err := decodeOrderTransition(delivery.Body, target)
	if err != nil {
		return messaging.Permanent(err)
	}
	messageID := strings.TrimSpace(delivery.MessageId)
	if messageID == "" {
		return messaging.Permanent(errors.New("order transition AMQP MessageId is required"))
	}

	err = h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		insert := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(
			&models.ProcessedMessage{MessageID: messageID},
		)
		if insert.Error != nil {
			return fmt.Errorf("record processed order transition: %w", insert.Error)
		}
		if insert.RowsAffected == 0 {
			return nil
		}

		var order models.Order
		query := tx.Table("orders.orders").
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", orderID).
			Take(&order)
		if errors.Is(query.Error, gorm.ErrRecordNotFound) {
			return ErrOrderNotFound
		}
		if query.Error != nil {
			return fmt.Errorf("load order for transition: %w", query.Error)
		}

		outcome := messages.Succeeded
		current := state.OrderStatus(order.Status)
		switch {
		case current == target:
			// Idempotent replay under a new message ID: emit the reply again.
		case current == state.OrderPendingPayment:
			if !state.CanTransition(current, target) {
				return fmt.Errorf("invalid order transition %s to %s", current, target)
			}
			update := tx.Table("orders.orders").
				Where("id = ? AND status = ?", orderID, state.OrderPendingPayment).
				Updates(map[string]any{"status": target, "updated_at": time.Now().UTC()})
			if update.Error != nil {
				return fmt.Errorf("update order transition: %w", update.Error)
			}
			if update.RowsAffected != 1 {
				return errors.New("order moved during guarded transition")
			}
		default:
			outcome = messages.Failed
			slog.Error(
				"order transition conflicts with terminal status",
				"order_id", orderID,
				"current_status", current,
				"requested_status", target,
			)
		}

		body, err := json.Marshal(messages.Reply{
			OrderID: orderID,
			Step:    step,
			Outcome: outcome,
		})
		if err != nil {
			return fmt.Errorf("encode order transition reply: %w", err)
		}
		return outbox.Enqueue(tx, outbox.Message{
			MessageID:  uuid.New(),
			RoutingKey: messages.OrderReplyRoutingKey,
			Payload:    body,
		})
	})
	if err == nil {
		return nil
	}
	wrapped := fmt.Errorf("process order transition: %w", err)
	if errors.Is(err, ErrOrderNotFound) || isPermanentDatabaseError(err) {
		return messaging.Permanent(wrapped)
	}
	return messaging.Transient(wrapped)
}

func decodeOrderTransition(body []byte, target state.OrderStatus) (uuid.UUID, error) {
	var orderID uuid.UUID
	switch target {
	case state.OrderPaid:
		var command messages.OrderMarkPaid
		if err := json.Unmarshal(body, &command); err != nil {
			return uuid.Nil(), fmt.Errorf("decode order.mark-paid: %w", err)
		}
		orderID = command.OrderID
	case state.OrderCancelled:
		var command messages.OrderCancel
		if err := json.Unmarshal(body, &command); err != nil {
			return uuid.Nil(), fmt.Errorf("decode order.cancel: %w", err)
		}
		orderID = command.OrderID
	default:
		return uuid.Nil(), fmt.Errorf("unsupported order transition target %q", target)
	}
	if orderID == uuid.Nil() {
		return uuid.Nil(), errors.New("order transition order_id is required")
	}
	return orderID, nil
}
