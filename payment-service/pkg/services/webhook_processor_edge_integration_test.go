package services

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/payment-service/database/models"
	"github.com/chrisfonseca08/distri/payment-service/pkg/payment_gateway"
	"gorm.io/gorm"
)

// ---- the deadline instant (pure decision, no database) ----

func TestDecideReconcileTreatsTheExactDeadlineInstantAsLate(t *testing.T) {
	deadline := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	for _, status := range []string{"AWAITING_PAYMENT", "UNKNOWN"} {
		attempt := models.PaymentAttempt{Status: status, AmountPaise: 25000, Currency: "INR", ExpiresAt: &deadline}
		capturedAt := func(observed time.Time) reconcileDecision {
			return decideReconcile(attempt, paymentgateway.OrderState{
				GatewayOrderID: "order_x", CapturedPaymentID: "pay_x", CapturedAmountPaise: 25000, CapturedCurrency: "INR",
				ObservedAt: observed, Source: "webhook",
			}, true, deadline.Add(time.Hour), webhookTestWindow)
		}

		onTime := capturedAt(deadline.Add(-time.Nanosecond))
		if onTime.updates["status"] != "SUCCEEDED" || len(onTime.compensations) != 0 {
			t.Fatalf("%s: capture 1ns before the deadline = %v / %d compensations, want SUCCEEDED and none", status, onTime.updates["status"], len(onTime.compensations))
		}
		for name, observed := range map[string]time.Time{"at the deadline": deadline, "1ns after": deadline.Add(time.Nanosecond)} {
			late := capturedAt(observed)
			if late.updates["status"] != "FAILED" || late.updates["failure_code"] != "PAYMENT_EXPIRED" {
				t.Fatalf("%s: capture %s = %v / %v, want FAILED PAYMENT_EXPIRED", status, name, late.updates["status"], late.updates["failure_code"])
			}
			if len(late.compensations) != 1 || late.compensations[0].PaymentID != "pay_x" {
				t.Fatalf("%s: capture %s must owe exactly one refund, got %+v", status, name, late.compensations)
			}
		}
	}
}

func TestDecideReconcileWithNoGatewayOrderExpiresExactlyAtTheDeadline(t *testing.T) {
	deadline := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	attempt := models.PaymentAttempt{Status: "AWAITING_PAYMENT", AmountPaise: 25000, Currency: "INR", ExpiresAt: &deadline}
	state := paymentgateway.OrderState{}

	if d := decideReconcile(attempt, state, false, deadline.Add(-time.Nanosecond), webhookTestWindow); d.updates != nil || d.expiry {
		t.Fatalf("1ns before the deadline: %+v, want no change", d)
	}
	if d := decideReconcile(attempt, state, false, deadline, webhookTestWindow); !d.expiry || d.updates["failure_code"] != "NO_GATEWAY_ORDER" {
		t.Fatalf("at the deadline: %+v, want an expiry", d)
	}
}

func TestDecideReconcileFallsBackToCreationTimePlusWindowWhenThereIsNoExpiry(t *testing.T) {
	created := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	attempt := models.PaymentAttempt{Status: "AWAITING_PAYMENT", AmountPaise: 25000, Currency: "INR", CreatedAt: created}
	state := func(observed time.Time) paymentgateway.OrderState {
		return paymentgateway.OrderState{GatewayOrderID: "o", CapturedPaymentID: "p", CapturedAmountPaise: 25000, CapturedCurrency: "INR", ObservedAt: observed}
	}
	if d := decideReconcile(attempt, state(created.Add(webhookTestWindow-time.Nanosecond)), true, created.Add(time.Hour), webhookTestWindow); d.updates["status"] != "SUCCEEDED" {
		t.Fatalf("just inside the window = %v, want SUCCEEDED", d.updates["status"])
	}
	if d := decideReconcile(attempt, state(created.Add(webhookTestWindow)), true, created.Add(time.Hour), webhookTestWindow); d.updates["status"] != "FAILED" {
		t.Fatalf("at the window end = %v, want FAILED", d.updates["status"])
	}
}

// ---- shared fixtures on one connection pool ----

