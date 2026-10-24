package services

import (
	"context"
	"encoding/json"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/payment-service/database/models"
	"github.com/chrisfonseca08/distri/payment-service/pkg/messages"
	"github.com/chrisfonseca08/distri/payment-service/pkg/messaging"
)

func refundCommand(orderID uuid.UUID) messaging.Delivery {
	body, _ := json.Marshal(messages.PaymentRefund{OrderID: orderID})
	return messaging.Delivery{MessageId: "refund-test-" + uuid.New().String(), RoutingKey: messages.PaymentRefundRoutingKey, Body: body}
}

func stepReplies(t *testing.T, f *compFixture) []string {
	t.Helper()
	var rows []string
	f.db.Raw(`SELECT payload->>'step' || ':' || (payload->>'outcome') FROM payments.outbox WHERE payload->>'order_id' = ? ORDER BY created_at`,
		f.attempt.OrderID.String()).Scan(&rows)
	return rows
}

func settleAttempt(t *testing.T, f *compFixture, status, paymentID string) {
	t.Helper()
	if err := f.db.Exec(`UPDATE payments.payment_attempts SET status = ?, gateway_payment_id = NULLIF(?, '') WHERE id = ?`,
		status, paymentID, f.attempt.ID.String()).Error; err != nil {
		t.Fatalf("settle attempt: %v", err)
	}
	t.Cleanup(func() {
		f.db.Exec(`DELETE FROM payments.outbox WHERE payload->>'order_id' = ?`, f.attempt.OrderID.String())
		f.db.Exec(`DELETE FROM payments.processed_messages WHERE message_id LIKE 'refund-test-%'`)
	})
}

// The saga asks for the refund of the payment that funded a checkout it could not complete.
func TestRefundCommandRecordsTheFundingPaymentOnceAndTheWorkerRefundsItExactlyOnce(t *testing.T) {
	f := newCompFixture(t, "razorpay-sim")
	settleAttempt(t, f, "SUCCEEDED", "pay_fund")
	handler := NewPaymentRefundHandler(f.db)

	if err := handler.Handle(context.Background(), refundCommand(f.attempt.OrderID)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	// the same order again under a new message id, and the very same message again
	repeat := refundCommand(f.attempt.OrderID)
	if err := handler.Handle(context.Background(), repeat); err != nil {
		t.Fatal(err)
	}
	if err := handler.Handle(context.Background(), repeat); err != nil {
		t.Fatal(err)
	}

	var records []models.CompensationRecord
	f.db.Where("order_id = ?", f.attempt.OrderID).Find(&records)
	if len(records) != 1 || records[0].Status != models.CompensationRequired || records[0].Reason != reasonCheckoutFailedAfterPayment ||
		records[0].GatewayPaymentID != "pay_fund" || records[0].AmountPaise != 25000 || records[0].Source != "saga" {
		t.Fatalf("records = %+v, want exactly one REQUIRED CHECKOUT_FAILED_AFTER_PAYMENT record for pay_fund", records)
	}
	if got := stepReplies(t, f); len(got) != 2 || got[0] != "REFUND_PAYMENT:Succeeded" || got[1] != "REFUND_PAYMENT:Succeeded" {
		t.Fatalf("replies = %v, want one per distinct message (a repeated message sends none)", got)
	}

	provider := newFakeProvider()
	provider.addPayment("pay_fund", 25000)
	newCompWorker(t, f.db, provider, "razorpay-sim", testCompConfig()).RunOnce(context.Background())
	got := f.load(t, records[0].ID)
	if got.Status != models.CompensationProcessed || provider.refundCount("pay_fund") != 1 || provider.totalRefunded("pay_fund") != 25000 {
		t.Fatalf("record %s, provider refunds=%d total=%d; want PROCESSED and exactly one full refund", got.Status, provider.refundCount("pay_fund"), provider.totalRefunded("pay_fund"))
	}
}

// Only the saga's explicit request lets the funding payment be refunded: a polling finding never does.
func TestTheFundingPaymentIsNeverRefundedForAnyOtherReason(t *testing.T) {
	f := newCompFixture(t, "razorpay-sim")
	settleAttempt(t, f, "SUCCEEDED", "pay_fund")
	record := f.record(t, "pay_fund", models.CompensationRequired) // reason LATE_CAPTURE
	provider := newFakeProvider()
	provider.addPayment("pay_fund", 25000)
	newCompWorker(t, f.db, provider, "razorpay-sim", testCompConfig()).RunOnce(context.Background())
	got := f.load(t, record.ID)
	if got.Status != models.CompensationReview || provider.submissionsFor("pay_fund") != 0 {
		t.Fatalf("record %s, submissions=%d; the funding payment must go to review without any refund", got.Status, provider.submissionsFor("pay_fund"))
	}
}

func TestRefundCommandAnswersByWhatIsKnownAboutThePayment(t *testing.T) {
	cases := []struct {
		name, status, payment string
		wantReply             string
		wantRecords           int
	}{
		{"payment failed: nothing to give back", "FAILED", "", "REFUND_PAYMENT:Succeeded", 0},
		{"payment not settled: do not guess", "AWAITING_PAYMENT", "", "REFUND_PAYMENT:Failed", 0},
		{"payment unknown: do not guess", "UNKNOWN", "", "REFUND_PAYMENT:Failed", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newCompFixture(t, "razorpay-sim")
			settleAttempt(t, f, c.status, c.payment)
			if err := NewPaymentRefundHandler(f.db).Handle(context.Background(), refundCommand(f.attempt.OrderID)); err != nil {
				t.Fatal(err)
			}
			if got := stepReplies(t, f); len(got) != 1 || got[0] != c.wantReply {
				t.Fatalf("replies = %v, want [%s]", got, c.wantReply)
			}
			var n int64
			f.db.Raw(`SELECT count(*) FROM payments.compensation_records WHERE order_id = ?`, f.attempt.OrderID.String()).Scan(&n)
			if int(n) != c.wantRecords {
				t.Fatalf("records = %d, want %d", n, c.wantRecords)
			}
		})
	}
	// an order that has no payment attempt at all
	f := newCompFixture(t, "razorpay-sim")
	f.db.Exec(`DELETE FROM payments.payment_attempts WHERE id = ?`, f.attempt.ID.String())
	if err := NewPaymentRefundHandler(f.db).Handle(context.Background(), refundCommand(f.attempt.OrderID)); err != nil {
		t.Fatal(err)
	}
	if got := stepReplies(t, f); len(got) != 1 || got[0] != "REFUND_PAYMENT:Failed" {
		t.Fatalf("replies = %v, want a Failed answer for an order with no payment", got)
	}
	t.Cleanup(func() {
		f.db.Exec(`DELETE FROM payments.outbox WHERE payload->>'order_id' = ?`, f.attempt.OrderID.String())
	})
}

