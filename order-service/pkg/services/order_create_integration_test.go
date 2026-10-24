package services

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"uuid"

	"github.com/chrisfonseca08/distri/order-service/database/models"
	"github.com/chrisfonseca08/distri/order-service/pkg/config"
	"github.com/chrisfonseca08/distri/order-service/pkg/messages"
	"github.com/chrisfonseca08/distri/order-service/pkg/messaging"
)

func TestOrderCreateHandlerTransactionAndIdempotency(t *testing.T) {
	if os.Getenv("RUN_ORDER_CREATE_INTEGRATION") != "1" {
		t.Skip("set RUN_ORDER_CREATE_INTEGRATION=1 to run database integration tests")
	}

	db, err := config.OpenDB()
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := config.RequireDedicatedTestDB(db); err != nil {
		t.Skip(err)
	}

	orderID := uuid.New()
	productID := uuid.New()
	firstMessageID := "order-create-test-" + uuid.New().String()
	secondMessageID := "order-create-test-" + uuid.New().String()
	t.Cleanup(func() {
		if err := db.Table("orders.outbox").
			Where("payload ->> 'order_id' = ?", orderID.String()).
			Delete(&models.Outbox{}).Error; err != nil {
			t.Errorf("delete outbox rows: %v", err)
		}
		if err := db.Table("orders.processed_messages").
			Where("message_id IN ?", []string{firstMessageID, secondMessageID}).
			Delete(&models.ProcessedMessage{}).Error; err != nil {
			t.Errorf("delete processed messages: %v", err)
		}
		if err := db.Table("orders.order_items").Where("order_id = ?", orderID).Delete(&models.OrderItem{}).Error; err != nil {
			t.Errorf("delete order items: %v", err)
		}
		if err := db.Table("orders.orders").Where("id = ?", orderID).Delete(&models.Order{}).Error; err != nil {
			t.Errorf("delete order: %v", err)
		}
	})

	command := messages.OrderCreate{
		OrderID:    orderID,
		Currency:   "INR",
		TotalPaise: 10000,
		Items: []messages.OrderCreateItem{{
			ProductID: productID, Quantity: 2, UnitPricePaise: 5000,
		}},
	}
	body, err := json.Marshal(command)
	if err != nil {
		t.Fatalf("encode command: %v", err)
	}

	handler := NewOrderCreateHandler(db)
	firstDelivery := messaging.Delivery{MessageId: firstMessageID, Body: body}
	if err := handler.Handle(t.Context(), firstDelivery); err != nil {
		t.Fatalf("handle first command: %v", err)
	}

	var order models.Order
	if err := db.Table("orders.orders").Where("id = ?", orderID).Take(&order).Error; err != nil {
		t.Fatalf("load order: %v", err)
	}
	if order.Status != "PENDING_PAYMENT" || order.TotalPaise != command.TotalPaise || order.Currency != command.Currency {
		t.Fatalf("order = %+v", order)
	}

	var items []models.OrderItem
	if err := db.Table("orders.order_items").Where("order_id = ?", orderID).Find(&items).Error; err != nil {
		t.Fatalf("load order items: %v", err)
	}
	if len(items) != 1 || items[0].ProductID != productID || items[0].Quantity != 2 || items[0].UnitPricePaise != 5000 {
		t.Fatalf("items = %+v", items)
	}

	var outboxRows []models.Outbox
	if err := db.Table("orders.outbox").
		Where("payload ->> 'order_id' = ?", orderID.String()).
		Find(&outboxRows).Error; err != nil {
		t.Fatalf("load outbox rows: %v", err)
	}
	if len(outboxRows) != 1 || outboxRows[0].MessageID == uuid.Nil() ||
		outboxRows[0].RoutingKey != messages.OrderReplyRoutingKey {
		t.Fatalf("outbox rows = %+v", outboxRows)
	}
	var reply messages.Reply
	if err := json.Unmarshal(outboxRows[0].Payload, &reply); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if reply.OrderID != orderID || reply.Step != messages.CREATE_ORDER || reply.Outcome != messages.Succeeded {
		t.Fatalf("reply = %+v", reply)
	}

	if err := handler.Handle(t.Context(), firstDelivery); err != nil {
		t.Fatalf("handle duplicate message ID: %v", err)
	}
	var count int64
	if err := db.Table("orders.outbox").Where("payload ->> 'order_id' = ?", orderID.String()).Count(&count).Error; err != nil {
		t.Fatalf("count duplicate outbox rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("outbox rows after duplicate = %d, want 1", count)
	}

	if err := handler.Handle(t.Context(), messaging.Delivery{MessageId: secondMessageID, Body: body}); err != nil {
		t.Fatalf("handle existing order with new message ID: %v", err)
	}
	count = 0
	if err := db.Table("orders.outbox").Where("payload ->> 'order_id' = ?", orderID.String()).Count(&count).Error; err != nil {
		t.Fatalf("count existing-order replies: %v", err)
	}
	if count != 2 {
		t.Fatalf("existing-order reply rows = %d, want 2", count)
	}
	count = 0
	if err := db.Table("orders.order_items").Where("order_id = ?", orderID).Count(&count).Error; err != nil {
		t.Fatalf("count existing-order items: %v", err)
	}
	if count != 1 {
		t.Fatalf("existing-order item rows = %d, want 1", count)
	}
}

func TestOrderCreateHandlerReturnsTransientDatabaseErrors(t *testing.T) {
	if os.Getenv("RUN_ORDER_CREATE_INTEGRATION") != "1" {
		t.Skip("set RUN_ORDER_CREATE_INTEGRATION=1 to run database integration tests")
	}

	db, err := config.OpenDB()
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get database connection: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	command := messages.OrderCreate{
		OrderID: uuid.New(), Currency: "INR", TotalPaise: 1,
		Items: []messages.OrderCreateItem{{ProductID: uuid.New(), Quantity: 1, UnitPricePaise: 1}},
	}
	body, err := json.Marshal(command)
	if err != nil {
		t.Fatalf("encode command: %v", err)
	}
	err = NewOrderCreateHandler(db).Handle(t.Context(), messaging.Delivery{
		MessageId: uuid.New().String(),
		Body:      body,
	})
	var transient *messaging.TransientError
	if !errors.As(err, &transient) {
		t.Fatalf("error = %v, want TransientError", err)
	}
}
