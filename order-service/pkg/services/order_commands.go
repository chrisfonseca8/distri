package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chrisfonseca08/distri/order-service/pkg/businessmetrics"
	"github.com/chrisfonseca08/distri/order-service/pkg/observability"
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

// NewOrderCommandHandler creates the handler for order commands received from RabbitMQ.
func NewOrderCommandHandler(db *gorm.DB) *OrderCommandHandler {
	return &OrderCommandHandler{db: db, createHandler: NewOrderCreateHandler(db)}
}

// Handle sends a command to the create, mark-paid or cancel handler based on its routing key.
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

// handleTransition applies the target status to an order, once per message ID, and queues a reply for the saga step.
func (h *OrderCommandHandler) handleTransition(
	ctx context.Context,
	delivery messaging.Delivery,
	target state.OrderStatus,
	step messages.Step,
) error {
	orderID, refunded, err := decodeOrderTransition(delivery.Body, target)
	if err != nil {
		return messaging.Permanent(err)
	}
	if refunded {
		step = messages.CANCEL_PAID_ORDER
	}
	messageID := strings.TrimSpace(delivery.MessageId)
	if messageID == "" {
		return messaging.Permanent(errors.New("order transition AMQP MessageId is required"))
	}
	ctx = observability.WithFields(ctx, observability.Fields{OrderID: orderID.String(), SagaID: orderID.String()})
	applied, replayed := false, false

	err = observability.Transaction(ctx, h.db, "order_transition", func(tx *gorm.DB) error {
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
			// The order does not exist (yet). A cancel is recorded as a tombstone and
			// succeeds, so a saga that gave up on order.create can finish compensating and
			// a late order.create for this id does nothing. Any other transition of an
			// order that was never created is answered Failed instead of being dead-lettered
			// without a reply.
			outcome := messages.Failed
			if target == state.OrderCancelled && !refunded {
				if err := tx.Table("orders.order_tombstones").
					Clauses(clause.OnConflict{DoNothing: true}).
					Create(&models.OrderTombstone{OrderID: orderID}).Error; err != nil {
					return fmt.Errorf("record order tombstone: %w", err)
				}
				outcome = messages.Succeeded
			} else {
				slog.ErrorContext(ctx, "order transition for an order that does not exist, replying Failed",
					"order_id", orderID, "requested_status", target)
			}
			return enqueueTransitionReply(tx, orderID, step, outcome)
		}
		if query.Error != nil {
			return fmt.Errorf("load order for transition: %w", query.Error)
		}

		outcome := messages.Succeeded
		current := state.OrderStatus(order.Status)
		switch {
		case current == target:
			// Idempotent replay under a new message ID: emit the reply again.
			replayed = true
		case current == state.OrderPaid && target == state.OrderCancelled && refunded:
			// The customer paid, but the checkout could not be completed and the payment is
			// being refunded: the paid order is cancelled. Nothing else may cancel a paid order.
			update := tx.Table("orders.orders").
				Where("id = ? AND status = ?", orderID, state.OrderPaid).
				Updates(map[string]any{"status": target, "updated_at": time.Now().UTC()})
			if update.Error != nil {
				return fmt.Errorf("cancel paid order: %w", update.Error)
			}
			if update.RowsAffected != 1 {
				return errors.New("order moved during guarded transition")
			}
			applied = true
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
			applied = true
		default:
			outcome = messages.Failed
			businessmetrics.Conflicts.WithLabelValues(string(step)).Inc()
			slog.ErrorContext(ctx,
				"order transition conflicts with terminal status",
				"order_id", orderID,
				"current_status", current,
				"requested_status", target,
			)
		}

		return enqueueTransitionReply(tx, orderID, step, outcome)
	})
	if err == nil {
		if applied {
			businessmetrics.Transitions.WithLabelValues(strings.ToLower(string(target))).Inc()
		}
		if replayed {
			businessmetrics.IdempotentReplays.WithLabelValues(string(step)).Inc()
		}
		return nil
	}
	businessmetrics.TransitionFailures.WithLabelValues(strings.ToLower(string(target)), observability.ClassifyDBError(err)).Inc()
	wrapped := fmt.Errorf("process order transition: %w", err)
	if errors.Is(err, ErrOrderNotFound) || isPermanentDatabaseError(err) {
		return messaging.Permanent(wrapped)
	}
	return messaging.Transient(wrapped)
}

// decodeOrderTransition reads the order ID from a mark-paid or cancel command, and whether
// a cancel is the cancel of a paid order whose payment is being refunded.
func decodeOrderTransition(body []byte, target state.OrderStatus) (uuid.UUID, bool, error) {
	var orderID uuid.UUID
	refunded := false
	switch target {
	case state.OrderPaid:
		var command messages.OrderMarkPaid
		if err := json.Unmarshal(body, &command); err != nil {
			return uuid.Nil(), false, fmt.Errorf("decode order.mark-paid: %w", err)
		}
		orderID = command.OrderID
	case state.OrderCancelled:
		var command messages.OrderCancel
		if err := json.Unmarshal(body, &command); err != nil {
			return uuid.Nil(), false, fmt.Errorf("decode order.cancel: %w", err)
		}
		orderID, refunded = command.OrderID, command.Refunded
	default:
		return uuid.Nil(), false, fmt.Errorf("unsupported order transition target %q", target)
	}
	if orderID == uuid.Nil() {
		return uuid.Nil(), false, errors.New("order transition order_id is required")
	}
	return orderID, refunded, nil
}

// enqueueTransitionReply writes a saga step reply to the outbox in the caller's transaction.
func enqueueTransitionReply(tx *gorm.DB, orderID uuid.UUID, step messages.Step, outcome messages.Outcome) error {
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
}
