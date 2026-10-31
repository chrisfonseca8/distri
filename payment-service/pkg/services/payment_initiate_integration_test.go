package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"uuid"

	"github.com/chrisfonseca08/distri/payment-service/database/models"
	"github.com/chrisfonseca08/distri/payment-service/pkg/jobtypes"
	"github.com/chrisfonseca08/distri/payment-service/pkg/messages"
	"github.com/chrisfonseca08/distri/payment-service/pkg/messaging"
	"gorm.io/gorm"
)

// initiateDelivery builds the AMQP delivery the broker would hand the consumer.
func initiateDelivery(t *testing.T, messageID string, command messages.PaymentInitiate) messaging.Delivery {
	t.Helper()
	body, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	return messaging.Delivery{RoutingKey: messages.PaymentInitiateRoutingKey, MessageId: messageID, Body: body}
}

func cleanupInitiate(t *testing.T, db *gorm.DB, orderID uuid.UUID, messageIDs ...string) {
	t.Helper()
	cleanupOrder(t, db, orderID)
	t.Cleanup(func() {
		db.Exec(`DELETE FROM payments.outbox WHERE payload->>'order_id' = ?`, orderID.String())
		if len(messageIDs) > 0 {
			db.Where("message_id IN ?", messageIDs).Delete(&models.ProcessedMessage{})
		}
	})
}

func attemptsFor(t *testing.T, db *gorm.DB, orderID uuid.UUID) []models.PaymentAttempt {
	t.Helper()
	var attempts []models.PaymentAttempt
	if err := db.Where("order_id = ?", orderID).Find(&attempts).Error; err != nil {
		t.Fatalf("load attempts: %v", err)
	}
	return attempts
}

func outboxCount(t *testing.T, db *gorm.DB, orderID uuid.UUID) int64 {
	t.Helper()
	var count int64
	if err := db.Table("payments.outbox").Where("payload->>'order_id' = ?", orderID.String()).Count(&count).Error; err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	return count
}

func TestSameInitiateMessageRedeliveredManyTimesCreatesOnePaymentAndOneJob(t *testing.T) {
	db := openTestDB(t)
	handler := NewPaymentInitiateHandler(db)
	orderID, messageID := uuid.New(), "initiate-"+uuid.New().String()
	cleanupInitiate(t, db, orderID, messageID)
	delivery := initiateDelivery(t, messageID, messages.PaymentInitiate{OrderID: orderID, AmountPaise: 25000, Currency: "INR"})

	for i := 0; i < 5; i++ {
		if err := handler.Handle(context.Background(), delivery); err != nil {
			t.Fatalf("delivery %d: %v", i, err)
		}
	}

	attempts := attemptsFor(t, db, orderID)
	if len(attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(attempts))
	}
	assertJobCount(t, db, attempts[0].ID, jobtypes.ProcessPayment, 1)
	var processed int64
	db.Model(&models.ProcessedMessage{}).Where("message_id = ?", messageID).Count(&processed)
	if processed != 1 {
		t.Fatalf("processed_messages rows = %d, want 1", processed)
	}
}

