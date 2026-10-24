package services

import (
	"context"
	"errors"
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

// fakeProvider is a small model of the documented Razorpay refund contract: refunds
// are capped by the captured amount, a repeated X-Refund-Idempotency key replays the
// original, and a refund on a fully refunded payment is refused. Knobs inject faults.
type fakeProvider struct {
	mu       sync.Mutex
	payments map[string]*fakePayment
	refunds  []*fakeRefund
	byKey    map[string]*fakeRefund

	behavior      string // ok | lost (commit, then fail) | reject | timeout (fail, create nothing)
	initialStatus string // status of a newly created refund: pending | processed | failed
	forgetKeys    bool   // the idempotency key is not remembered (undocumented retention)
	hideRefunds   bool   // lookups do not show refunds yet (delayed visibility)

	submissions int
	perPayment  map[string]int
	fetches     map[string]int // payment ids whose data was read (for isolation checks)
}

type fakePayment struct {
	amount, refunded int64
	currency         string
}

type fakeRefund struct {
	id, paymentID, receipt, key, status string
	amount                              int64
	notes                               map[string]string
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{
		payments: map[string]*fakePayment{}, byKey: map[string]*fakeRefund{}, behavior: "ok",
		initialStatus: "processed", perPayment: map[string]int{}, fetches: map[string]int{},
	}
}

func (f *fakeProvider) addPayment(id string, amount int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.payments[id] = &fakePayment{amount: amount, currency: "INR"}
}

func (f *fakeProvider) CreateCharge(context.Context, uuid.UUID, int64) (string, error) {
	return "", errors.New("not used")
}

func (f *fakeProvider) Refund(_ context.Context, request paymentgateway.RefundRequest) (paymentgateway.RefundResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.submissions++
	f.perPayment[request.PaymentID]++
	switch f.behavior {
	case "reject":
		return paymentgateway.RefundResult{}, &paymentgateway.ErrRejected{Code: "BAD_REQUEST_ERROR", Description: "refund rejected"}
	case "timeout":
		return paymentgateway.RefundResult{}, errors.New("context deadline exceeded")
	}
	if existing, ok := f.byKey[request.IdempotencyKey]; ok && !f.forgetKeys && request.IdempotencyKey != "" {
		return existing.result(), nil
	}
	payment := f.payments[request.PaymentID]
	if payment == nil {
		return paymentgateway.RefundResult{}, &paymentgateway.ErrRejected{Description: "The id provided does not exist"}
	}
	remaining := payment.amount - payment.refunded
	if remaining <= 0 {
		return paymentgateway.RefundResult{}, paymentgateway.ErrFullyRefunded
	}
	if request.AmountPaise > remaining {
		return paymentgateway.RefundResult{}, &paymentgateway.ErrRejected{Description: "refund greater than the captured amount"}
	}
	refund := &fakeRefund{
		id: fmt.Sprintf("rfnd_%d", len(f.refunds)+1), paymentID: request.PaymentID, receipt: request.Receipt,
		key: request.IdempotencyKey, amount: request.AmountPaise, status: f.initialStatus, notes: request.Notes,
	}
	payment.refunded += request.AmountPaise
	f.refunds = append(f.refunds, refund)
	f.byKey[request.IdempotencyKey] = refund
	if f.behavior == "lost" {
		return paymentgateway.RefundResult{}, errors.New("read tcp: connection reset by peer")
	}
	return refund.result(), nil
}

func (r *fakeRefund) result() paymentgateway.RefundResult {
	return paymentgateway.RefundResult{ID: r.id, Status: r.status, PaymentID: r.paymentID, AmountPaise: r.amount, Receipt: r.receipt, Notes: r.notes}
}

func (f *fakeProvider) FetchRefund(_ context.Context, id string) (paymentgateway.RefundResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, refund := range f.refunds {
		if refund.id == id && !f.hideRefunds {
			return refund.result(), nil
		}
	}
	return paymentgateway.RefundResult{}, paymentgateway.ErrRefundNotFound
}

func (f *fakeProvider) ListPaymentRefunds(_ context.Context, paymentID string) ([]paymentgateway.RefundResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetches[paymentID]++
	var out []paymentgateway.RefundResult
	for _, refund := range f.refunds {
		if refund.paymentID == paymentID && !f.hideRefunds {
			out = append(out, refund.result())
		}
	}
	return out, nil
}

func (f *fakeProvider) FetchPayment(_ context.Context, id string) (paymentgateway.PaymentInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetches[id]++
	payment := f.payments[id]
	if payment == nil {
		return paymentgateway.PaymentInfo{}, paymentgateway.ErrPaymentNotFound
	}
	status := "captured"
	if payment.refunded >= payment.amount {
		status = "captured" // a fully refunded payment stays captured here; the amount tells
	}
	return paymentgateway.PaymentInfo{ID: id, Status: status, Currency: payment.currency, AmountPaise: payment.amount, AmountRefundedPaise: payment.refunded}, nil
}

func (f *fakeProvider) setStatus(refundID, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, refund := range f.refunds {
		if refund.id == refundID {
			if status == "failed" && refund.status != "failed" {
				f.payments[refund.paymentID].refunded -= refund.amount
			}
			refund.status = status
		}
	}
}

func (f *fakeProvider) totalRefunded(paymentID string) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.payments[paymentID].refunded
}