// newSharedFixture builds a fixture on an existing pool: the storm tests create
// dozens of attempts and a pool per fixture would exhaust Postgres connections.
func newSharedFixture(t *testing.T, db *gorm.DB, status string, expiresIn time.Duration) *webhookFixture {
	t.Helper()
	f := &webhookFixture{
		db: db, orderID: uuid.New(),
		gwOrderID: "order_it_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:14],
		paymentID: "pay_it_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:14],
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM payments.compensation_events WHERE record_id IN (SELECT id FROM payments.compensation_records WHERE order_id = ?)", f.orderID)
		db.Exec("DELETE FROM payments.compensation_records WHERE order_id = ?", f.orderID)
		db.Exec("DELETE FROM payments.outbox WHERE payload->>'order_id' = ?", f.orderID.String())
		for _, id := range f.eventIDs {
			db.Exec("DELETE FROM payments.webhook_events WHERE event_id = ?", id)
		}
		db.Exec("DELETE FROM payments.payment_attempts WHERE order_id = ?", f.orderID)
	})
	f.insertAttempt(t, status, f.gwOrderID, expiresIn)
	return f
}

// drain runs `processors` concurrent webhook processors until every event of the
// given fixtures has left PENDING/PROCESSING, or the timeout passes.
func drain(t *testing.T, db *gorm.DB, processors int, fixtures []*webhookFixture) {
	t.Helper()
	var ids []string
	for _, f := range fixtures {
		ids = append(ids, f.eventIDs...)
	}
	deadline := time.Now().Add(60 * time.Second)
	var wg sync.WaitGroup
	for i := 0; i < processors; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			processor := newProcessor(t, db)
			for time.Now().Before(deadline) {
				if err := processor.ProcessOnce(context.Background()); err != nil {
					t.Errorf("process: %v", err)
					return
				}
				var open int64
				db.Table("payments.webhook_events").Where("event_id IN ? AND status IN ('PENDING','PROCESSING')", ids).Count(&open)
				if open == 0 {
					return
				}
				time.Sleep(20 * time.Millisecond)
			}
		}()
	}
	wg.Wait()
}

// ---- storms and replays ----

func TestWebhookStormAcrossManyPaymentsAppliesEachCaptureExactlyOnce(t *testing.T) {
	const payments, duplicates = 25, 6
	db := openTestDB(t)
	var fixtures []*webhookFixture
	for i := 0; i < payments; i++ {
		f := newSharedFixture(t, db, "AWAITING_PAYMENT", 10*time.Minute)
		// Razorpay sends captured and order.paid for one capture, and redelivers: many
		// distinct event ids, one payment.
		for d := 0; d < duplicates; d++ {
			kind := "payment.captured"
			if d%2 == 1 {
				kind = "order.paid"
			}
			f.event(t, kind, time.Now(), 25000, "INR")
		}
		f.event(t, "payment.authorized", time.Now().Add(-time.Minute), 25000, "INR")
		f.event(t, "payment.failed", time.Now().Add(-time.Minute), 25000, "INR")
		fixtures = append(fixtures, f)
	}

	drain(t, db, 4, fixtures)

	for i, f := range fixtures {
		if got := f.attempt(t); got.Status != "SUCCEEDED" || got.GatewayPaymentID == nil || *got.GatewayPaymentID != f.paymentID {
			t.Fatalf("payment %d: attempt = %s / %v, want SUCCEEDED by %s", i, got.Status, got.GatewayPaymentID, f.paymentID)
		}
		if replies := f.replies(t); len(replies) != 1 || replies[0] != "Succeeded" {
			t.Fatalf("payment %d: saga replies = %v, want exactly one Succeeded", i, replies)
		}
		if len(f.compensations(t)) != 0 {
			t.Fatalf("payment %d: duplicates of the funding capture created compensation %v", i, f.compensations(t))
		}
		for _, id := range f.eventIDs {
			if row := f.eventRow(t, id); row.Status != "PROCESSED" {
				t.Fatalf("payment %d: event %s is %s, want PROCESSED", i, id, row.Status)
			}
		}
	}
}

func TestWebhookStormOnOneOrderWithFourProcessorsCreatesOneReply(t *testing.T) {
	db := openTestDB(t)
	f := newSharedFixture(t, db, "AWAITING_PAYMENT", 10*time.Minute)
	for i := 0; i < 80; i++ {
		f.event(t, "payment.captured", time.Now(), 25000, "INR")
	}

	drain(t, db, 4, []*webhookFixture{f})

	if got := f.attempt(t); got.Status != "SUCCEEDED" {
		t.Fatalf("attempt = %s, want SUCCEEDED", got.Status)
	}
	if replies := f.replies(t); len(replies) != 1 {
		t.Fatalf("80 duplicate captures produced replies %v, want exactly one", replies)
	}
}

