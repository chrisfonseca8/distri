package admin

import (
	"context"
	"errors"
	"os"
	"testing"
	"uuid"

	"github.com/chrisfonseca08/distri/payment-service/pkg/config"
	"gorm.io/gorm"
)

func adminDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("RUN_ADMIN_INTEGRATION") != "1" {
		t.Skip("set RUN_ADMIN_INTEGRATION=1 to run database integration tests")
	}
	db, err := config.OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.RequireDedicatedTestDB(db); err != nil {
		t.Skip(err)
	}
	return db
}

func seedReview(t *testing.T, db *gorm.DB, refundID *string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := db.Exec(`INSERT INTO payments.compensation_records
		(id, payment_attempt_id, order_id, gateway, gateway_payment_id, amount_paise, currency, reason, status, review_reason, refund_id,
		 failure_count, retry_count, observed_at, source, created_at, updated_at)
		VALUES (?, ?, ?, 'razorpay-sim', ?, 5000, 'INR', 'LATE_CAPTURE', 'REVIEW', 'refund rejected 3 times', ?, 3, 2, now(), 'poll', now(), now())`,
		id.String(), uuid.New().String(), uuid.New().String(), "pay_adm_"+id.String()[:8], refundID).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM payments.compensation_events WHERE record_id = ?`, id.String())
		db.Exec(`DELETE FROM payments.compensation_records WHERE id = ?`, id.String())
	})
	return id
}

func TestRetryReviewRequiresANoteAndRefusesRecordsHoldingAProviderRefund(t *testing.T) {
	db := adminDB(t)
	ctx := context.Background()
	plain := seedReview(t, db, nil)
	refund := "rfnd_existing"
	withRefund := seedReview(t, db, &refund)

	if err := RetryReview(ctx, db, plain, "  "); err == nil {
		t.Fatal("a retry without a note must be refused")
	}
	if err := RetryReview(ctx, db, withRefund, "checked"); !errors.Is(err, ErrNotRetryable) {
		t.Fatalf("a record holding a provider refund = %v, want ErrNotRetryable (check the provider, do not repeat)", err)
	}

	if err := RetryReview(ctx, db, plain, "gateway key was rotated, retrying"); err != nil {
		t.Fatalf("retry: %v", err)
	}
	var row struct {
		Status       string
		ReviewReason *string
		FailureCount int
		Due          bool
	}
	db.Raw(`SELECT status, review_reason, failure_count, next_attempt_at <= now() AS due FROM payments.compensation_records WHERE id = ?`, plain.String()).Scan(&row)
	if row.Status != "REQUIRED" || row.ReviewReason != nil || row.FailureCount != 0 || !row.Due {
		t.Fatalf("record = %+v, want REQUIRED, no review reason, counters reset, due now", row)
	}
	history, err := History(ctx, db, plain)
	if err != nil || len(history) != 1 || history[0].FromStatus != "REVIEW" || history[0].ToStatus != "REQUIRED" {
		t.Fatalf("history = %+v, %v; want the operator decision recorded", history, err)
	}
	if err := RetryReview(ctx, db, plain, "again"); !errors.Is(err, ErrNotRetryable) {
		t.Fatalf("retrying a record that is no longer in review = %v, want ErrNotRetryable", err)
	}
	list, err := ListReview(ctx, db, 500)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range list {
		if record.ID == plain {
			t.Fatal("a retried record must leave the review list")
		}
	}
}

func TestParkedEventsAreListedAndReplayedOnce(t *testing.T) {
	db := adminDB(t)
	ctx := context.Background()
	eventID := "evt_adm_" + uuid.New().String()
	db.Exec(`INSERT INTO payments.webhook_events (event_id, event_type, body, status, park_reason, received_at, next_attempt_at, attempts)
		VALUES (?, 'payment.captured', '{}'::bytea, 'PARKED', 'NO_MAPPING', now(), now(), 12)`, eventID)
	t.Cleanup(func() { db.Exec(`DELETE FROM payments.webhook_events WHERE event_id = ?`, eventID) })

	parked, err := ListParked(ctx, db, 500)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range parked {
		found = found || event.EventID == eventID
	}
	if !found {
		t.Fatal("the parked event must be listed")
	}
	if err := ReplayParked(ctx, db, eventID); err != nil {
		t.Fatalf("replay: %v", err)
	}
	var status string
	var attempts int
	db.Raw(`SELECT status, attempts FROM payments.webhook_events WHERE event_id = ?`, eventID).Row().Scan(&status, &attempts)
	if status != "PENDING" || attempts != 0 {
		t.Fatalf("event = %s / %d attempts, want PENDING / 0", status, attempts)
	}
	if err := ReplayParked(ctx, db, eventID); !errors.Is(err, ErrNotParked) {
		t.Fatalf("replaying an event that is not parked = %v, want ErrNotParked", err)
	}
}