func (f *fakeProvider) refundCount(paymentID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, refund := range f.refunds {
		if refund.paymentID == paymentID {
			n++
		}
	}
	return n
}

func (f *fakeProvider) submissionsFor(paymentID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.perPayment[paymentID]
}

// ---- fixture ------------------------------------------------------------------

type compFixture struct {
	db      *gorm.DB
	gateway string
	attempt models.PaymentAttempt
}

// newCompFixture creates a FAILED attempt of the given gateway label, as left by an
// expired checkout.
func newCompFixture(t *testing.T, gateway string) *compFixture {
	t.Helper()
	db := openTestDB(t)
	f := &compFixture{db: db, gateway: gateway}
	gw := "order_cmp_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:12]
	f.attempt = models.PaymentAttempt{
		OrderID: uuid.New(), AmountPaise: 25000, Currency: "INR", Status: "FAILED", Gateway: gateway, GatewayOrderID: &gw,
	}
	if err := db.Create(&f.attempt).Error; err != nil {
		t.Fatalf("insert attempt: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM payments.compensation_events WHERE record_id IN (SELECT id FROM payments.compensation_records WHERE order_id = ?)", f.attempt.OrderID)
		db.Exec("DELETE FROM payments.compensation_records WHERE order_id = ?", f.attempt.OrderID)
		db.Exec("DELETE FROM payments.payment_attempts WHERE order_id = ?", f.attempt.OrderID)
	})
	return f
}

func (f *compFixture) record(t *testing.T, paymentID, status string) models.CompensationRecord {
	t.Helper()
	record := models.CompensationRecord{
		PaymentAttemptID: f.attempt.ID, OrderID: f.attempt.OrderID, Gateway: f.gateway, GatewayPaymentID: paymentID,
		AmountPaise: 25000, Currency: "INR", Reason: "LATE_CAPTURE", Status: status, ObservedAt: time.Now(), Source: "webhook",
	}
	if err := f.db.Create(&record).Error; err != nil {
		t.Fatalf("insert compensation: %v", err)
	}
	return record
}

func (f *compFixture) load(t *testing.T, id uuid.UUID) models.CompensationRecord {
	t.Helper()
	var record models.CompensationRecord
	if err := f.db.Where("id = ?", id).Take(&record).Error; err != nil {
		t.Fatalf("load compensation: %v", err)
	}
	return record
}

func (f *compFixture) history(t *testing.T, id uuid.UUID) []string {
	t.Helper()
	var rows []struct{ FromStatus, ToStatus string }
	f.db.Table("payments.compensation_events").Select("from_status, to_status").Where("record_id = ?", id).Order("at, id").Scan(&rows)
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = row.FromStatus + ">" + row.ToStatus
	}
	return out
}

func testCompConfig() CompensationConfig {
	return CompensationConfig{
		Interval: 10 * time.Millisecond, Batch: 20, Lease: 5 * time.Second, GatewayTimeout: time.Second,
		PendingPoll: time.Millisecond, AbsenceWindow: time.Hour, IdempotentRetryWindow: 2 * time.Hour, UnknownMaxAge: 3 * time.Hour,
		MaxIdempotentRetries: 2, MaxFailedRetries: 2, RetryDelay: time.Millisecond,
	}
}

func newCompWorker(t *testing.T, db *gorm.DB, provider *fakeProvider, gateway string, config CompensationConfig) *CompensationWorker {
	t.Helper()
	worker, err := NewCompensationWorker(db, provider, gateway, config)
	if err != nil {
		t.Fatal(err)
	}
	return worker
}

// ---- tests ------------------------------------------------------------------

func TestLateCaptureBecomesAConfirmedProcessedRefund(t *testing.T) {
	f := newCompFixture(t, "razorpay-sim")
	provider := newFakeProvider()
	provider.addPayment("pay_late", 25000)
	record := f.record(t, "pay_late", models.CompensationRequired)

	worker := newCompWorker(t, f.db, provider, "razorpay-sim", testCompConfig())
	worker.RunOnce(context.Background())

	got := f.load(t, record.ID)
	if got.Status != models.CompensationProcessed || got.RefundID == nil || got.ProviderStatus == nil || *got.ProviderStatus != "processed" {
		t.Fatalf("record = %s refund=%v provider=%v", got.Status, got.RefundID, got.ProviderStatus)
	}
	if provider.refundCount("pay_late") != 1 || provider.totalRefunded("pay_late") != 25000 {
		t.Fatalf("provider refunds=%d total=%d", provider.refundCount("pay_late"), provider.totalRefunded("pay_late"))
	}
	if got.OperationID == nil || provider.refunds[0].receipt != got.OperationID.String() ||
		provider.refunds[0].key != "cmp-"+got.OperationID.String() || provider.refunds[0].notes["operation_id"] != got.OperationID.String() {
		t.Fatalf("the stored operation id must be the receipt, notes and idempotency key: %+v / %+v", got.OperationID, provider.refunds[0])
	}
	history := f.history(t, record.ID)
	want := []string{"REQUIRED>SUBMITTING", "SUBMITTING>PROCESSED"}
	if len(history) != len(want) || history[0] != want[0] || history[1] != want[1] {
		t.Fatalf("history = %v, want %v", history, want)
	}
}