func TestReplayingProcessedEventsChangesNothing(t *testing.T) {
	f := newWebhookFixture(t, "AWAITING_PAYMENT", 10*time.Minute)
	first := f.event(t, "payment.captured", time.Now(), 25000, "INR")
	if err := newProcessor(t, f.db).ProcessOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := f.attempt(t)

	// The provider redelivers, or an operator resets the inbox row: the same event is
	// processed again from PENDING.
	for i := 0; i < 3; i++ {
		if err := f.db.Exec(`UPDATE payments.webhook_events SET status = 'PENDING', next_attempt_at = now(), processed_at = NULL WHERE event_id = ?`, first).Error; err != nil {
			t.Fatalf("reset event: %v", err)
		}
		if err := newProcessor(t, f.db).ProcessOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	after := f.attempt(t)
	if after.Status != "SUCCEEDED" || *after.GatewayPaymentID != *before.GatewayPaymentID {
		t.Fatalf("attempt after replays = %s / %v, want unchanged %s / %v", after.Status, after.GatewayPaymentID, before.Status, before.GatewayPaymentID)
	}
	if replies := f.replies(t); len(replies) != 1 {
		t.Fatalf("replays produced replies %v, want still exactly one", replies)
	}
	if len(f.compensations(t)) != 0 {
		t.Fatalf("replaying the funding capture created compensation %v", f.compensations(t))
	}
	if row := f.eventRow(t, first); row.Status != "PROCESSED" {
		t.Fatalf("replayed event is %s, want PROCESSED", row.Status)
	}
}

func TestRepeatedCapturesOnACancelledCheckoutOweOneRefundPerPayment(t *testing.T) {
	db := openTestDB(t)
	f := newSharedFixture(t, db, "FAILED", -time.Hour)
	f.db.Exec("UPDATE payments.payment_attempts SET failure_code = 'PAYMENT_EXPIRED' WHERE order_id = ?", f.orderID)
	second := "pay_it_second_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:8]
	for i := 0; i < 5; i++ {
		f.eventAt(t, "payment.captured", time.Now(), time.Now(), 25000, "INR", f.paymentID)
		f.eventAt(t, "payment.captured", time.Now(), time.Now(), 25000, "INR", second)
	}

	drain(t, db, 3, []*webhookFixture{f})

	records := f.compensations(t)
	if len(records) != 2 || records[f.paymentID] != "REQUIRED" || records[second] != "REQUIRED" {
		t.Fatalf("records = %v, want exactly one REQUIRED record per distinct payment", records)
	}
	if got := f.attempt(t); got.Status != "FAILED" {
		t.Fatalf("attempt = %s, a cancelled checkout must stay cancelled", got.Status)
	}
	if replies := f.replies(t); len(replies) != 0 {
		t.Fatalf("replies = %v: the saga already compensated and must not be told again", replies)
	}
}

func TestCaptureForAnAmountThatDoesNotMatchIsNeverAcceptedEvenInAStorm(t *testing.T) {
	db := openTestDB(t)
	f := newSharedFixture(t, db, "AWAITING_PAYMENT", 10*time.Minute)
	for i := 0; i < 10; i++ {
		f.event(t, "payment.captured", time.Now(), 24999, "INR") // one paisa short
	}

	drain(t, db, 3, []*webhookFixture{f})

	got := f.attempt(t)
	if got.Status == "SUCCEEDED" {
		t.Fatal("an underpaid capture completed the payment")
	}
	if got.FailureCode == nil || *got.FailureCode != "AMOUNT_MISMATCH" {
		t.Fatalf("failure code = %v, want AMOUNT_MISMATCH", got.FailureCode)
	}
	if replies := f.replies(t); len(replies) != 1 || replies[0] != "Failed" {
		t.Fatalf("replies = %v, want exactly one Failed", replies)
	}
	if records := f.compensations(t); len(records) != 1 {
		t.Fatalf("records = %v, want the one underpaid payment tracked for refund", records)
	}
}
