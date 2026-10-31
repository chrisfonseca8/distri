package outbox

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/orchestrator-service/database/models"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/config"
	"gorm.io/gorm"
)

func edgeDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("RUN_OUTBOX_INTEGRATION") != "1" {
		t.Skip("set RUN_OUTBOX_INTEGRATION=1 to run database integration tests")
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

func enqueueRows(t *testing.T, db *gorm.DB, count int) []uuid.UUID {
	t.Helper()
	ids := make([]uuid.UUID, count)
	for i := range ids {
		ids[i] = uuid.New()
		err := db.Transaction(func(tx *gorm.DB) error {
			return Enqueue(tx, Message{MessageID: ids[i], RoutingKey: "edge.test", Payload: json.RawMessage(`{"edge":true}`)})
		})
		if err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}
	t.Cleanup(func() { db.Where("message_id IN ?", ids).Delete(&models.Outbox{}) })
	return ids
}

// countingPublisher records every publish per message id, as a broker would see them.
type countingPublisher struct {
	mutex  sync.Mutex
	counts map[uuid.UUID]int
	delay  time.Duration
}

func (p *countingPublisher) Publish(_ context.Context, id uuid.UUID, _ string, _ []byte) error {
	if p.delay > 0 {
		time.Sleep(p.delay)
	}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if p.counts == nil {
		p.counts = map[uuid.UUID]int{}
	}
	p.counts[id]++
	return nil
}

func (p *countingPublisher) count(id uuid.UUID) int {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return p.counts[id]
}

func edgeConfig() RelayConfig {
	return RelayConfig{
		BatchSize: 5, PollInterval: 20 * time.Millisecond, Lease: 600 * time.Millisecond,
		PublishTimeout: 50 * time.Millisecond, RetryBase: 20 * time.Millisecond, RetryCap: 100 * time.Millisecond,
	}
}

func unpublished(t *testing.T, db *gorm.DB, ids []uuid.UUID) int64 {
	t.Helper()
	var open int64
	if err := db.Model(&models.Outbox{}).Where("message_id IN ? AND published_at IS NULL", ids).Count(&open).Error; err != nil {
		t.Fatalf("count unpublished: %v", err)
	}
	return open
}

func waitUntilPublished(t *testing.T, db *gorm.DB, ids []uuid.UUID, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if unpublished(t, db, ids) == 0 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("%d of %d rows still unpublished after %v", unpublished(t, db, ids), len(ids), within)
}

func TestTwoRelaysPublishEveryRowExactlyOnceWhenNothingCrashes(t *testing.T) {
	db := edgeDB(t)
	ids := enqueueRows(t, db, 60)
	broker := &countingPublisher{delay: 2 * time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		relay, err := NewRelay(NewStore(db), broker, edgeConfig())
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() { defer wg.Done(); relay.Run(ctx) }()
	}
	waitUntilPublished(t, db, ids, 20*time.Second)
	cancel()
	wg.Wait()

	for _, id := range ids {
		if got := broker.count(id); got != 1 {
			t.Fatalf("message %s reached the broker %d times with two healthy relays, want exactly 1", id, got)
		}
	}
}

// dropMarkStore forgets the "published" mark, as if the relay process was killed after
// the broker confirmed the publish and before the database update.
type dropMarkStore struct {
	*Store
	dropped bool
	mutex   sync.Mutex
}

func (s *dropMarkStore) MarkPublished(ctx context.Context, id uuid.UUID, worker string) (bool, error) {
	s.mutex.Lock()
	drop := !s.dropped
	s.mutex.Unlock()
	if drop {
		return true, nil // the process "died" here: nothing was written
	}
	return s.Store.MarkPublished(ctx, id, worker)
}

func TestRelayKilledAfterBrokerConfirmButBeforeTheMarkLosesNothingAndRepeatsWithTheSameMessageID(t *testing.T) {
	db := edgeDB(t)
	ids := enqueueRows(t, db, 12)
	broker := &countingPublisher{}

	// Relay A publishes and "dies" before it can record anything.
	doomedStore := &dropMarkStore{Store: NewStore(db)}
	doomed, err := NewRelay(doomedStore, broker, edgeConfig())
	if err != nil {
		t.Fatal(err)
	}
	ctxA, stopA := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); doomed.Run(ctxA) }()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		published := 0
		for _, id := range ids {
			if broker.count(id) > 0 {
				published++
			}
		}
		if published == len(ids) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	stopA()
	<-done
	if unpublished(t, db, ids) != int64(len(ids)) {
		t.Fatalf("setup: the doomed relay must leave every row unmarked, %d unmarked", unpublished(t, db, ids))
	}

	// Relay B takes over once the leases expire.
	rescuer, err := NewRelay(NewStore(db), broker, edgeConfig())
	if err != nil {
		t.Fatal(err)
	}
	ctxB, stopB := context.WithCancel(context.Background())
	go rescuer.Run(ctxB)
	waitUntilPublished(t, db, ids, 20*time.Second)
	stopB()

	for _, id := range ids {
		if got := broker.count(id); got < 2 {
			t.Fatalf("message %s reached the broker %d times, want at least 2 (at-least-once: the first mark was lost)", id, got)
		}
		var rows int64
		db.Model(&models.Outbox{}).Where("message_id = ? AND published_at IS NOT NULL", id).Count(&rows)
		if rows != 1 {
			t.Fatalf("message %s has %d published rows, want 1 (one logical message, a stable id for consumers to deduplicate on)", id, rows)
		}
	}
}

