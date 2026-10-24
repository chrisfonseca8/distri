package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"uuid"

	"github.com/chrisfonseca08/distri/order-service/database/models"
	"github.com/chrisfonseca08/distri/order-service/pkg/messages"
	"github.com/chrisfonseca08/distri/order-service/pkg/messaging"
	"github.com/chrisfonseca08/distri/order-service/pkg/outbox"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type OrderCreateHandler struct {
	db *gorm.DB
}

// NewOrderCreateHandler creates the handler for order.create commands.
func NewOrderCreateHandler(db *gorm.DB) *OrderCreateHandler {
	return &OrderCreateHandler{db: db}
}

// Handle creates the order and its items from an order.create command, once per message.
//
// A command that can never succeed still gets an answer when its order id is known: a
// Failed reply lets the saga compensate at once instead of waiting for its retries to
// run out. Only a command that cannot even be read (no order id) is dead-lettered.
func (h *OrderCreateHandler) Handle(
	ctx context.Context,
	delivery messaging.Delivery,
) error {
	var command messages.OrderCreate
	if err := json.Unmarshal(delivery.Body, &command); err != nil {
		return messaging.Permanent(fmt.Errorf("decode order.create message: %w", err))
	}
	messageID := strings.TrimSpace(delivery.MessageId)
	if messageID == "" {
		return messaging.Permanent(errors.New("order.create AMQP MessageId is required"))
	}
	if command.OrderID == uuid.Nil() {
		return messaging.Permanent(errors.New("order.create order_id is required"))
	}

	if err := validateOrderCreate(command); err != nil {
		slog.Error("order.create rejected, replying Failed", "order_id", command.OrderID, "error", err)
		return h.replyOnly(ctx, command.OrderID, messageID, messages.Failed)
	}

	outcome := messages.Succeeded
	err := h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		processed := models.ProcessedMessage{MessageID: messageID}
		insertProcessed := tx.Table("orders.processed_messages").
			Clauses(clause.OnConflict{DoNothing: true}).
			Create(&processed)
		if insertProcessed.Error != nil {
			return fmt.Errorf("record processed order.create message: %w", insertProcessed.Error)
		}
		if insertProcessed.RowsAffected == 0 {
			outcome = ""
			return nil
		}

		var tombstones int64
		if err := tx.Table("orders.order_tombstones").
			Where("order_id = ?", command.OrderID).Count(&tombstones).Error; err != nil {
			return fmt.Errorf("check order tombstone: %w", err)
		}
		if tombstones > 0 {
			// The checkout was already cancelled; creating the order now would resurrect it.
			slog.Warn("order.create ignored: the order was cancelled before it was created", "order_id", command.OrderID)
			outcome = messages.Failed
			return h.enqueueReply(tx, command.OrderID, outcome)
		}

		order := models.Order{
			ID:         command.OrderID,
			Status:     "PENDING_PAYMENT",
			TotalPaise: command.TotalPaise,
			Currency:   command.Currency,
		}
		insertOrder := tx.Table("orders.orders").
			Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "id"}},
				DoNothing: true,
			}).
			Create(&order)
		if insertOrder.Error != nil {
			return fmt.Errorf("create order from command: %w", insertOrder.Error)
		}

		if insertOrder.RowsAffected == 1 {
			items := make([]models.OrderItem, 0, len(command.Items))
			for _, item := range command.Items {
				items = append(items, models.OrderItem{
					OrderID:        command.OrderID,
					ProductID:      item.ProductID,
					Quantity:       item.Quantity,
					UnitPricePaise: item.UnitPricePaise,
				})
			}
			if err := tx.Table("orders.order_items").
				Omit(clause.Associations).
				Create(&items).Error; err != nil {
				return fmt.Errorf("create order items from command: %w", err)
			}
		} else {
			// A repeat under a new message id. It is only a repeat if the order that already
			// exists is the one this command describes.
			matches, err := existingOrderMatches(tx, command)
			if err != nil {
				return err
			}
			if !matches {
				slog.Error("order.create conflicts with the existing order, replying Failed", "order_id", command.OrderID)
				outcome = messages.Failed
			}
		}

		return h.enqueueReply(tx, command.OrderID, outcome)
	})
	if err != nil {
		wrapped := fmt.Errorf("process order.create message: %w", err)
		if isPermanentDatabaseError(err) {
			return messaging.Permanent(wrapped)
		}
		return messaging.Transient(wrapped)
	}
	return nil
}

