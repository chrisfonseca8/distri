package services

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/payment-service/database/models"
	"github.com/chrisfonseca08/distri/payment-service/pkg/payment_gateway"
	"gorm.io/gorm"
)

const webhookTestWindow = 5 * time.Minute

type webhookFixture struct {
	db        *gorm.DB
	orderID   uuid.UUID
	gwOrderID string
	paymentID string
	eventIDs  []string
}

func newWebhookFixture(t *testing.T, status string, expiresIn time.Duration) *webhookFixture {
	t.Helper()
	db := openTestDB(t)
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

func (f *webhookFixture) insertAttempt(t *testing.T, status, gwOrderID string, expiresIn time.Duration) {
	t.Helper()
	expires := time.Now().Add(expiresIn)
	attempt := models.PaymentAttempt{
		OrderID: f.orderID, AmountPaise: 25000, Currency: "INR", Status: status, Gateway: "razorpay",
		GatewayOrderID: &gwOrderID, ExpiresAt: &expires,
	}
	if err := f.db.Create(&attempt).Error; err != nil {
		t.Fatalf("insert attempt: %v", err)
	}
}

func (f *webhookFixture) attempt(t *testing.T) models.PaymentAttempt {
	t.Helper()
	var attempt models.PaymentAttempt
	if err := f.db.Where("order_id = ?", f.orderID).Take(&attempt).Error; err != nil {
		t.Fatalf("load attempt: %v", err)
	}
	return attempt
}

func (f *webhookFixture) replies(t *testing.T) []string {
	t.Helper()
	var outcomes []string
	if err := f.db.Raw("SELECT payload->>'outcome' FROM payments.outbox WHERE payload->>'order_id' = ? ORDER BY created_at", f.orderID.String()).
		Scan(&outcomes).Error; err != nil {
		t.Fatalf("load replies: %v", err)
	}
	return outcomes
}

// event stores an event as the HTTP handler would have: PENDING, due now,
// received now. createdAt is only the provider's claim inside the payload.
func (f *webhookFixture) event(t *testing.T, eventType string, createdAt time.Time, amount int64, currency string) string {
	t.Helper()
	return f.eventAt(t, eventType, createdAt, time.Now(), amount, currency, f.paymentID)
}

// eventAt also sets when WE received it (the evidence the deadline uses) and which
// payment it is about.
func (f *webhookFixture) eventAt(t *testing.T, eventType string, createdAt, receivedAt time.Time, amount int64, currency, paymentID string) string {
	t.Helper()
	status, captured := "authorized", false
	switch eventType {
	case "payment.captured", "order.paid":
		status, captured = "captured", true
	case "payment.failed":
		status = "failed"
	}
	eventID := "evt_it_" + uuid.New().String()
	body := fmt.Sprintf(`{"entity":"event","event":%q,"created_at":%d,"payload":{"payment":{"entity":{"id":%q,"order_id":%q,"amount":%d,"currency":%q,"status":%q,"captured":%t}}}}`,
		eventType, createdAt.Unix(), paymentID, f.gwOrderID, amount, currency, status, captured)
	gw := f.gwOrderID
	row := models.WebhookEvent{
		EventID: eventID, EventType: eventType, Body: []byte(body), Status: "PENDING",
		GatewayOrderID: &gw, ReceivedAt: receivedAt,
	}
	if err := f.db.Create(&row).Error; err != nil {
		t.Fatalf("insert event: %v", err)
	}
	f.eventIDs = append(f.eventIDs, eventID)
	return eventID
}

func (f *webhookFixture) eventRow(t *testing.T, eventID string) models.WebhookEvent {
	t.Helper()
	var row models.WebhookEvent
	if err := f.db.Where("event_id = ?", eventID).Take(&row).Error; err != nil {
		t.Fatalf("load event: %v", err)
	}
	return row
}

func newProcessor(t *testing.T, db *gorm.DB) *WebhookProcessor {
	t.Helper()
	processor, err := NewWebhookProcessor(db, WebhookProcessorConfig{
		Interval: time.Second, Batch: 50, Lease: 30 * time.Second, MaxAttempts: 3,
		BackoffBase: time.Hour, BackoffCap: time.Hour, PaymentWindow: webhookTestWindow,
		GatewayName: "razorpay",
	})
	if err != nil {
		t.Fatal(err)
	}
	return processor
}

func TestCapturedWebhookCompletesThePaymentOnceUnderConcurrentDiscovery(t *testing.T) {
	f := newWebhookFixture(t, "AWAITING_PAYMENT", 10*time.Minute)
	now := time.Now()
	captured := f.event(t, "payment.captured", now, 25000, "INR")
	orderPaid := f.event(t, "order.paid", now, 25000, "INR") // Razorpay sends both for one capture

	stale := f.attempt(t) // what a poller read before the webhook committed
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ { // two worker replicas
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := newProcessor(t, f.db).ProcessOnce(context.Background()); err != nil {
				t.Errorf("process: %v", err)
			}
		}()
	}
	wg.Add(1)
	go func() { // polling discovers the same capture from a stale read
		defer wg.Done()
		decision := decideReconcile(stale, paymentgateway.OrderState{
			GatewayOrderID: f.gwOrderID, CapturedPaymentID: f.paymentID, CapturedAmountPaise: 25000,
		}, true, time.Now(), webhookTestWindow)
		_ = f.db.Transaction(func(tx *gorm.DB) error {
			_, err := applyDecision(tx, stale, decision)
			return err
		})
	}()
	wg.Wait()
	// a straggler batch in case a replica claimed nothing the first time
	if err := newProcessor(t, f.db).ProcessOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	got := f.attempt(t)
	if got.Status != "SUCCEEDED" || got.GatewayPaymentID == nil || *got.GatewayPaymentID != f.paymentID {
		t.Fatalf("attempt = %s / %v", got.Status, got.GatewayPaymentID)
	}
	if replies := f.replies(t); len(replies) != 1 || replies[0] != "Succeeded" {
		t.Fatalf("saga replies = %v, want exactly one Succeeded", replies)
	}
	for _, id := range []string{captured, orderPaid} {
		if row := f.eventRow(t, id); row.Status != "PROCESSED" {
			t.Fatalf("event %s is %s", id, row.Status)
		}
	}
}