func TestPendingRefundStaysUnresolvedUntilProcessed(t *testing.T) {
	f := newCompFixture(t, "razorpay-sim")
	provider := newFakeProvider()
	provider.initialStatus = "pending"
	provider.addPayment("pay_pending", 25000)
	record := f.record(t, "pay_pending", models.CompensationRequired)
	worker := newCompWorker(t, f.db, provider, "razorpay-sim", testCompConfig())

	worker.RunOnce(context.Background())
	got := f.load(t, record.ID)
	if got.Status != models.CompensationPending || got.RefundID == nil {
		t.Fatalf("accepted-but-pending refund: record = %s refund=%v", got.Status, got.RefundID)
	}
	for i := 0; i < 3; i++ { // still pending at the provider: still unresolved, never "complete"
		f.db.Exec("UPDATE payments.compensation_records SET next_attempt_at = now() WHERE id = ?", record.ID)
		worker.RunOnce(context.Background())
		if got := f.load(t, record.ID); got.Status != models.CompensationPending {
			t.Fatalf("poll %d: record = %s, must stay PENDING while the provider says pending", i, got.Status)
		}
	}
	provider.setStatus(*got.RefundID, "processed")
	f.db.Exec("UPDATE payments.compensation_records SET next_attempt_at = now() WHERE id = ?", record.ID)
	worker.RunOnce(context.Background())
	if got := f.load(t, record.ID); got.Status != models.CompensationProcessed {
		t.Fatalf("record = %s after the provider processed it", got.Status)
	}
	if provider.submissionsFor("pay_pending") != 1 {
		t.Fatalf("submissions = %d, polling must never resubmit", provider.submissionsFor("pay_pending"))
	}
}

func TestLostSuccessfulResponseIsReconciledWithoutAnotherSubmission(t *testing.T) {
	f := newCompFixture(t, "razorpay-sim")
	provider := newFakeProvider()
	provider.behavior = "lost" // the refund is created, the response never arrives
	provider.initialStatus = "pending"
	provider.addPayment("pay_lost", 25000)
	record := f.record(t, "pay_lost", models.CompensationRequired)
	worker := newCompWorker(t, f.db, provider, "razorpay-sim", testCompConfig())

	worker.RunOnce(context.Background()) // submit, then reconcile in the same pass
	got := f.load(t, record.ID)
	if got.Status != models.CompensationPending || got.RefundID == nil {
		t.Fatalf("record = %s refund=%v, want the refund adopted by lookup", got.Status, got.RefundID)
	}
	if provider.submissionsFor("pay_lost") != 1 || provider.refundCount("pay_lost") != 1 {
		t.Fatalf("submissions=%d refunds=%d, a lost response must not cause a second submission", provider.submissionsFor("pay_lost"), provider.refundCount("pay_lost"))
	}
	history := strings.Join(f.history(t, record.ID), " ")
	if !strings.Contains(history, "SUBMITTING>UNKNOWN") || !strings.Contains(history, "UNKNOWN>PENDING") {
		t.Fatalf("the unknown outcome must be visible in the history: %s", history)
	}
}

func TestWorkerStoppingAtAnyPointRecoversSafely(t *testing.T) {
	op := func() *uuid.UUID { id := uuid.New(); return &id }
	expire := func(f *compFixture, id uuid.UUID) {
		f.db.Exec("UPDATE payments.compensation_records SET locked_until = now() - interval '1 minute', locked_by = 'dead-worker' WHERE id = ?", id)
	}

	t.Run("before the API request: nothing was sent, so it starts again and submits once", func(t *testing.T) {
		f := newCompFixture(t, "razorpay-sim")
		provider := newFakeProvider()
		provider.addPayment("pay_before", 25000)
		record := f.record(t, "pay_before", models.CompensationSubmitting)
		f.db.Exec("UPDATE payments.compensation_records SET operation_id = ? WHERE id = ?", op(), record.ID)
		expire(f, record.ID) // submitted_at stays NULL
		newCompWorker(t, f.db, provider, "razorpay-sim", testCompConfig()).RunOnce(context.Background())
		got := f.load(t, record.ID)
		if got.Status != models.CompensationProcessed || provider.submissionsFor("pay_before") != 1 || provider.refundCount("pay_before") != 1 {
			t.Fatalf("record=%s submissions=%d refunds=%d", got.Status, provider.submissionsFor("pay_before"), provider.refundCount("pay_before"))
		}
	})

	t.Run("after the gateway committed but before the response arrived: reconciled, never resent", func(t *testing.T) {
		f := newCompFixture(t, "razorpay-sim")
		provider := newFakeProvider()
		provider.initialStatus = "pending"
		provider.addPayment("pay_after", 25000)
		record := f.record(t, "pay_after", models.CompensationSubmitting)
		operation := op()
		f.db.Exec("UPDATE payments.compensation_records SET operation_id = ?, submitted_at = now(), amount_refunded_before = 0 WHERE id = ?", operation, record.ID)
		expire(f, record.ID)
		// the gateway had already accepted it under this operation id
		provider.Refund(context.Background(), paymentgateway.RefundRequest{
			PaymentID: "pay_after", AmountPaise: 25000, Receipt: operation.String(), IdempotencyKey: "cmp-" + operation.String(),
		})
		before := provider.submissionsFor("pay_after")
		newCompWorker(t, f.db, provider, "razorpay-sim", testCompConfig()).RunOnce(context.Background())
		got := f.load(t, record.ID)
		if got.Status != models.CompensationPending || got.RefundID == nil || provider.submissionsFor("pay_after") != before {
			t.Fatalf("record=%s refund=%v submissions %d->%d", got.Status, got.RefundID, before, provider.submissionsFor("pay_after"))
		}
		if provider.refundCount("pay_after") != 1 {
			t.Fatalf("refunds at the provider = %d, want 1", provider.refundCount("pay_after"))
		}
	})

	t.Run("while reconciling: a new worker simply continues a PENDING refund", func(t *testing.T) {
		f := newCompFixture(t, "razorpay-sim")
		provider := newFakeProvider()
		provider.initialStatus = "pending"
		provider.addPayment("pay_cont", 25000)
		record := f.record(t, "pay_cont", models.CompensationRequired)
		newCompWorker(t, f.db, provider, "razorpay-sim", testCompConfig()).RunOnce(context.Background())
		got := f.load(t, record.ID)
		provider.setStatus(*got.RefundID, "processed")
		f.db.Exec("UPDATE payments.compensation_records SET next_attempt_at = now() WHERE id = ?", record.ID)
		newCompWorker(t, f.db, provider, "razorpay-sim", testCompConfig()).RunOnce(context.Background()) // a different worker instance
		if got := f.load(t, record.ID); got.Status != models.CompensationProcessed || provider.submissionsFor("pay_cont") != 1 {
			t.Fatalf("record=%s submissions=%d", got.Status, provider.submissionsFor("pay_cont"))
		}
	})
}

