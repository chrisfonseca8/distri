package retention

import (
	"context"
	"os"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/payment-service/pkg/config"
	"gorm.io/gorm"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("RUN_RETENTION_INTEGRATION") != "1" {
		t.Skip("set RUN_RETENTION_INTEGRATION=1 to run database integration tests")
	}
	db, err := config.OpenDB()
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := config.RequireDedicatedTestDB(db); err != nil {
		t.Skip(err)
	}
	return db
}

func count(t *testing.T, db *gorm.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.Raw(query, args...).Scan(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestPurgeKeepsFinancialRecordsAndUnfinishedWork(t *testing.T) {
	db := testDB(t)
	tag := "ret-" + uuid.New().String()
	old := time.Now().Add(-40 * 24 * time.Hour)
	recent := time.Now().Add(-1 * time.Hour)
	exec := func(query string, args ...any) {
		t.Helper()
		if err := db.Exec(query, args...).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM payments.outbox WHERE routing_key = ?`, tag)
		db.Exec(`DELETE FROM payments.processed_messages WHERE message_id LIKE ?`, tag+"%")
		db.Exec(`DELETE FROM payments.webhook_events WHERE event_id LIKE ?`, tag+"%")
		db.Exec(`DELETE FROM payments.jobs WHERE last_error = ?`, tag)
	})

	exec(`INSERT INTO payments.outbox (message_id, routing_key, payload, published_at, created_at) VALUES (?, ?, '{}', ?, ?), (?, ?, '{}', NULL, ?)`,
		uuid.New().String(), tag, old, old, uuid.New().String(), tag, old)
	exec(`INSERT INTO payments.processed_messages (message_id, processed_at) VALUES (?, ?), (?, ?)`, tag+"-old", old, tag+"-new", recent)
	// webhook events: processed old goes, pending old stays, parked old goes (30 d rule), parked recent stays
	for _, row := range []struct {
		id, status string
		at         time.Time
	}{{"-done-old", "PROCESSED", old}, {"-pending-old", "PENDING", old}, {"-parked-old", "PARKED", old}, {"-parked-new", "PARKED", recent}, {"-done-new", "PROCESSED", recent}} {
		exec(`INSERT INTO payments.webhook_events (event_id, event_type, body, status, received_at, next_attempt_at) VALUES (?, 'payment.captured', '{}'::bytea, ?, ?, ?)`,
			tag+row.id, row.status, row.at, row.at)
	}
	exec(`INSERT INTO payments.jobs (type, payment_id, status, last_error, created_at, updated_at, next_run_at) VALUES
		('process_payment', ?, 'succeeded', ?, ?, ?, ?), ('process_payment', ?, 'dead', ?, ?, ?, ?)`,
		uuid.New().String(), tag, old, old, old, uuid.New().String(), tag, old, old, old)

	purger := New(db, Config{Interval: time.Hour, Batch: 2, Messages: 14 * 24 * time.Hour, Parked: 30 * 24 * time.Hour, Checkouts: 30 * 24 * time.Hour}, Rules())
	purger.RunOnce(context.Background())

	if got := count(t, db, `SELECT count(*) FROM payments.outbox WHERE routing_key = ?`, tag); got != 1 {
		t.Fatalf("outbox left = %d, want 1 (the unpublished one)", got)
	}
	if got := count(t, db, `SELECT count(*) FROM payments.processed_messages WHERE message_id LIKE ?`, tag+"%"); got != 1 {
		t.Fatalf("processed left = %d, want 1", got)
	}
	left := count(t, db, `SELECT count(*) FROM payments.webhook_events WHERE event_id LIKE ?`, tag+"%")
	if left != 3 {
		t.Fatalf("webhook events left = %d, want 3 (pending old, parked recent, processed recent)", left)
	}
	if got := count(t, db, `SELECT count(*) FROM payments.jobs WHERE last_error = ? AND status = 'dead'`, tag); got != 1 {
		t.Fatal("a dead job must never be purged: it waits for a person")
	}
	if got := count(t, db, `SELECT count(*) FROM payments.jobs WHERE last_error = ? AND status = 'succeeded'`, tag); got != 0 {
		t.Fatal("an old succeeded job should be purged")
	}
}