func TestOlderAuthorizedEventDoesNotRegressACapturedPayment(t *testing.T) {
	f := newWebhookFixture(t, "AWAITING_PAYMENT", 10*time.Minute)
	f.event(t, "payment.captured", time.Now(), 25000, "INR")
	if err := newProcessor(t, f.db).ProcessOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	late := f.event(t, "payment.authorized", time.Now().Add(-time.Minute), 25000, "INR")
	failed := f.event(t, "payment.failed", time.Now().Add(-time.Minute), 25000, "INR")
	if err := newProcessor(t, f.db).ProcessOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.attempt(t); got.Status != "SUCCEEDED" {
		t.Fatalf("status = %s, an older event must not regress a captured payment", got.Status)
	}
	if replies := f.replies(t); len(replies) != 1 {
		t.Fatalf("replies = %v, want exactly one", replies)
	}
	for _, id := range []string{late, failed} {
		if row := f.eventRow(t, id); row.Status != "PROCESSED" {
			t.Fatalf("event %s is %s", id, row.Status)
		}
	}
}

func TestFailedPaymentAttemptDoesNotFailTheCheckout(t *testing.T) {
	f := newWebhookFixture(t, "AWAITING_PAYMENT", 10*time.Minute)
	f.event(t, "payment.failed", time.Now(), 25000, "INR")
	if err := newProcessor(t, f.db).ProcessOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.attempt(t); got.Status != "AWAITING_PAYMENT" || len(f.replies(t)) != 0 {
		t.Fatalf("status=%s replies=%v: the customer may retry until the window ends", got.Status, f.replies(t))
	}
}