func TestUnknownOutcomeIsNeverReadAsNoRefund(t *testing.T) {
	setup := func(t *testing.T, mutate func(*fakeProvider)) (*compFixture, *fakeProvider, models.CompensationRecord, *CompensationWorker) {
		f := newCompFixture(t, "razorpay-sim")
		provider := newFakeProvider()
		provider.addPayment("pay_unk", 25000)
		mutate(provider)
		record := f.record(t, "pay_unk", models.CompensationRequired)
		return f, provider, record, newCompWorker(t, f.db, provider, "razorpay-sim", testCompConfig())
	}

	t.Run("a timeout that created nothing waits through the absence window, then resubmits the SAME operation once", func(t *testing.T) {
		config := testCompConfig()
		config.AbsenceWindow = time.Minute
		f, provider, record, _ := setup(t, func(p *fakeProvider) { p.behavior = "timeout" })
		worker := newCompWorker(t, f.db, provider, "razorpay-sim", config)
		worker.RunOnce(context.Background())
		first := f.load(t, record.ID)
		if first.Status != models.CompensationUnknown {
			t.Fatalf("record = %s, a timeout must be UNKNOWN", first.Status)
		}
		worker.RunOnce(context.Background()) // inside the absence window: an empty lookup proves nothing
		if got := f.load(t, record.ID); got.Status != models.CompensationUnknown || provider.submissionsFor("pay_unk") != 1 {
			t.Fatalf("inside the window: record=%s submissions=%d", got.Status, provider.submissionsFor("pay_unk"))
		}
		provider.behavior = "ok"
		worker.now = func() time.Time { return time.Now().Add(5 * time.Minute) } // past the absence window
		worker.RunOnce(context.Background())
		got := f.load(t, record.ID)
		if got.Status != models.CompensationProcessed || got.RetryCount != 1 || *got.OperationID != *first.OperationID {
			t.Fatalf("record=%s retries=%d operation %v vs %v", got.Status, got.RetryCount, got.OperationID, first.OperationID)
		}
		if provider.refundCount("pay_unk") != 1 {
			t.Fatalf("refunds = %d, want exactly 1", provider.refundCount("pay_unk"))
		}
	})

	t.Run("a resubmission whose key the provider forgot cannot over-refund: the full-refund cap answers", func(t *testing.T) {
		config := testCompConfig()
		config.AbsenceWindow = time.Minute
		f, provider, record, _ := setup(t, func(p *fakeProvider) { p.behavior = "lost"; p.forgetKeys = true; p.hideRefunds = true })
		worker := newCompWorker(t, f.db, provider, "razorpay-sim", config)
		worker.RunOnce(context.Background()) // created at the provider, response lost, nothing visible
		if got := f.load(t, record.ID); got.Status != models.CompensationUnknown {
			t.Fatalf("record = %s", got.Status)
		}
		// amount_refunded rose even though no refund is visible: the evidence that one exists
		worker.now = func() time.Time { return time.Now().Add(5 * time.Minute) }
		worker.RunOnce(context.Background())
		if got := f.load(t, record.ID); got.Status != models.CompensationUnknown {
			t.Fatalf("record = %s: refunded money with no visible refund must stay unresolved, not retried", got.Status)
		}
		if provider.submissionsFor("pay_unk") != 1 || provider.totalRefunded("pay_unk") != 25000 {
			t.Fatalf("submissions=%d refunded=%d", provider.submissionsFor("pay_unk"), provider.totalRefunded("pay_unk"))
		}
		provider.mu.Lock()
		provider.hideRefunds = false // the refund becomes visible
		provider.mu.Unlock()
		worker.RunOnce(context.Background())
		if got := f.load(t, record.ID); got.Status != models.CompensationProcessed {
			t.Fatalf("record = %s once the refund became visible", got.Status)
		}
	})

	t.Run("nothing found for too long goes to review with an actionable reason", func(t *testing.T) {
		config := testCompConfig()
		config.AbsenceWindow, config.IdempotentRetryWindow, config.UnknownMaxAge = time.Minute, 2*time.Minute, 3*time.Minute
		f, provider, record, _ := setup(t, func(p *fakeProvider) { p.behavior = "timeout" })
		worker := newCompWorker(t, f.db, provider, "razorpay-sim", config)
		worker.RunOnce(context.Background())
		worker.now = func() time.Time { return time.Now().Add(10 * time.Minute) }
		worker.RunOnce(context.Background())
		got := f.load(t, record.ID)
		if got.Status != models.CompensationReview || got.ReviewReason == nil || !strings.Contains(*got.ReviewReason, "may or may not have reached") {
			t.Fatalf("record=%s reason=%v", got.Status, got.ReviewReason)
		}
	})
}

