package services

import (
	"encoding/json"
	"os"
	"testing"
	"uuid"

	"github.com/chrisfonseca08/distri/order-service/database/models"
	"github.com/chrisfonseca08/distri/order-service/pkg/config"
	"github.com/chrisfonseca08/distri/order-service/pkg/messages"
	"github.com/chrisfonseca08/distri/order-service/pkg/messaging"
	"gorm.io/gorm"
)

func failureTestDB(t *testing.T, orderID uuid.UUID) *gorm.DB {
	t.Helper()
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
	t.Cleanup(func() {
		db.Exec(`DELETE FROM orders.outbox WHERE payload ->> 'order_id' = ?`, orderID.String())
		db.Exec(`DELETE FROM orders.order_items WHERE order_id = ?`, orderID)
		db.Exec(`DELETE FROM orders.orders WHERE id = ?`, orderID)
		db.Exec(`DELETE FROM orders.order_tombstones WHERE order_id = ?`, orderID)
		db.Exec(`DELETE FROM orders.processed_messages WHERE message_id LIKE ?`, "fail-test-%")
	})
	return db
}

func replies(t *testing.T, db *gorm.DB, orderID uuid.UUID) []messages.Reply {
	t.Helper()
	var rows []models.Outbox
	if err := db.Table("orders.outbox").Where("payload ->> 'order_id' = ?", orderID.String()).Order("created_at").Find(&rows).Error; err != nil {
		t.Fatalf("load replies: %v", err)
	}
	out := make([]messages.Reply, 0, len(rows))
	for _, row := range rows {
		var reply messages.Reply
		if err := json.Unmarshal(row.Payload, &reply); err != nil {
			t.Fatalf("decode reply: %v", err)
		}
		out = append(out, reply)
	}
	return out
}

func orderExists(t *testing.T, db *gorm.DB, orderID uuid.UUID) bool {
	t.Helper()
	var n int64
	db.Table("orders.orders").Where("id = ?", orderID).Count(&n)
	return n > 0
}

func createCommand(orderID uuid.UUID, total int64, quantity int64) []byte {
	body, _ := json.Marshal(messages.OrderCreate{
		OrderID: orderID, Currency: "INR", TotalPaise: total,
		Items: []messages.OrderCreateItem{{ProductID: uuid.MustParse("00000000-0000-4000-8000-0000000000aa"), Quantity: quantity, UnitPricePaise: 1000}},
	})
	return body
}

// EDGE-11 / IMP-1: a command that can never succeed is answered Failed, not dead-lettered silently.
func TestInvalidOrderCreateIsAnsweredFailedSoTheSagaCanCompensate(t *testing.T) {
	orderID := uuid.New()
	db := failureTestDB(t, orderID)
	handler := NewOrderCreateHandler(db)

	// the total does not match the lines: invalid, but the order id is known
	if err := handler.Handle(t.Context(), messaging.Delivery{MessageId: "fail-test-" + uuid.New().String(), Body: createCommand(orderID, 9999, 1)}); err != nil {
		t.Fatalf("a rejected command with a known order id is answered, not errored: %v", err)
	}
	got := replies(t, db, orderID)
	if len(got) != 1 || got[0].Step != messages.CREATE_ORDER || got[0].Outcome != messages.Failed {
		t.Fatalf("replies = %+v, want one CREATE_ORDER Failed", got)
	}
	if orderExists(t, db, orderID) {
		t.Fatal("no order may be created for a rejected command")
	}

	// the same message again changes nothing
	messageID := "fail-test-" + uuid.New().String()
	delivery := messaging.Delivery{MessageId: messageID, Body: createCommand(orderID, 9999, 1)}
	if err := handler.Handle(t.Context(), delivery); err != nil {
		t.Fatal(err)
	}
	if err := handler.Handle(t.Context(), delivery); err != nil {
		t.Fatal(err)
	}
	if got := replies(t, db, orderID); len(got) != 2 {
		t.Fatalf("replies = %d, want 2 (one earlier + one for this message id, not three)", len(got))
	}
}