func TestEventBeforeTheGatewayOrderMappingIsRetriedNotDiscarded(t *testing.T) {
	f := newWebhookFixture(t, "UNKNOWN", 10*time.Minute)
	// the attempt exists but does not know the gateway order yet
	f.db.Exec("UPDATE payments.payment_attempts SET gateway_order_id = NULL WHERE order_id = ?", f.orderID)
	eventID := f.event(t, "payment.captured", time.Now(), 25000, "INR")

	processor := newProcessor(t, f.db)
	if err := processor.ProcessOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	row := f.eventRow(t, eventID)
	if row.Status != "PENDING" || row.Attempts != 1 || row.LastError == nil || !row.NextAttemptAt.After(time.Now().Add(30*time.Minute)) {
		t.Fatalf("event = %s attempts=%d error=%v next=%v, want PENDING with a backoff", row.Status, row.Attempts, row.LastError, row.NextAttemptAt)
	}
	if got := f.attempt(t); got.Status != "UNKNOWN" {
		t.Fatalf("status = %s, nothing may change before the mapping exists", got.Status)
	}

	// the mapping commits (what polling or the create call eventually does)
	f.db.Exec("UPDATE payments.payment_attempts SET gateway_order_id = ? WHERE order_id = ?", f.gwOrderID, f.orderID)
	f.db.Exec("UPDATE payments.webhook_events SET next_attempt_at = now() WHERE event_id = ?", eventID)
	if err := processor.ProcessOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.attempt(t); got.Status != "SUCCEEDED" {
		t.Fatalf("status = %s after the mapping appeared, want SUCCEEDED", got.Status)
	}
	if row := f.eventRow(t, eventID); row.Status != "PROCESSED" {
		t.Fatalf("event is %s", row.Status)
	}
}