func TestDefinitiveFailureFollowsTheRecoveryPolicy(t *testing.T) {
	t.Run("a rejection is retried with a NEW operation a bounded number of times, then reviewed; history is kept", func(t *testing.T) {
		f := newCompFixture(t, "razorpay-sim")
		provider := newFakeProvider()
		provider.behavior = "reject"
		provider.addPayment("pay_rej", 25000)
		record := f.record(t, "pay_rej", models.CompensationRequired)
		worker := newCompWorker(t, f.db, provider, "razorpay-sim", testCompConfig())
		operations := map[uuid.UUID]bool{}
		for pass := 0; pass < 8; pass++ {
			f.db.Exec("UPDATE payments.compensation_records SET next_attempt_at = now() WHERE id = ? AND status IN ('FAILED','REQUIRED')", record.ID)
			worker.RunOnce(context.Background())
			if got := f.load(t, record.ID); got.OperationID != nil {
				operations[*got.OperationID] = true
			}
		}
		got := f.load(t, record.ID)
		if got.Status != models.CompensationReview || got.ReviewReason == nil || got.FailureCount != 3 {
			t.Fatalf("record=%s failures=%d reason=%v, want REVIEW after 3 rejections", got.Status, got.FailureCount, got.ReviewReason)
		}
		if len(operations) != 3 || provider.refundCount("pay_rej") != 0 {
			t.Fatalf("operations=%d refunds=%d, want 3 distinct operations and nothing created", len(operations), provider.refundCount("pay_rej"))
		}
		if !strings.Contains(strings.Join(f.history(t, record.ID), " "), "SUBMITTING>FAILED") {
			t.Fatalf("the rejections must stay visible in the history: %v", f.history(t, record.ID))
		}
	})
	t.Run("a refund the provider created and then failed goes straight to review", func(t *testing.T) {
		f := newCompFixture(t, "razorpay-sim")
		provider := newFakeProvider()
		provider.initialStatus = "pending"
		provider.addPayment("pay_pf", 25000)
		record := f.record(t, "pay_pf", models.CompensationRequired)
		worker := newCompWorker(t, f.db, provider, "razorpay-sim", testCompConfig())
		worker.RunOnce(context.Background())
		provider.setStatus(*f.load(t, record.ID).RefundID, "failed")
		f.db.Exec("UPDATE payments.compensation_records SET next_attempt_at = now() WHERE id = ?", record.ID)
		worker.RunOnce(context.Background())
		got := f.load(t, record.ID)
		if got.Status != models.CompensationReview || got.ReviewReason == nil || !strings.Contains(*got.ReviewReason, "failed") {
			t.Fatalf("record=%s reason=%v", got.Status, got.ReviewReason)
		}
		if provider.submissionsFor("pay_pf") != 1 {
			t.Fatal("a provider-failed refund must not be retried automatically")
		}
	})
}

func TestConcurrentWorkersCannotOverRefund(t *testing.T) {
	f := newCompFixture(t, "razorpay-sim")
	provider := newFakeProvider()
	provider.initialStatus = "pending"
	var records []models.CompensationRecord
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("pay_conc_%d", i)
		provider.addPayment(id, 25000)
		records = append(records, f.record(t, id, models.CompensationRequired))
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ { // eight workers racing for the same six records
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker := newCompWorker(t, f.db, provider, "razorpay-sim", testCompConfig())
			worker.RunOnce(context.Background())
		}()
	}
	wg.Wait()
	for i := range records {
		id := fmt.Sprintf("pay_conc_%d", i)
		if provider.submissionsFor(id) != 1 || provider.refundCount(id) != 1 || provider.totalRefunded(id) != 25000 {
			t.Fatalf("%s: submissions=%d refunds=%d refunded=%d, want exactly one refund of the captured amount",
				id, provider.submissionsFor(id), provider.refundCount(id), provider.totalRefunded(id))
		}
		if got := f.load(t, records[i].ID); got.Status != models.CompensationPending {
			t.Fatalf("%s record = %s", id, got.Status)
		}
	}
}