func TestSameInitiateMessageDeliveredConcurrentlyCreatesOnePayment(t *testing.T) {
	db := openTestDB(t)
	handler := NewPaymentInitiateHandler(db)
	orderID, messageID := uuid.New(), "initiate-"+uuid.New().String()
	cleanupInitiate(t, db, orderID, messageID)
	delivery := initiateDelivery(t, messageID, messages.PaymentInitiate{OrderID: orderID, AmountPaise: 25000, Currency: "INR"})

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ { // two consumers, redelivery during a slow ack
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := handler.Handle(context.Background(), delivery); err != nil {
				t.Errorf("handle: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	attempts := attemptsFor(t, db, orderID)
	if len(attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(attempts))
	}
	assertJobCount(t, db, attempts[0].ID, jobtypes.ProcessPayment, 1)
}

func TestResentInitiateWithANewMessageIdStillCreatesOnePayment(t *testing.T) {
	// The saga sweeper resends an unanswered command as a NEW outbox row, so a new
	// message id: only the order id can make this idempotent.
	db := openTestDB(t)
	handler := NewPaymentInitiateHandler(db)
	orderID := uuid.New()
	ids := []string{"resend-a-" + uuid.New().String(), "resend-b-" + uuid.New().String(), "resend-c-" + uuid.New().String()}
	cleanupInitiate(t, db, orderID, ids...)

	for _, id := range ids {
		delivery := initiateDelivery(t, id, messages.PaymentInitiate{OrderID: orderID, AmountPaise: 25000, Currency: "INR"})
		if err := handler.Handle(context.Background(), delivery); err != nil {
			t.Fatalf("resend %s: %v", id, err)
		}
	}

	attempts := attemptsFor(t, db, orderID)
	if len(attempts) != 1 {
		t.Fatalf("attempts = %d after 3 resends, want 1", len(attempts))
	}
	assertJobCount(t, db, attempts[0].ID, jobtypes.ProcessPayment, 1)
}

func TestResentInitiateForAnAlreadyFinishedPaymentRepeatsTheReplyButNeverTheCharge(t *testing.T) {
	db := openTestDB(t)
	handler := NewPaymentInitiateHandler(db)
	orderID := uuid.New()
	first, second, redelivery := "fin-1-"+uuid.New().String(), "fin-2-"+uuid.New().String(), "fin-1-"+uuid.New().String()
	cleanupInitiate(t, db, orderID, first, second, redelivery)
	command := messages.PaymentInitiate{OrderID: orderID, AmountPaise: 25000, Currency: "INR"}

	if err := handler.Handle(context.Background(), initiateDelivery(t, first, command)); err != nil {
		t.Fatal(err)
	}
	if outboxCount(t, db, orderID) != 0 {
		t.Fatal("a payment that has not finished must not reply yet")
	}
	attempts := attemptsFor(t, db, orderID)
	if err := db.Model(&models.PaymentAttempt{}).Where("id = ?", attempts[0].ID).Update("status", "SUCCEEDED").Error; err != nil {
		t.Fatal(err)
	}

	// The saga never saw the reply and resends the command under a new message id.
	if err := handler.Handle(context.Background(), initiateDelivery(t, second, command)); err != nil {
		t.Fatal(err)
	}
	if got := outboxCount(t, db, orderID); got != 1 {
		t.Fatalf("replies after a resend for a finished payment = %d, want 1 (the saga is told again)", got)
	}
	// The broker redelivers that same resend: no second reply, because the message id is known.
	if err := handler.Handle(context.Background(), initiateDelivery(t, second, command)); err != nil {
		t.Fatal(err)
	}
	if got := outboxCount(t, db, orderID); got != 1 {
		t.Fatalf("replies after redelivery of the same message = %d, want still 1", got)
	}
	if got := len(attemptsFor(t, db, orderID)); got != 1 {
		t.Fatalf("attempts = %d, a finished payment must never be charged again", got)
	}
	assertJobCount(t, db, attempts[0].ID, jobtypes.ProcessPayment, 1)
}

func TestInvalidInitiateCommandsAreRejectedPermanentlyAndLeaveNoTrace(t *testing.T) {
	db := openTestDB(t)
	handler := NewPaymentInitiateHandler(db)
	orderID := uuid.New()
	cleanupInitiate(t, db, orderID)

	cases := map[string]messaging.Delivery{
		"zero amount":      initiateDelivery(t, "bad-1-"+uuid.New().String(), messages.PaymentInitiate{OrderID: orderID, AmountPaise: 0, Currency: "INR"}),
		"negative amount":  initiateDelivery(t, "bad-2-"+uuid.New().String(), messages.PaymentInitiate{OrderID: orderID, AmountPaise: -1, Currency: "INR"}),
		"short currency":   initiateDelivery(t, "bad-3-"+uuid.New().String(), messages.PaymentInitiate{OrderID: orderID, AmountPaise: 100, Currency: "IN"}),
		"nil order":        initiateDelivery(t, "bad-4-"+uuid.New().String(), messages.PaymentInitiate{OrderID: uuid.Nil(), AmountPaise: 100, Currency: "INR"}),
		"missing msg id":   initiateDelivery(t, "", messages.PaymentInitiate{OrderID: orderID, AmountPaise: 100, Currency: "INR"}),
		"malformed json":   {RoutingKey: messages.PaymentInitiateRoutingKey, MessageId: "bad-5-" + uuid.New().String(), Body: []byte(`{not json`)},
		"wrong routingkey": {RoutingKey: "payment.other", MessageId: "bad-6-" + uuid.New().String(), Body: []byte(`{}`)},
	}
	for name, delivery := range cases {
		err := handler.Handle(context.Background(), delivery)
		var permanent *messaging.PermanentError
		if err == nil || !asPermanent(err, &permanent) {
			t.Fatalf("%s: error = %v, want a permanent failure (a retry can never fix it)", name, err)
		}
	}
	if got := len(attemptsFor(t, db, orderID)); got != 0 {
		t.Fatalf("invalid commands created %d payment attempts", got)
	}
	t.Log(fmt.Sprintf("%d invalid commands rejected permanently", len(cases)))
}