func TestTheDispatcherRoutesByRoutingKeyAndRejectsOthers(t *testing.T) {
	handler := NewPaymentCommandHandler(NewPaymentInitiateHandler(nil), NewPaymentRefundHandler(nil))
	err := handler.Handle(context.Background(), messaging.Delivery{RoutingKey: "payment.nonsense", MessageId: "x", Body: []byte(`{}`)})
	var permanent *messaging.PermanentError
	if err == nil || !asPermanent(err, &permanent) {
		t.Fatalf("an unknown routing key must be a permanent failure, got %v", err)
	}
}

func asPermanent(err error, target **messaging.PermanentError) bool {
	for err != nil {
		if p, ok := err.(*messaging.PermanentError); ok {
			*target = p
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// ---- bounded automatic retry of a refund the gateway kept rejecting ----

func TestRejectedRefundInReviewIsRetriedAutomaticallyAFewTimesThenLeftToAPerson(t *testing.T) {
	f := newCompFixture(t, "razorpay-sim")
	config := testCompConfig()
	config.ReviewRetryAfter, config.MaxReviewRetries = time.Minute, 2
	provider := newFakeProvider()
	provider.addPayment("pay_rej", 25000)
	worker := newCompWorker(t, f.db, provider, "razorpay-sim", config)

	seed := func(reason string, refundID *string, retries int, age time.Duration) models.CompensationRecord {
		record := f.record(t, "pay_"+uuid.New().String()[:8], models.CompensationReview)
		f.db.Exec(`UPDATE payments.compensation_records SET review_reason = ?, refund_id = ?, review_retries = ?, updated_at = ? WHERE id = ?`,
			reason, refundID, retries, time.Now().Add(-age), record.ID.String())
		return record
	}
	rejected := seed("refund rejected 3 times: BAD_REQUEST_ERROR", nil, 0, 5*time.Minute)
	exhausted := seed("refund rejected 3 times: BAD_REQUEST_ERROR", nil, 2, 5*time.Minute)
	tooRecent := seed("refund rejected 3 times: BAD_REQUEST_ERROR", nil, 0, 10*time.Second)
	unknown := seed("no refund carrying operation x found after 10m; the request may or may not have reached the gateway", nil, 0, 5*time.Minute)
	refundID := "rfnd_held"
	holdsRefund := seed("refund rejected 3 times: BAD_REQUEST_ERROR", &refundID, 0, 5*time.Minute)

	if err := worker.retryRejectedReviews(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := func(id uuid.UUID) (string, int) {
		r := f.load(t, id)
		return r.Status, r.ReviewRetries
	}
	if s, n := status(rejected.ID); s != models.CompensationRequired || n != 1 {
		t.Fatalf("a definitively rejected refund = %s after %d retries, want REQUIRED and 1", s, n)
	}
	for name, id := range map[string]uuid.UUID{"retries used up": exhausted.ID, "too recent": tooRecent.ID, "unknown outcome": unknown.ID, "holds a provider refund": holdsRefund.ID} {
		if s, _ := status(id); s != models.CompensationReview {
			t.Fatalf("%s: status %s, want it left in REVIEW for a person", name, s)
		}
	}
	got := f.load(t, rejected.ID)
	if got.ReviewReason != nil || got.FailureCount != 0 {
		t.Fatalf("a retried record starts clean: %+v", got)
	}
	if history := f.history(t, rejected.ID); len(history) == 0 || history[len(history)-1] != "REVIEW>REQUIRED" {
		t.Fatalf("history = %v, want the automatic retry recorded", history)
	}
}