func TestOnlyEstablishedRecordsAreRefunded(t *testing.T) {
	t.Run("the payment that funded fulfilment is never refunded, whatever a record says", func(t *testing.T) {
		f := newCompFixture(t, "razorpay-sim")
		provider := newFakeProvider()
		provider.addPayment("pay_funding", 25000)
		provider.addPayment("pay_extra", 25000)
		f.db.Exec("UPDATE payments.payment_attempts SET status = 'SUCCEEDED', gateway_payment_id = 'pay_funding' WHERE id = ?", f.attempt.ID)
		funding := f.record(t, "pay_funding", models.CompensationRequired)
		extra := f.record(t, "pay_extra", models.CompensationRequired)
		newCompWorker(t, f.db, provider, "razorpay-sim", testCompConfig()).RunOnce(context.Background())
		if got := f.load(t, funding.ID); got.Status != models.CompensationReview || provider.submissionsFor("pay_funding") != 0 || provider.totalRefunded("pay_funding") != 0 {
			t.Fatalf("funding payment: record=%s submissions=%d", got.Status, provider.submissionsFor("pay_funding"))
		}
		if got := f.load(t, extra.ID); got.Status != models.CompensationProcessed || provider.totalRefunded("pay_extra") != 25000 {
			t.Fatalf("additional capture: record=%s refunded=%d", got.Status, provider.totalRefunded("pay_extra"))
		}
	})
	t.Run("a refund we did not create on the payment sends the record to review before any submission", func(t *testing.T) {
		f := newCompFixture(t, "razorpay-sim")
		provider := newFakeProvider()
		provider.addPayment("pay_ext", 25000)
		provider.payments["pay_ext"].refunded = 10000 // refunded by someone else
		record := f.record(t, "pay_ext", models.CompensationRequired)
		newCompWorker(t, f.db, provider, "razorpay-sim", testCompConfig()).RunOnce(context.Background())
		got := f.load(t, record.ID)
		if got.Status != models.CompensationReview || provider.submissionsFor("pay_ext") != 0 {
			t.Fatalf("record=%s submissions=%d", got.Status, provider.submissionsFor("pay_ext"))
		}
	})
	t.Run("old review records are classified: eligible only when everything is established", func(t *testing.T) {
		f := newCompFixture(t, "razorpay-sim")
		provider := newFakeProvider()
		provider.addPayment("pay_ok", 25000)
		good := f.record(t, "pay_ok", models.CompensationReview)
		bad := f.record(t, "pay_noamount", models.CompensationReview)
		f.db.Exec("UPDATE payments.compensation_records SET amount_paise = 0 WHERE id = ?", bad.ID)
		wrongEnv := f.record(t, "pay_wrongenv", models.CompensationReview)
		f.db.Exec("UPDATE payments.compensation_records SET gateway = 'razorpay' WHERE id = ?", wrongEnv.ID)
		newCompWorker(t, f.db, provider, "razorpay-sim", testCompConfig()).RunOnce(context.Background())
		if got := f.load(t, good.ID); got.Status != models.CompensationProcessed {
			t.Fatalf("established record = %s", got.Status)
		}
		if got := f.load(t, bad.ID); got.Status != models.CompensationReview || got.ReviewReason == nil || !strings.Contains(*got.ReviewReason, "amount") {
			t.Fatalf("record without an amount = %s reason=%v", got.Status, got.ReviewReason)
		}
		if got := f.load(t, wrongEnv.ID); got.Status != models.CompensationReview || got.ReviewReason != nil {
			t.Fatalf("a record of another environment must not even be classified by this worker: %s %v", got.Status, got.ReviewReason)
		}
	})
}

