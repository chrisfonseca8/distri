package services

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/chrisfonseca08/distri/payment-service/pkg/businessmetrics"
	"gorm.io/gorm"
)

// The sampler must count exactly what the claim query could claim. This runs the
// real claim statement (rolled back) next to the sampler over rows in every state.
func TestWebhookSamplerCountsExactlyWhatTheClaimQueryCouldClaim(t *testing.T) {
	db := openTestDB(t)
	sampler := NewWebhookSampler(db)
	before, err := sampler(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	claimable := func() int64 {
		var n int64
		_ = db.Transaction(func(tx *gorm.DB) error {
			var rows []struct{ ID string }
			tx.Raw(claimWebhookQuery, 1_000_000, "sampler-test", (30 * time.Second).Microseconds()).Scan(&rows)
			n = int64(len(rows))
			return gorm.ErrInvalidTransaction // roll back: the claim must not stick
		})
		return n
	}
	claimBefore := claimable()

	f := newSharedFixture(t, db, "AWAITING_PAYMENT", 10*time.Minute)
	set := func(id, sets string) {
		if err := db.Exec("UPDATE payments.webhook_events SET "+sets+" WHERE event_id = ?", id).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	ready := f.eventAt(t, "payment.captured", now, now, 25000, "INR", f.paymentID)
	expiredLease := f.eventAt(t, "payment.captured", now, now, 25000, "INR", f.paymentID)
	liveLease := f.eventAt(t, "payment.captured", now, now, 25000, "INR", f.paymentID)
	future := f.eventAt(t, "payment.captured", now, now, 25000, "INR", f.paymentID)
	processed := f.eventAt(t, "payment.captured", now, now, 25000, "INR", f.paymentID)
	parked := f.eventAt(t, "payment.captured", now, now, 25000, "INR", f.paymentID)
	set(ready, "next_attempt_at = now() - interval '40 seconds'")
	set(expiredLease, "next_attempt_at = now() - interval '10 seconds', locked_by = 'dead', locked_until = now() - interval '1 second'")
	set(liveLease, "next_attempt_at = now() - interval '10 seconds', locked_by = 'alive', locked_until = now() + interval '1 minute'")
	set(future, "next_attempt_at = now() + interval '1 hour'")
	set(processed, "status = 'PROCESSED', processed_at = now()")
	set(parked, "status = 'PARKED', park_reason = 'MALFORMED'")

	after, err := sampler(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := after.Ready - before.Ready; got != 2 {
		t.Fatalf("sampler counted %d new claimable events, want 2 (ready and expired lease only)", got)
	}
	if got := claimable() - claimBefore; got != 2 {
		t.Fatalf("the claim query could claim %d new events, want 2: sampler and claim disagree", got)
	}
	if after.OldestReadyAge < 39*time.Second {
		t.Fatalf("oldest ready age %s, want at least the 40 s of the oldest ready event", after.OldestReadyAge)
	}
}

// Workers are added and retired while events are being processed. Whatever the
// interleaving, every payment settles once, produces one saga reply, owes no refund,
// and each event is claimed and applied exactly once. A late authorized/failed event
// afterwards, handled by whichever workers exist then, must not regress anything.
func TestAutoscaledWorkersNeverRegressOrRepeatPaymentEffects(t *testing.T) {
	const payments = 40
	db := openTestDB(t)
	var fixtures []*webhookFixture
	for i := 0; i < payments; i++ {
		f := newSharedFixture(t, db, "AWAITING_PAYMENT", 10*time.Minute)
		f.event(t, "payment.authorized", time.Now().Add(-time.Second), 25000, "INR")
		f.event(t, "payment.captured", time.Now(), 25000, "INR")
		f.event(t, "order.paid", time.Now(), 25000, "INR")
		f.event(t, "payment.captured", time.Now(), 25000, "INR")                   // redelivery under a new event id
		f.event(t, "payment.authorized", time.Now().Add(-time.Hour), 25000, "INR") // out of order: older than the capture
		fixtures = append(fixtures, f)
	}
	cfg := WebhookProcessorConfig{
		Interval: time.Second, Batch: 5, Lease: 30 * time.Second, MaxAttempts: 3,
		BackoffBase: time.Hour, BackoffCap: time.Hour, PaymentWindow: webhookTestWindow, GatewayName: "razorpay",
	}
	pool := NewWebhookWorkerPool(func() (*WebhookProcessor, error) { return NewWebhookProcessor(db, cfg) })
	ctx, cancel := context.WithCancel(context.Background())
	if err := pool.Start(ctx, 1); err != nil {
		t.Fatal(err)
	}
	var churn sync.WaitGroup
	stopChurn := make(chan struct{})
	churn.Add(1)
	go func() { // 1 -> 4 -> 1 -> 4 ... every 15 ms while events are processed
		defer churn.Done()
		for i := 0; ; i++ {
			select {
			case <-stopChurn:
				return
			case <-time.After(15 * time.Millisecond):
			}
			if i%6 < 3 {
				_ = pool.Add()
			} else {
				pool.Retire()
			}
		}
	}()
	waitForInboxToDrain(t, db, fixtures)

	for _, f := range fixtures { // late events for payments that are already captured
		f.event(t, "payment.authorized", time.Now().Add(-time.Hour), 25000, "INR")
		f.event(t, "payment.failed", time.Now().Add(-time.Hour), 25000, "INR")
	}
	waitForInboxToDrain(t, db, fixtures)
	close(stopChurn)
	churn.Wait()
	cancel()
	pool.Wait()

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
			if row := f.eventRow(t, id); row.Status != "PROCESSED" || row.Attempts != 1 {
				t.Fatalf("payment %d: event %s is %s after %d claims, want PROCESSED after exactly 1", i, id, row.Status, row.Attempts)
			}
		}
	}
	busy, handled := pool.Load()
	if handled < int64(payments*5) || busy <= 0 {
		t.Fatalf("pool accounted %d events and %s busy time, want at least %d events", handled, busy, payments*5)
	}
	_ = businessmetrics.WebhookClaimable
}