func TestAStaleRelayCannotMarkOrRescheduleARowAnotherRelayNowHolds(t *testing.T) {
	db := edgeDB(t)
	ids := enqueueRows(t, db, 1)
	store := NewStore(db)

	first, err := store.Claim(context.Background(), "worker-a", 10, 50*time.Millisecond)
	if err != nil || !containsMessage(first, ids[0]) {
		t.Fatalf("worker-a claim: %v rows=%d err=%v", containsMessage(first, ids[0]), len(first), err)
	}
	time.Sleep(120 * time.Millisecond) // worker-a's lease expires (it is paused or dead)
	second, err := store.Claim(context.Background(), "worker-b", 10, time.Minute)
	if err != nil || !containsMessage(second, ids[0]) {
		t.Fatalf("worker-b must reclaim the expired row, got %d rows err=%v", len(second), err)
	}

	var rowID uuid.UUID
	for _, row := range second {
		if row.MessageID == ids[0] {
			rowID = row.ID
		}
	}
	if marked, err := store.MarkPublished(context.Background(), rowID, "worker-a"); err != nil || marked {
		t.Fatalf("stale worker marked the row published: marked=%v err=%v", marked, err)
	}
	if moved, err := store.Reschedule(context.Background(), rowID, "worker-a", 0, "stale"); err != nil || moved {
		t.Fatalf("stale worker rescheduled the row: moved=%v err=%v", moved, err)
	}
	if marked, err := store.MarkPublished(context.Background(), rowID, "worker-b"); err != nil || !marked {
		t.Fatalf("the current holder must be able to mark it: marked=%v err=%v", marked, err)
	}
}

func containsMessage(rows []models.Outbox, id uuid.UUID) bool {
	for _, row := range rows {
		if row.MessageID == id {
			return true
		}
	}
	return false
}

func TestClaimedRowsAreNotClaimedAgainBeforeTheirLeaseExpires(t *testing.T) {
	db := edgeDB(t)
	ids := enqueueRows(t, db, 8)
	store := NewStore(db)

	var wg sync.WaitGroup
	results := make([][]models.Outbox, 4)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows, err := store.Claim(context.Background(), "claimer-"+string(rune('a'+i)), 100, time.Minute)
			if err != nil {
				t.Errorf("claim: %v", err)
			}
			results[i] = rows
		}()
	}
	wg.Wait()

	seen := map[uuid.UUID]int{}
	for _, rows := range results {
		for _, row := range rows {
			seen[row.MessageID]++
		}
	}
	for _, id := range ids {
		if seen[id] != 1 {
			t.Fatalf("message %s was claimed by %d concurrent claimers, want exactly 1", id, seen[id])
		}
	}
}

func TestALeasedRowIsInvisibleToLaterClaimsUntilTheLeaseExpires(t *testing.T) {
	db := edgeDB(t)
	ids := enqueueRows(t, db, 3)
	store := NewStore(db)

	held, err := store.Claim(context.Background(), "holder", 100, 300*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if !containsMessage(held, id) {
			t.Fatalf("setup: holder did not claim %s", id)
		}
	}

	// A claim after the first one has committed: only the lease keeps the rows apart.
	early, err := store.Claim(context.Background(), "intruder", 100, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if containsMessage(early, id) {
			t.Fatalf("message %s was handed to a second worker while the first still held a live lease", id)
		}
	}

	time.Sleep(450 * time.Millisecond)
	late, err := store.Claim(context.Background(), "successor", 100, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if !containsMessage(late, id) {
			t.Fatalf("message %s was not reclaimable after its lease expired", id)
		}
	}
}