func TestRefundWebhooksAndPollingCauseOneBusinessEffect(t *testing.T) {
	f := newCompFixture(t, "razorpay-sim")
	provider := newFakeProvider()
	provider.initialStatus = "pending"
	provider.addPayment("pay_wh", 25000)
	record := f.record(t, "pay_wh", models.CompensationRequired)
	worker := newCompWorker(t, f.db, provider, "razorpay-sim", testCompConfig())
	worker.RunOnce(context.Background())
	pending := f.load(t, record.ID)

	processed := paymentgateway.RefundResult{ID: *pending.RefundID, Status: "processed", PaymentID: "pay_wh", Receipt: pending.OperationID.String()}
	apply := func(refund paymentgateway.RefundResult) string {
		var note string
		if err := f.db.Transaction(func(tx *gorm.DB) error {
			var err error
			note, err = worker.ApplyProviderRefund(tx, refund)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return note
	}
	// the processed event arrives twice, and polling discovers it as well
	apply(processed)
	apply(processed)
	provider.setStatus(*pending.RefundID, "processed")
	f.db.Exec("UPDATE payments.compensation_records SET next_attempt_at = now() WHERE id = ?", record.ID) // no-op: already PROCESSED
	worker.RunOnce(context.Background())
	// an older "pending" event delivered late must not regress it
	apply(paymentgateway.RefundResult{ID: *pending.RefundID, Status: "pending", PaymentID: "pay_wh", Receipt: pending.OperationID.String()})

	if got := f.load(t, record.ID); got.Status != models.CompensationProcessed {
		t.Fatalf("record = %s", got.Status)
	}
	processedMoves := 0
	for _, step := range f.history(t, record.ID) {
		if strings.HasSuffix(step, ">PROCESSED") {
			processedMoves++
		}
	}
	if processedMoves != 1 || provider.submissionsFor("pay_wh") != 1 {
		t.Fatalf("processed transitions=%d submissions=%d (history %v), want one of each", processedMoves, provider.submissionsFor("pay_wh"), f.history(t, record.ID))
	}

	t.Run("a webhook for a refund this workflow did not create goes to review, never adopted", func(t *testing.T) {
		other := newCompFixture(t, "razorpay-sim")
		unsubmitted := other.record(t, "pay_foreign", models.CompensationRequired)
		var note string
		_ = other.db.Transaction(func(tx *gorm.DB) error {
			note, _ = worker.ApplyProviderRefund(tx, paymentgateway.RefundResult{ID: "rfnd_foreign", Status: "processed", PaymentID: "pay_foreign", Receipt: "someone-else"})
			return nil
		})
		if got := other.load(t, unsubmitted.ID); got.Status != models.CompensationReview || !strings.Contains(note, "foreign") {
			t.Fatalf("record=%s note=%q", got.Status, note)
		}
	})
}

func TestGatewayEnvironmentsNeverCrossRoutes(t *testing.T) {
	realFixture := newCompFixture(t, "razorpay")
	simFixture := newCompFixture(t, "razorpay-sim")
	realProvider, simProvider := newFakeProvider(), newFakeProvider()
	realProvider.addPayment("pay_real", 25000)
	simProvider.addPayment("pay_sim", 25000)
	realRecord := realFixture.record(t, "pay_real", models.CompensationRequired)
	simRecord := simFixture.record(t, "pay_sim", models.CompensationRequired)

	realWorker := newCompWorker(t, realFixture.db, realProvider, "razorpay", testCompConfig())
	simWorker := newCompWorker(t, simFixture.db, simProvider, "razorpay-sim", testCompConfig())
	for i := 0; i < 3; i++ {
		realWorker.RunOnce(context.Background())
		simWorker.RunOnce(context.Background())
	}
	if realProvider.submissionsFor("pay_sim") != 0 || realProvider.fetches["pay_sim"] != 0 {
		t.Fatalf("the real-API worker touched simulator money: %d submissions, %d reads", realProvider.submissionsFor("pay_sim"), realProvider.fetches["pay_sim"])
	}
	if simProvider.submissionsFor("pay_real") != 0 || simProvider.fetches["pay_real"] != 0 {
		t.Fatalf("the simulator worker touched real money: %d submissions, %d reads", simProvider.submissionsFor("pay_real"), simProvider.fetches["pay_real"])
	}
	if realFixture.load(t, realRecord.ID).Status != models.CompensationProcessed || simFixture.load(t, simRecord.ID).Status != models.CompensationProcessed {
		t.Fatal("each worker must still complete the work of its own environment")
	}

	t.Run("payment reconciliation and the settled-attempt audit are routed by gateway label too", func(t *testing.T) {
		db := openTestDB(t)
		insert := func(gateway string) models.PaymentAttempt {
			gw := "order_iso_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:12]
			expires := time.Now().Add(time.Minute)
			attempt := models.PaymentAttempt{OrderID: uuid.New(), AmountPaise: 100, Currency: "INR", Status: "AWAITING_PAYMENT",
				Gateway: gateway, GatewayOrderID: &gw, ExpiresAt: &expires}
			if err := db.Create(&attempt).Error; err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Exec("DELETE FROM payments.payment_attempts WHERE id = ?", attempt.ID) })
			return attempt
		}
		simAttempt, realAttempt := insert("razorpay-sim"), insert("razorpay")
		settledSim := insert("razorpay-sim")
		db.Exec("UPDATE payments.payment_attempts SET status = 'SUCCEEDED', updated_at = now() WHERE id = ?", settledSim.ID)

		lookups := &countingLookup{seen: map[uuid.UUID]int{}}
		reconciler, err := NewPaymentReconciler(db, lookups, "razorpay", ReconcilerConfig{
			Interval: time.Second, MinPoll: 0, Batch: 500, PaymentWindow: time.Minute,
			AuditWindow: time.Hour, AuditInterval: time.Millisecond, GatewayTimeout: time.Second,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := reconciler.ReconcileOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		if lookups.seen[simAttempt.OrderID] != 0 || lookups.seen[settledSim.OrderID] != 0 {
			t.Fatal("the real-API reconciler looked up simulator attempts")
		}
		if lookups.seen[realAttempt.OrderID] == 0 {
			t.Fatal("the real-API reconciler must still look up its own attempts")
		}
	})

	t.Run("startup refuses unsettled work with an unknown gateway label", func(t *testing.T) {
		db := openTestDB(t)
		gw := "order_lbl_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:12]
		attempt := models.PaymentAttempt{OrderID: uuid.New(), AmountPaise: 100, Currency: "INR", Status: "AWAITING_PAYMENT", Gateway: "razorpay-staging", GatewayOrderID: &gw}
		if err := db.Create(&attempt).Error; err != nil {
			t.Fatal(err)
		}
		defer db.Exec("DELETE FROM payments.payment_attempts WHERE id = ?", attempt.ID)
		if err := ValidateGatewayLabels(context.Background(), db, "razorpay-sim"); err == nil || !strings.Contains(err.Error(), "razorpay-staging") {
			t.Fatalf("err = %v, want a refusal naming the unknown label", err)
		}
		db.Exec("UPDATE payments.payment_attempts SET status = 'FAILED' WHERE id = ?", attempt.ID)
		if err := ValidateGatewayLabels(context.Background(), db, "razorpay-sim"); err != nil {
			t.Fatalf("settled work with an unknown label does not block startup: %v", err)
		}
		if err := ValidateGatewayLabels(context.Background(), db, "razorpay-typo"); err == nil {
			t.Fatal("an unknown own environment must be rejected")
		}
	})
}

// countingLookup records which orders a reconciler asked about.
type countingLookup struct {
	mu   sync.Mutex
	seen map[uuid.UUID]int
}

func (c *countingLookup) CreateCharge(context.Context, uuid.UUID, int64) (string, error) {
	return "", nil
}

func (c *countingLookup) LookupOrder(_ context.Context, orderID uuid.UUID) (paymentgateway.OrderState, error) {
	c.mu.Lock()
	c.seen[orderID]++
	c.mu.Unlock()
	return paymentgateway.OrderState{}, paymentgateway.ErrOrderNotFound
}

func TestSettledAttemptAuditFindsAnExtraCaptureOnlyInsideTheWindow(t *testing.T) {
	db := openTestDB(t)
	mk := func(age time.Duration) models.PaymentAttempt {
		gw := "order_aud_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:12]
		pay := "pay_funding_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:8]
		attempt := models.PaymentAttempt{OrderID: uuid.New(), AmountPaise: 25000, Currency: "INR", Status: "SUCCEEDED",
			Gateway: "razorpay-sim", GatewayOrderID: &gw, GatewayPaymentID: &pay}
		if err := db.Create(&attempt).Error; err != nil {
			t.Fatal(err)
		}
		db.Exec("UPDATE payments.payment_attempts SET updated_at = ? WHERE id = ?", time.Now().Add(-age), attempt.ID)
		t.Cleanup(func() {
			db.Exec("DELETE FROM payments.compensation_events WHERE record_id IN (SELECT id FROM payments.compensation_records WHERE order_id = ?)", attempt.OrderID)
			db.Exec("DELETE FROM payments.compensation_records WHERE order_id = ?", attempt.OrderID)
			db.Exec("DELETE FROM payments.payment_attempts WHERE id = ?", attempt.ID)
		})
		return attempt
	}
	recent, old := mk(5*time.Minute), mk(3*time.Hour)
	lookup := &extraCaptureLookup{extras: map[uuid.UUID]paymentgateway.OrderState{}, seen: map[uuid.UUID]int{}}
	for _, attempt := range []models.PaymentAttempt{recent, old} {
		lookup.extras[attempt.OrderID] = paymentgateway.OrderState{
			GatewayOrderID: *attempt.GatewayOrderID, AmountPaise: 25000,
			CapturedPaymentID: *attempt.GatewayPaymentID, CapturedAmountPaise: 25000, CapturedCurrency: "INR",
			ExtraCaptured: []paymentgateway.CapturedPayment{{PaymentID: "pay_extra_" + attempt.OrderID.String()[:8], AmountPaise: 25000, Currency: "INR"}},
		}
	}
	reconciler, err := NewPaymentReconciler(db, lookup, "razorpay-sim", ReconcilerConfig{
		Interval: time.Second, MinPoll: 0, Batch: 500, PaymentWindow: time.Minute,
		AuditWindow: time.Hour, AuditInterval: time.Millisecond, GatewayTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // the second pass must find nothing new
		if err := reconciler.ReconcileOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	count := func(a models.PaymentAttempt) int64 {
		var n int64
		db.Table("payments.compensation_records").Where("order_id = ?", a.OrderID).Count(&n)
		return n
	}
	if count(recent) != 1 {
		t.Fatalf("recently settled attempt: %d compensation records, want exactly 1 (found once, deduplicated)", count(recent))
	}
	if count(old) != 0 || lookup.seen[old.OrderID] != 0 {
		t.Fatalf("an attempt settled outside the audit window must not be polled (records=%d lookups=%d)", count(old), lookup.seen[old.OrderID])
	}
	var funding int64
	db.Table("payments.compensation_records").Where("order_id = ? AND gateway_payment_id = ?", recent.OrderID, *recent.GatewayPaymentID).Count(&funding)
	if funding != 0 {
		t.Fatal("the funding payment must never get a compensation record")
	}
}

type extraCaptureLookup struct {
	mu     sync.Mutex
	extras map[uuid.UUID]paymentgateway.OrderState
	seen   map[uuid.UUID]int
}

func (e *extraCaptureLookup) CreateCharge(context.Context, uuid.UUID, int64) (string, error) {
	return "", nil
}

func (e *extraCaptureLookup) LookupOrder(_ context.Context, orderID uuid.UUID) (paymentgateway.OrderState, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seen[orderID]++
	return e.extras[orderID], nil
}