// EDGE-13 / IMP-2: cancel before create leaves a tombstone, and the late create does nothing.
func TestCancelBeforeCreateLeavesATombstoneAndTheLateCreateIsRefused(t *testing.T) {
	orderID := uuid.New()
	db := failureTestDB(t, orderID)
	commands := NewOrderCommandHandler(db)

	cancelBody, _ := json.Marshal(messages.OrderCancel{OrderID: orderID})
	cancel := messaging.Delivery{MessageId: "fail-test-" + uuid.New().String(), RoutingKey: messages.OrderCancelRoutingKey, Body: cancelBody}
	if err := commands.Handle(t.Context(), cancel); err != nil {
		t.Fatalf("cancel of an order that does not exist yet must succeed: %v", err)
	}
	got := replies(t, db, orderID)
	if len(got) != 1 || got[0].Step != messages.MARK_ORDER_CANCELLED || got[0].Outcome != messages.Succeeded {
		t.Fatalf("replies = %+v, want one MARK_ORDER_CANCELLED Succeeded", got)
	}
	var tombstones int64
	db.Table("orders.order_tombstones").Where("order_id = ?", orderID).Count(&tombstones)
	if tombstones != 1 {
		t.Fatalf("tombstones = %d, want 1", tombstones)
	}

	// the delayed order.create arrives afterwards
	create := messaging.Delivery{MessageId: "fail-test-" + uuid.New().String(), RoutingKey: messages.OrderCreateRoutingKey, Body: createCommand(orderID, 1000, 1)}
	if err := commands.Handle(t.Context(), create); err != nil {
		t.Fatalf("late create: %v", err)
	}
	if orderExists(t, db, orderID) {
		t.Fatal("a late order.create must not resurrect an order that was cancelled first")
	}
	got = replies(t, db, orderID)
	if len(got) != 2 || got[1].Step != messages.CREATE_ORDER || got[1].Outcome != messages.Failed {
		t.Fatalf("replies = %+v, want the late create answered Failed", got)
	}

	// cancelling twice is harmless
	again := cancel
	again.MessageId = "fail-test-" + uuid.New().String()
	if err := commands.Handle(t.Context(), again); err != nil {
		t.Fatal(err)
	}
}

func TestMarkPaidForAnOrderThatWasNeverCreatedIsAnsweredFailed(t *testing.T) {
	orderID := uuid.New()
	db := failureTestDB(t, orderID)
	body, _ := json.Marshal(messages.OrderMarkPaid{OrderID: orderID})
	delivery := messaging.Delivery{MessageId: "fail-test-" + uuid.New().String(), RoutingKey: messages.OrderMarkPaidRoutingKey, Body: body}
	if err := NewOrderCommandHandler(db).Handle(t.Context(), delivery); err != nil {
		t.Fatalf("mark-paid for an unknown order is answered, not dead-lettered: %v", err)
	}
	got := replies(t, db, orderID)
	if len(got) != 1 || got[0].Step != messages.MARK_ORDER_PAID || got[0].Outcome != messages.Failed {
		t.Fatalf("replies = %+v, want one MARK_ORDER_PAID Failed", got)
	}
}