func TestEventThatNeverMapsIsParkedAfterBoundedRetries(t *testing.T) {
	f := newWebhookFixture(t, "UNKNOWN", 10*time.Minute)
	f.db.Exec("UPDATE payments.payment_attempts SET gateway_order_id = NULL WHERE order_id = ?", f.orderID)
	eventID := f.event(t, "payment.captured", time.Now(), 25000, "INR")
	processor := newProcessor(t, f.db)
	for i := 0; i < 3; i++ { // MaxAttempts is 3
		f.db.Exec("UPDATE payments.webhook_events SET next_attempt_at = now() WHERE event_id = ? AND status = 'PENDING'", eventID)
		if err := processor.ProcessOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	row := f.eventRow(t, eventID)
	if row.Status != "PARKED" || row.LastError == nil || len(row.Body) == 0 {
		t.Fatalf("event = %s error=%v, want PARKED and kept (never deleted)", row.Status, row.LastError)
	}
}

func (f *webhookFixture) compensations(t *testing.T) map[string]string {
	t.Helper()
	var rows []struct {
		GatewayPaymentID string
		Status           string
	}
	if err := f.db.Table("payments.compensation_records").Select("gateway_payment_id, status").
		Where("order_id = ?", f.orderID).Scan(&rows).Error; err != nil {
		t.Fatalf("load compensation records: %v", err)
	}
	out := map[string]string{}
	for _, row := range rows {
		out[row.GatewayPaymentID] = row.Status
	}
	return out
}

func TestDeadlinePolicyUsesOurObservationTimeNotProviderTimestamps(t *testing.T) {
	t.Run("first seen after the deadline: expired, one Failed reply, compensation tracked", func(t *testing.T) {
		f := newWebhookFixture(t, "AWAITING_PAYMENT", -time.Minute) // deadline passed a minute ago
		f.event(t, "payment.captured", time.Now(), 25000, "INR")
		if err := newProcessor(t, f.db).ProcessOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		got := f.attempt(t)
		if got.Status != "FAILED" || got.FailureCode == nil || *got.FailureCode != "PAYMENT_EXPIRED" || got.GatewayPaymentID == nil {
			t.Fatalf("attempt = %s code=%v payment=%v", got.Status, got.FailureCode, got.GatewayPaymentID)
		}
		if replies := f.replies(t); len(replies) != 1 || replies[0] != "Failed" {
			t.Fatalf("replies = %v, want one Failed (the checkout is not resurrected)", replies)
		}
		if records := f.compensations(t); len(records) != 1 || records[f.paymentID] != "REQUIRED" {
			t.Fatalf("compensation records = %v, want the captured payment REQUIRED", records)
		}
		if got.CaptureSource == nil || *got.CaptureSource != "webhook" || got.CaptureObservedAt == nil {
			t.Fatalf("capture evidence not recorded: %v %v", got.CaptureSource, got.CaptureObservedAt)
		}
	})
	t.Run("received before the deadline but processed after it is still a success", func(t *testing.T) {
		f := newWebhookFixture(t, "AWAITING_PAYMENT", -time.Minute)
		f.eventAt(t, "payment.captured", time.Now(), time.Now().Add(-2*time.Minute), 25000, "INR", f.paymentID)
		if err := newProcessor(t, f.db).ProcessOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := f.attempt(t); got.Status != "SUCCEEDED" {
			t.Fatalf("status = %s, want SUCCEEDED (our receipt time was before the deadline)", got.Status)
		}
		if len(f.compensations(t)) != 0 {
			t.Fatal("an on-time capture owes no compensation")
		}
	})
	t.Run("a payload claiming an earlier capture does not rescue a late receipt", func(t *testing.T) {
		f := newWebhookFixture(t, "AWAITING_PAYMENT", -time.Minute)
		// created_at inside the payload says ten minutes ago; we received it just now.
		f.eventAt(t, "payment.captured", time.Now().Add(-10*time.Minute), time.Now(), 25000, "INR", f.paymentID)
		if err := newProcessor(t, f.db).ProcessOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := f.attempt(t); got.Status != "FAILED" || f.compensations(t)[f.paymentID] != "REQUIRED" {
			t.Fatalf("status = %s records=%v: provider timestamps must not extend the deadline", got.Status, f.compensations(t))
		}
	})
}

func TestCaptureNotifiedAfterCancellationDoesNotResurrectAndIsTracked(t *testing.T) {
	f := newWebhookFixture(t, "FAILED", -time.Hour)
	f.db.Exec("UPDATE payments.payment_attempts SET failure_code = 'PAYMENT_EXPIRED' WHERE order_id = ?", f.orderID)
	// The payload claims a capture long before the cancellation, and we even
	// received it before the original deadline: the attempt is already cancelled.
	f.eventAt(t, "payment.captured", time.Now().Add(-3*time.Hour), time.Now().Add(-2*time.Hour), 25000, "INR", f.paymentID)
	if err := newProcessor(t, f.db).ProcessOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := f.attempt(t)
	if got.Status != "FAILED" {
		t.Fatalf("attempt = %s, a cancelled checkout must stay cancelled", got.Status)
	}
	if len(f.replies(t)) != 0 {
		t.Fatalf("replies = %v, the saga already compensated", f.replies(t))
	}
	if records := f.compensations(t); records[f.paymentID] != "REQUIRED" {
		t.Fatalf("records = %v, the captured money must stay tracked", records)
	}
}

func TestAdditionalCapturesGetTheirOwnCompensation(t *testing.T) {
	t.Run("a different payment captured on a settled attempt is refunded; the funding payment never is", func(t *testing.T) {
		f := newWebhookFixture(t, "AWAITING_PAYMENT", 10*time.Minute)
		f.event(t, "payment.captured", time.Now(), 25000, "INR")
		if err := newProcessor(t, f.db).ProcessOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(f.compensations(t)) != 0 {
			t.Fatalf("the funding payment must have no compensation, got %v", f.compensations(t))
		}
		other := "pay_it_other_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:8]
		eventID := f.eventAt(t, "payment.captured", time.Now(), time.Now(), 25000, "INR", other)
		if err := newProcessor(t, f.db).ProcessOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		got := f.attempt(t)
		if got.Status != "SUCCEEDED" || *got.GatewayPaymentID != f.paymentID {
			t.Fatalf("the settled attempt must not change: %s %v", got.Status, got.GatewayPaymentID)
		}
		records := f.compensations(t)
		if len(records) != 1 || records[other] != "REQUIRED" {
			t.Fatalf("records = %v, want only the extra payment, eligible for refund", records)
		}
		if row := f.eventRow(t, eventID); row.Status != "PROCESSED" {
			t.Fatalf("event is %s", row.Status)
		}
		if len(f.replies(t)) != 1 {
			t.Fatalf("replies = %v, want only the original Succeeded", f.replies(t))
		}
	})
	t.Run("two captures on a cancelled checkout are refunded separately", func(t *testing.T) {
		f := newWebhookFixture(t, "AWAITING_PAYMENT", -time.Minute)
		f.event(t, "payment.captured", time.Now(), 25000, "INR") // late
		if err := newProcessor(t, f.db).ProcessOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		second := "pay_it_second_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:8]
		f.eventAt(t, "payment.captured", time.Now(), time.Now(), 25000, "INR", second)
		if err := newProcessor(t, f.db).ProcessOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		records := f.compensations(t)
		if records[f.paymentID] != "REQUIRED" || records[second] != "REQUIRED" || len(records) != 2 {
			t.Fatalf("records = %v, want both payments tracked as REQUIRED", records)
		}
	})
}

func TestParkedEventIsReplayedAfterTheMappingAppears(t *testing.T) {
	f := newWebhookFixture(t, "AWAITING_PAYMENT", 10*time.Minute)
	f.db.Exec("UPDATE payments.payment_attempts SET gateway_order_id = NULL WHERE order_id = ?", f.orderID)
	eventID := f.event(t, "payment.captured", time.Now(), 25000, "INR")
	processor := newProcessor(t, f.db)
	for i := 0; i < 3; i++ { // MaxAttempts is 3: the third failure parks it
		f.db.Exec("UPDATE payments.webhook_events SET next_attempt_at = now() WHERE event_id = ? AND status = 'PENDING'", eventID)
		if err := processor.ProcessOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	row := f.eventRow(t, eventID)
	if row.Status != "PARKED" || row.ParkReason == nil || *row.ParkReason != "NO_MAPPING" {
		t.Fatalf("event = %s park_reason=%v, want PARKED/NO_MAPPING", row.Status, row.ParkReason)
	}
	// While there is still no mapping a replay must not touch it.
	if n, err := processor.ReplayParked(context.Background()); err != nil || n != 0 {
		t.Fatalf("replay without a mapping moved %d events (err %v)", n, err)
	}

	f.db.Exec("UPDATE payments.payment_attempts SET gateway_order_id = ? WHERE order_id = ?", f.gwOrderID, f.orderID)
	if err := processor.ProcessOnce(context.Background()); err != nil { // replays, then applies
		t.Fatal(err)
	}
	if got := f.attempt(t); got.Status != "SUCCEEDED" {
		t.Fatalf("status = %s after the replay, want SUCCEEDED", got.Status)
	}
	if row := f.eventRow(t, eventID); row.Status != "PROCESSED" {
		t.Fatalf("event is %s", row.Status)
	}
	// Idempotent: nothing left to replay, and a second pass changes nothing.
	if n, err := processor.ReplayParked(context.Background()); err != nil || n != 0 {
		t.Fatalf("a second replay moved %d events (err %v)", n, err)
	}
	if replies := f.replies(t); len(replies) != 1 {
		t.Fatalf("replies = %v, want exactly one", replies)
	}
}

func TestOnlyNoMappingParkedEventsAreReplayed(t *testing.T) {
	f := newWebhookFixture(t, "AWAITING_PAYMENT", 10*time.Minute)
	eventID := f.event(t, "payment.captured", time.Now(), 25000, "INR")
	f.db.Exec("UPDATE payments.webhook_events SET status = 'PARKED', park_reason = 'MALFORMED' WHERE event_id = ?", eventID)
	if n, err := newProcessor(t, f.db).ReplayParked(context.Background()); err != nil || n != 0 {
		t.Fatalf("a MALFORMED event was replayed (%d, %v)", n, err)
	}
}

// The expiry decision and the webhook worker race for the same attempt, started
// together behind a barrier. Whoever commits first wins, and the other must leave
// one consistent outcome: never two replies, never captured money without a record.
func TestExpiryAndWebhookRaceLeaveOneConsistentOutcome(t *testing.T) {
	const trials = 12
	succeeded, failed := 0, 0
	for trial := 0; trial < trials; trial++ {
		// Comfortably separated times: the test database is slow, so a margin of
		// seconds would be eaten by the setup itself.
		f := newWebhookFixture(t, "AWAITING_PAYMENT", -time.Minute) // deadline a minute ago
		f.eventAt(t, "payment.captured", time.Now(), time.Now().Add(-10*time.Minute), 25000, "INR", f.paymentID)
		stale := f.attempt(t)
		expiry := decideReconcile(stale, paymentgateway.OrderState{GatewayOrderID: f.gwOrderID}, true, time.Now(), webhookTestWindow)
		if !expiry.expiry {
			t.Fatalf("setup: expected an expiry decision, got %+v", expiry)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			if err := newProcessor(t, f.db).ProcessOnce(context.Background()); err != nil {
				t.Errorf("process: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			_ = f.db.Transaction(func(tx *gorm.DB) error {
				_, err := applyDecision(tx, stale, expiry)
				return err
			})
		}()
		close(start)
		wg.Wait()
		// A straggler pass. If the concurrent pass hit a transient database error the
		// event is PENDING with a long backoff (the test configures an hour): making it
		// due again is exactly what the real retry would do later.
		f.db.Exec("UPDATE payments.webhook_events SET next_attempt_at = now(), locked_by = NULL, locked_until = NULL WHERE event_id IN ? AND status = 'PENDING'", f.eventIDs)
		_ = newProcessor(t, f.db).ProcessOnce(context.Background())

		got := f.attempt(t)
		replies := f.replies(t)
		records := f.compensations(t)
		switch got.Status {
		case "SUCCEEDED":
			succeeded++
			if len(replies) != 1 || replies[0] != "Succeeded" || len(records) != 0 {
				t.Fatalf("trial %d SUCCEEDED: replies=%v records=%v", trial, replies, records)
			}
		case "FAILED":
			failed++
			if len(replies) != 1 || replies[0] != "Failed" || records[f.paymentID] != "REQUIRED" {
				t.Fatalf("trial %d FAILED: replies=%v records=%v (captured money must be tracked)", trial, replies, records)
			}
		default:
			t.Fatalf("trial %d ended %s", trial, got.Status)
		}
	}
	t.Logf("race outcomes over %d trials: %d succeeded, %d expired-and-compensated", trials, succeeded, failed)
}

func TestExpiryIsDeferredWhileAnOnTimeCaptureWaitsInTheInbox(t *testing.T) {
	f := newWebhookFixture(t, "AWAITING_PAYMENT", -time.Minute)
	reconciler := &PaymentReconciler{db: f.db, config: ReconcilerConfig{PaymentWindow: webhookTestWindow}}
	attempt := f.attempt(t)

	onTime := f.eventAt(t, "payment.captured", time.Now(), time.Now().Add(-10*time.Minute), 25000, "INR", f.paymentID)
	if !reconciler.captureWaitingInInbox(context.Background(), attempt) {
		t.Fatal("a pending capture received before the deadline must defer expiry")
	}
	f.db.Exec("UPDATE payments.webhook_events SET status = 'PROCESSED' WHERE event_id = ?", onTime)
	if reconciler.captureWaitingInInbox(context.Background(), attempt) {
		t.Fatal("a processed event no longer waits")
	}
	f.eventAt(t, "payment.captured", time.Now(), time.Now(), 25000, "INR", f.paymentID) // received after the deadline
	if reconciler.captureWaitingInInbox(context.Background(), attempt) {
		t.Fatal("a capture received after the deadline must not defer expiry")
	}
}

func TestCapturedAmountOrCurrencyMismatchIsNotAccepted(t *testing.T) {
	for name, event := range map[string][2]any{"amount": {int64(24000), "INR"}, "currency": {int64(25000), "USD"}} {
		t.Run(name, func(t *testing.T) {
			f := newWebhookFixture(t, "AWAITING_PAYMENT", 10*time.Minute)
			f.event(t, "payment.captured", time.Now(), event[0].(int64), event[1].(string))
			if err := newProcessor(t, f.db).ProcessOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			got := f.attempt(t)
			if got.Status != "FAILED" || got.FailureCode == nil || *got.FailureCode != "AMOUNT_MISMATCH" ||
				f.compensations(t)[f.paymentID] != "REQUIRED" {
				t.Fatalf("attempt = %s code=%v records=%v", got.Status, got.FailureCode, f.compensations(t))
			}
		})
	}
}

func TestProcessedMarkAndEffectsCommitTogether(t *testing.T) {
	f := newWebhookFixture(t, "AWAITING_PAYMENT", 10*time.Minute)
	eventID := f.event(t, "payment.captured", time.Now(), 25000, "INR")
	// Another worker holds the lease: this worker must change nothing.
	f.db.Exec("UPDATE payments.webhook_events SET locked_by = 'someone-else', locked_until = now() + interval '1 minute' WHERE event_id = ?", eventID)
	processor := newProcessor(t, f.db)
	row := f.eventRow(t, eventID)
	processor.handle(context.Background(), row)
	if got := f.attempt(t); got.Status != "AWAITING_PAYMENT" || len(f.replies(t)) != 0 {
		t.Fatalf("a worker without the lease changed state: %s %v", got.Status, f.replies(t))
	}
	if after := f.eventRow(t, eventID); after.Status != "PENDING" {
		t.Fatalf("event = %s, want it left PENDING", after.Status)
	}
}

// Both interleavings of the same race, forced explicitly rather than hoped for:
// the decision to expire is taken from a stale read in each case.
func TestExpiryAndWebhookInEitherOrderAreConsistent(t *testing.T) {
	setup := func(t *testing.T) (*webhookFixture, models.PaymentAttempt, reconcileDecision) {
		f := newWebhookFixture(t, "AWAITING_PAYMENT", -time.Minute) // deadline a minute ago
		f.eventAt(t, "payment.captured", time.Now(), time.Now().Add(-10*time.Minute), 25000, "INR", f.paymentID)
		stale := f.attempt(t)
		expiry := decideReconcile(stale, paymentgateway.OrderState{GatewayOrderID: f.gwOrderID}, true, time.Now(), webhookTestWindow)
		if !expiry.expiry {
			t.Fatalf("setup: expected an expiry decision, got %+v", expiry)
		}
		return f, stale, expiry
	}
	applyExpiry := func(f *webhookFixture, stale models.PaymentAttempt, expiry reconcileDecision) {
		_ = f.db.Transaction(func(tx *gorm.DB) error {
			_, err := applyDecision(tx, stale, expiry)
			return err
		})
	}

	t.Run("webhook commits first: the on-time capture wins and the stale expiry is a no-op", func(t *testing.T) {
		f, stale, expiry := setup(t)
		if err := newProcessor(t, f.db).ProcessOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		applyExpiry(f, stale, expiry)
		got := f.attempt(t)
		if got.Status != "SUCCEEDED" || len(f.compensations(t)) != 0 {
			t.Fatalf("attempt=%s records=%v", got.Status, f.compensations(t))
		}
		if replies := f.replies(t); len(replies) != 1 || replies[0] != "Succeeded" {
			t.Fatalf("replies = %v, want one Succeeded and no contradictory Failed", replies)
		}
	})
	t.Run("expiry commits first: the checkout stays cancelled and the capture is compensated", func(t *testing.T) {
		f, stale, expiry := setup(t)
		applyExpiry(f, stale, expiry)
		if err := newProcessor(t, f.db).ProcessOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		got := f.attempt(t)
		if got.Status != "FAILED" {
			t.Fatalf("attempt=%s", got.Status)
		}
		if f.compensations(t)[f.paymentID] != "REQUIRED" {
			t.Fatalf("records = %v, want the capture tracked", f.compensations(t))
		}
		if replies := f.replies(t); len(replies) != 1 || replies[0] != "Failed" {
			t.Fatalf("replies = %v, want one Failed and no Succeeded", replies)
		}
	})
}
