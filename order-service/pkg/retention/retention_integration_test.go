package retention

import (
	"context"
	"os"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/order-service/pkg/config"
)

func TestPurgeRemovesOnlyExpiredMessageBookkeeping(t *testing.T) {
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
	tag := "ret-" + uuid.New().String()
	old := time.Now().Add(-40 * 24 * time.Hour)
	recent := time.Now().Add(-1 * time.Hour)
	t.Cleanup(func() {
		db.Exec(`DELETE FROM orders.outbox WHERE routing_key = ?`, tag)
		db.Exec(`DELETE FROM orders.processed_messages WHERE message_id LIKE ?`, tag+"%")
	})
	for _, query := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO orders.outbox (message_id, routing_key, payload, published_at, created_at) VALUES (?, ?, '{}', ?, ?), (?, ?, '{}', ?, ?), (?, ?, '{}', NULL, ?)`,
			[]any{uuid.New().String(), tag, old, old, uuid.New().String(), tag, recent, recent, uuid.New().String(), tag, old}},
		{`INSERT INTO orders.processed_messages (message_id, processed_at) VALUES (?, ?), (?, ?)`, []any{tag + "-old", old, tag + "-new", recent}},
	} {
		if err := db.Exec(query.sql, query.args...).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	New(db, Config{Interval: time.Hour, Batch: 1, Messages: 14 * 24 * time.Hour, Parked: 30 * 24 * time.Hour, Checkouts: 30 * 24 * time.Hour}, Rules()).RunOnce(context.Background())

	var outboxLeft, processedLeft int64
	db.Raw(`SELECT count(*) FROM orders.outbox WHERE routing_key = ?`, tag).Scan(&outboxLeft)
	db.Raw(`SELECT count(*) FROM orders.processed_messages WHERE message_id LIKE ?`, tag+"%").Scan(&processedLeft)
	if outboxLeft != 2 || processedLeft != 1 {
		t.Fatalf("left: outbox=%d (want 2: recent published + old unpublished), processed=%d (want 1)", outboxLeft, processedLeft)
	}
}