// EDGE-12 / IMP-3: an order that already exists with different contents is not silently accepted.
func TestExistingOrderWithDifferentContentsIsAnsweredFailed(t *testing.T) {
	orderID := uuid.New()
	db := failureTestDB(t, orderID)
	handler := NewOrderCreateHandler(db)
	if err := handler.Handle(t.Context(), messaging.Delivery{MessageId: "fail-test-" + uuid.New().String(), Body: createCommand(orderID, 2000, 2)}); err != nil {
		t.Fatal(err)
	}
	// same order id, different quantity and total
	if err := handler.Handle(t.Context(), messaging.Delivery{MessageId: "fail-test-" + uuid.New().String(), Body: createCommand(orderID, 3000, 3)}); err != nil {
		t.Fatal(err)
	}
	// identical contents under a new message id is a plain repeat
	if err := handler.Handle(t.Context(), messaging.Delivery{MessageId: "fail-test-" + uuid.New().String(), Body: createCommand(orderID, 2000, 2)}); err != nil {
		t.Fatal(err)
	}
	got := replies(t, db, orderID)
	if len(got) != 3 || got[0].Outcome != messages.Succeeded || got[1].Outcome != messages.Failed || got[2].Outcome != messages.Succeeded {
		t.Fatalf("outcomes = %+v, want Succeeded, Failed (different contents), Succeeded (identical repeat)", got)
	}
}

// A paid order is cancelled only by the cancel of a refunded checkout.
func TestPaidOrderIsCancelledOnlyWhenItsPaymentIsBeingRefunded(t *testing.T) {
	orderID := uuid.New()
	db := failureTestDB(t, orderID)
	commands := NewOrderCommandHandler(db)
	create := messaging.Delivery{MessageId: "fail-test-" + uuid.New().String(), RoutingKey: messages.OrderCreateRoutingKey, Body: createCommand(orderID, 1000, 1)}
	if err := commands.Handle(t.Context(), create); err != nil {
		t.Fatal(err)
	}
	paidBody, _ := json.Marshal(messages.OrderMarkPaid{OrderID: orderID})
	if err := commands.Handle(t.Context(), messaging.Delivery{MessageId: "fail-test-" + uuid.New().String(), RoutingKey: messages.OrderMarkPaidRoutingKey, Body: paidBody}); err != nil {
		t.Fatal(err)
	}
	status := func() string {
		var s string
		db.Raw(`SELECT status FROM orders.orders WHERE id = ?`, orderID.String()).Scan(&s)
		return s
	}
	if status() != "PAID" {
		t.Fatalf("setup: status = %s, want PAID", status())
	}

	// an ordinary cancel of a paid order is refused
	plain, _ := json.Marshal(messages.OrderCancel{OrderID: orderID})
	if err := commands.Handle(t.Context(), messaging.Delivery{MessageId: "fail-test-" + uuid.New().String(), RoutingKey: messages.OrderCancelRoutingKey, Body: plain}); err != nil {
		t.Fatal(err)
	}
	if status() != "PAID" {
		t.Fatalf("status = %s: nothing but a refunded checkout may cancel a paid order", status())
	}
	got := replies(t, db, orderID)
	if last := got[len(got)-1]; last.Step != messages.MARK_ORDER_CANCELLED || last.Outcome != messages.Failed {
		t.Fatalf("last reply = %+v, want MARK_ORDER_CANCELLED Failed", last)
	}

	// the cancel of a refunded checkout is accepted and answered as its own step
	refunded, _ := json.Marshal(messages.OrderCancel{OrderID: orderID, Refunded: true})
	deliver := messaging.Delivery{MessageId: "fail-test-" + uuid.New().String(), RoutingKey: messages.OrderCancelRoutingKey, Body: refunded}
	if err := commands.Handle(t.Context(), deliver); err != nil {
		t.Fatal(err)
	}
	if status() != "CANCELLED" {
		t.Fatalf("status = %s, want CANCELLED", status())
	}
	got = replies(t, db, orderID)
	if last := got[len(got)-1]; last.Step != messages.CANCEL_PAID_ORDER || last.Outcome != messages.Succeeded {
		t.Fatalf("last reply = %+v, want CANCEL_PAID_ORDER Succeeded", last)
	}
	// repeating it is harmless
	deliver.MessageId = "fail-test-" + uuid.New().String()
	if err := commands.Handle(t.Context(), deliver); err != nil {
		t.Fatal(err)
	}
	if status() != "CANCELLED" {
		t.Fatal("a repeat must leave the order cancelled")
	}
}
