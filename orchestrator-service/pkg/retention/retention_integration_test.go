package retention

import (
	"context"
	"os"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/config"
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

func TestPurgeRemovesOnlyExpiredBookkeepingInBatches(t *testing.T) {
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
		db.Exec(`DELETE FROM orchestrator.outbox WHERE routing_key = ?`, tag)
		db.Exec(`DELETE FROM orchestrator.processed_messages WHERE message_id LIKE ?`, tag+"%")
		db.Exec(`DELETE FROM orchestrator.checkout_attempts WHERE idempotency_key LIKE ?`, tag+"%")
		db.Exec(`DELETE FROM orchestrator.sagas WHERE currency = ?`, "RT"+tag[len(tag)-1:])
	})

	// outbox: 3 old published (needs 2 batches of 2), 1 recent published, 1 old unpublished
	for i := 0; i < 3; i++ {
		exec(`INSERT INTO orchestrator.outbox (message_id, routing_key, payload, published_at, created_at) VALUES (?, ?, '{}', ?, ?)`, uuid.New().String(), tag, old, old)
	}
	exec(`INSERT INTO orchestrator.outbox (message_id, routing_key, payload, published_at, created_at) VALUES (?, ?, '{}', ?, ?)`, uuid.New().String(), tag, recent, recent)
	exec(`INSERT INTO orchestrator.outbox (message_id, routing_key, payload, created_at) VALUES (?, ?, '{}', ?)`, uuid.New().String(), tag, old)
	// processed messages
	exec(`INSERT INTO orchestrator.processed_messages (message_id, processed_at) VALUES (?, ?), (?, ?)`, tag+"-old", old, tag+"-new", recent)
	// checkout attempts: old completed goes, old in progress stays, recent completed stays
	exec(`INSERT INTO orchestrator.checkout_attempts (idempotency_key, request_hash, status, order_id, response_code, response_body, created_at, updated_at)
		VALUES (?, 'h', 'completed', ?, 202, '{}', ?, ?), (?, 'h', 'in_progress', ?, NULL, NULL, ?, ?), (?, 'h', 'completed', ?, 202, '{}', ?, ?)`,
		tag+"-old-done", uuid.New().String(), old, old, tag+"-old-open", uuid.New().String(), old, old, tag+"-new-done", uuid.New().String(), recent, recent)
	// sagas: old COMPLETED goes (with its step), old STUCK stays
	currency := "RT" + tag[len(tag)-1:]
	doneID, stuckID := uuid.New(), uuid.New()
	exec(`INSERT INTO orchestrator.sagas (id, status, current_step, version, deadline_at, created_at, updated_at, total_paise, currency)
		VALUES (?, 'COMPLETED', 'CONFIRM_STOCK', 5, ?, ?, ?, 100, ?), (?, 'STUCK', 'CONFIRM_STOCK', 5, ?, ?, ?, 100, ?)`,
		doneID.String(), old, old, old, currency, stuckID.String(), old, old, old, currency)
	exec(`INSERT INTO orchestrator.saga_steps (saga_id, step, status, created_at, updated_at) VALUES (?, 'CONFIRM_STOCK', 'SUCCEEDED', ?, ?)`, doneID.String(), old, old)

	purger := New(db, Config{Interval: time.Hour, Batch: 2, Messages: 14 * 24 * time.Hour, Parked: 30 * 24 * time.Hour, Checkouts: 30 * 24 * time.Hour}, Rules())
	deleted := purger.RunOnce(context.Background())
	if deleted["outbox"] != 3 {
		t.Fatalf("deleted outbox rows = %d, want 3 (old published only, across two batches): %v", deleted["outbox"], deleted)
	}
	if got := count(t, db, `SELECT count(*) FROM orchestrator.outbox WHERE routing_key = ?`, tag); got != 2 {
		t.Fatalf("outbox rows left = %d, want 2 (recent published + old unpublished)", got)
	}
	if got := count(t, db, `SELECT count(*) FROM orchestrator.processed_messages WHERE message_id LIKE ?`, tag+"%"); got != 1 {
		t.Fatalf("processed rows left = %d, want 1", got)
	}
	if got := count(t, db, `SELECT count(*) FROM orchestrator.checkout_attempts WHERE idempotency_key LIKE ?`, tag+"%"); got != 2 {
		t.Fatalf("checkout attempts left = %d, want 2 (the old in-progress one and the recent one)", got)
	}
	if got := count(t, db, `SELECT count(*) FROM orchestrator.sagas WHERE id = ?`, doneID.String()); got != 0 {
		t.Fatal("an old COMPLETED saga should be purged")
	}
	if got := count(t, db, `SELECT count(*) FROM orchestrator.saga_steps WHERE saga_id = ?`, doneID.String()); got != 0 {
		t.Fatal("its steps should go with it (ON DELETE CASCADE)")
	}
	if got := count(t, db, `SELECT count(*) FROM orchestrator.sagas WHERE id = ?`, stuckID.String()); got != 1 {
		t.Fatal("a STUCK saga must never be purged: it waits for a person")
	}
}