// replyOnly records the message and answers it without creating an order.
func (h *OrderCreateHandler) replyOnly(ctx context.Context, orderID uuid.UUID, messageID string, outcome messages.Outcome) error {
	err := h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		insertProcessed := tx.Table("orders.processed_messages").
			Clauses(clause.OnConflict{DoNothing: true}).
			Create(&models.ProcessedMessage{MessageID: messageID})
		if insertProcessed.Error != nil {
			return fmt.Errorf("record processed order.create message: %w", insertProcessed.Error)
		}
		if insertProcessed.RowsAffected == 0 {
			return nil
		}
		return h.enqueueReply(tx, orderID, outcome)
	})
	if err != nil {
		wrapped := fmt.Errorf("reply to order.create message: %w", err)
		if isPermanentDatabaseError(err) {
			return messaging.Permanent(wrapped)
		}
		return messaging.Transient(wrapped)
	}
	return nil
}

// enqueueReply writes the CREATE_ORDER reply to the outbox in the caller's transaction.
func (h *OrderCreateHandler) enqueueReply(tx *gorm.DB, orderID uuid.UUID, outcome messages.Outcome) error {
	body, err := json.Marshal(messages.Reply{OrderID: orderID, Step: messages.CREATE_ORDER, Outcome: outcome})
	if err != nil {
		return messaging.Permanent(fmt.Errorf("encode order reply: %w", err))
	}
	return outbox.Enqueue(tx, outbox.Message{
		MessageID:  uuid.New(),
		RoutingKey: messages.OrderReplyRoutingKey,
		Payload:    body,
	})
}

// existingOrderMatches reports whether the stored order has the total, currency and
// lines the command describes.
func existingOrderMatches(tx *gorm.DB, command messages.OrderCreate) (bool, error) {
	var order models.Order
	if err := tx.Table("orders.orders").Where("id = ?", command.OrderID).Take(&order).Error; err != nil {
		return false, fmt.Errorf("load existing order: %w", err)
	}
	if order.TotalPaise != command.TotalPaise || order.Currency != command.Currency {
		return false, nil
	}
	var items []models.OrderItem
	if err := tx.Table("orders.order_items").Where("order_id = ?", command.OrderID).Find(&items).Error; err != nil {
		return false, fmt.Errorf("load existing order items: %w", err)
	}
	if len(items) != len(command.Items) {
		return false, nil
	}
	stored := make(map[uuid.UUID]models.OrderItem, len(items))
	for _, item := range items {
		stored[item.ProductID] = item
	}
	for _, item := range command.Items {
		have, found := stored[item.ProductID]
		if !found || have.Quantity != item.Quantity || have.UnitPricePaise != item.UnitPricePaise {
			return false, nil
		}
	}
	return true, nil
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

// validateOrderCreate checks that an order.create command has the fields an order needs.
func validateOrderCreate(command messages.OrderCreate) error {
	if command.OrderID == uuid.Nil() {
		return errors.New("order.create order_id is required")
	}
	if strings.TrimSpace(command.Currency) == "" {
		return errors.New("order.create currency is required")
	}
	if len(command.Currency) != 3 {
		return errors.New("order.create currency must contain three characters")
	}
	if len(command.Items) == 0 {
		return errors.New("order.create must contain at least one item")
	}

	productIDs := make(map[uuid.UUID]struct{}, len(command.Items))
	var calculatedTotal int64
	for _, item := range command.Items {
		if item.ProductID == uuid.Nil() {
			return errors.New("order.create item product_id is required")
		}
		if _, duplicate := productIDs[item.ProductID]; duplicate {
			return fmt.Errorf("order.create contains duplicate product %s", item.ProductID)
		}
		productIDs[item.ProductID] = struct{}{}

		if item.Quantity <= 0 {
			return fmt.Errorf("order.create item %s quantity must be positive", item.ProductID)
		}
		if item.UnitPricePaise <= 0 {
			return fmt.Errorf("order.create item %s unit price must be positive", item.ProductID)
		}
		if item.Quantity > math.MaxInt64/item.UnitPricePaise {
			return errors.New("order.create item total overflows int64")
		}
		lineTotal := item.Quantity * item.UnitPricePaise
		if calculatedTotal > math.MaxInt64-lineTotal {
			return errors.New("order.create total overflows int64")
		}
		calculatedTotal += lineTotal
	}
	if command.TotalPaise != calculatedTotal {
		return fmt.Errorf(
			"order.create total_paise is %d, want %d from items",
			command.TotalPaise,
			calculatedTotal,
		)
	}
	return nil
}
