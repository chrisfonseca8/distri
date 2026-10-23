package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func NewOrderCreateHandler(db *gorm.DB) *OrderCreateHandler {
	return &OrderCreateHandler{db: db}
}

func (h *OrderCreateHandler) Handle(
	ctx context.Context,
	delivery messaging.Delivery,
) error {
	var command messages.OrderCreate
	if err := json.Unmarshal(delivery.Body, &command); err != nil {
		return messaging.Permanent(fmt.Errorf("decode order.create message: %w", err))
	}
	if err := validateOrderCreate(command); err != nil {
		return messaging.Permanent(err)
	}

	messageID := strings.TrimSpace(delivery.MessageId)
	if messageID == "" {
		return messaging.Permanent(errors.New("order.create AMQP MessageId is required"))
	}

	replyPayload, err := json.Marshal(messages.OrderReply{
		OrderID: command.OrderID,
		Step:    messages.CREATE_ORDER,
		Outcome: messages.Succeeded,
	})
	if err != nil {
		return messaging.Permanent(fmt.Errorf("encode order reply: %w", err))
	}

	err = h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		processed := models.ProcessedMessage{MessageID: messageID}
		insertProcessed := tx.Table("orders.processed_messages").
			Clauses(clause.OnConflict{DoNothing: true}).
			Create(&processed)
		if insertProcessed.Error != nil {
			return fmt.Errorf("record processed order.create message: %w", insertProcessed.Error)
		}
		if insertProcessed.RowsAffected == 0 {
			return nil
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
		}

		if err := outbox.Enqueue(tx, outbox.Message{
			MessageID:  uuid.New(),
			RoutingKey: messages.OrderReplyRoutingKey,
			Payload:    replyPayload,
		}); err != nil {
			return err
		}
		return nil
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
