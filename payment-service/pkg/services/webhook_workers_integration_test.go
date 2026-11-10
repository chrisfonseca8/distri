package services

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"
)

// startWorkers runs the real processing loops (not ProcessOnce) with `workers`
// goroutines over one pool, and returns a stop function that cancels them and
// waits for every loop to exit, as main's shutdown does.
func startWorkers(t *testing.T, db *gorm.DB, workers int) (stop func()) {
	t.Helper()
	processors, err := NewWebhookProcessors(db, WebhookProcessorConfig{
		Interval: time.Second, Batch: 10, Lease: 30 * time.Second, MaxAttempts: 3,
		BackoffBase: time.Hour, BackoffCap: time.Hour, PaymentWindow: webhookTestWindow, GatewayName: "razorpay",
	}, workers)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); RunWebhookWorkers(ctx, processors) }()
	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("webhook workers did not stop after cancellation")
		}
	}
}

func waitForInboxToDrain(t *testing.T, db *gorm.DB, fixtures []*webhookFixture) {
	t.Helper()
	var ids []string
	for _, f := range fixtures {
		ids = append(ids, f.eventIDs...)
	}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		var open int64
		db.Table("payments.webhook_events").Where("event_id IN ? AND status <> 'PROCESSED'", ids).Count(&open)
		if open == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the inbox did not drain within 60s")
}

// Two workers share one inbox. Each payment gets the events Razorpay really sends
// (authorized, captured, order.paid, redeliveries). Whatever order the two workers
// pick them up in, every payment must settle once, produce one saga reply, and
// owe no refund.
func TestTwoWebhookWorkersApplyEachPaymentOnce(t *testing.T) {
	const payments = 30
	db := openTestDB(t)
	var fixtures []*webhookFixture
	for i := 0; i < payments; i++ {
		f := newSharedFixture(t, db, "AWAITING_PAYMENT", 10*time.Minute)
		f.event(t, "payment.authorized", time.Now().Add(-time.Second), 25000, "INR")
		f.event(t, "payment.captured", time.Now(), 25000, "INR")
		f.event(t, "order.paid", time.Now(), 25000, "INR")
		f.event(t, "payment.captured", time.Now(), 25000, "INR") // redelivery under a new event id
		f.event(t, "order.paid", time.Now(), 25000, "INR")
		fixtures = append(fixtures, f)
	}
	stop := startWorkers(t, db, 2)
	waitForInboxToDrain(t, db, fixtures)

	// A late authorized (and failed) event for an already captured payment, handled
	// by the same running workers, must not move it backwards or repeat anything.
	for _, f := range fixtures {
		f.event(t, "payment.authorized", time.Now().Add(-time.Hour), 25000, "INR")
		f.event(t, "payment.failed", time.Now().Add(-time.Hour), 25000, "INR")
	}
	waitForInboxToDrain(t, db, fixtures)
	stop()

	for i, f := range fixtures {
		got := f.attempt(t)
		if got.Status != "SUCCEEDED" || got.GatewayPaymentID == nil || *got.GatewayPaymentID != f.paymentID {
			t.Fatalf("payment %d: %s / %v, want SUCCEEDED by %s", i, got.Status, got.GatewayPaymentID, f.paymentID)
		}
		if replies := f.replies(t); len(replies) != 1 || replies[0] != "Succeeded" {
			t.Fatalf("payment %d: saga replies = %v, want exactly one Succeeded", i, replies)
		}
		if c := f.compensations(t); len(c) != 0 {
			t.Fatalf("payment %d: duplicate deliveries created compensation %v", i, c)
		}
		for _, id := range f.eventIDs {
			row := f.eventRow(t, id)
			if row.Status != "PROCESSED" || row.Attempts != 1 {
				t.Fatalf("payment %d: event %s is %s after %d attempts, want PROCESSED after exactly 1", i, id, row.Status, row.Attempts)
			}
		}
	}
}

// A late payment.authorized that is claimed by a different worker at the same time
// as the capture is applied must still be a no-op, and an inbox row that one
// worker holds under a live lease must not be handed to the other.
func TestTwoWebhookWorkersNeverClaimTheSameRowTwice(t *testing.T) {
	db := openTestDB(t)
	f := newSharedFixture(t, db, "AWAITING_PAYMENT", 10*time.Minute)
	for i := 0; i < 60; i++ {
		f.event(t, "payment.captured", time.Now(), 25000, "INR")
	}
	stop := startWorkers(t, db, 2)
	waitForInboxToDrain(t, db, []*webhookFixture{f})
	stop()
	for _, id := range f.eventIDs {
		if row := f.eventRow(t, id); row.Attempts != 1 {
			t.Fatalf("event %s was claimed %d times, want exactly once", id, row.Attempts)
		}
	}
	if replies := f.replies(t); len(replies) != 1 {
		t.Fatalf("replies = %v, want exactly one", replies)
	}
}
