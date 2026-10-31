package sweeper

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/admin"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/clients"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/config"
	"gorm.io/gorm"
)

// ---- STUCK revival: the cases the happy-path test does not reach ----

// clearResendPayload makes a step look like a row written before resend payloads
// existed: it has no command to send again.
func clearResendPayload(t *testing.T, db *gorm.DB, id uuid.UUID, step string) {
	t.Helper()
	exec(t, db, `UPDATE orchestrator.saga_steps SET command_payload = NULL WHERE saga_id = ? AND step = ?`, id.String(), step)
}

func TestStuckUndoSagaWithoutAResendPayloadIsNeverRevivedAndSpendsItsCounter(t *testing.T) {
	db := testDB(t)
	id := uuid.New()
	seedSaga(t, db, id, "RELEASE_STOCK", 5, time.Now().Add(time.Hour))
	clearResendPayload(t, db, id, "RELEASE_STOCK")
	makeStuck(t, db, id, 5*time.Minute)

	undoSweeper(t, db).SweepOnce(context.Background())

	if got := scalar(t, db, `SELECT status || '/' || undo_revives FROM orchestrator.sagas WHERE id = ?`, id.String()); got != "STUCK/3" {
		t.Fatalf("saga with nothing to resend = %s, want STUCK/3 (left for a person, counter spent so it is not picked every pass)", got)
	}
	if got := scalar(t, db, `SELECT count(*)::text FROM orchestrator.outbox WHERE payload ->> 'order_id' = ?`, id.String()); got != "0" {
		t.Fatalf("outbox rows = %s, want 0: there was no command to send", got)
	}
	if got := scalar(t, db, `SELECT attempts::text FROM orchestrator.saga_steps WHERE saga_id = ? AND step = 'RELEASE_STOCK'`, id.String()); got != "5" {
		t.Fatalf("attempts = %s, want the step left untouched", got)
	}
}

func TestAdminReviveRefusesWhenThereIsNothingToResendAndChangesNothing(t *testing.T) {
	db := testDB(t)
	id := uuid.New()
	seedSaga(t, db, id, "MARK_ORDER_PAID", 5, time.Now().Add(time.Hour))
	clearResendPayload(t, db, id, "MARK_ORDER_PAID")
	makeStuck(t, db, id, 5*time.Minute)
	before := scalar(t, db, `SELECT status || '/' || version::text || '/' || undo_revives::text FROM orchestrator.sagas WHERE id = ?`, id.String())

	err := admin.Revive(context.Background(), db, id, 15*time.Minute)

	if !errors.Is(err, admin.ErrNotRevivable) {
		t.Fatalf("Revive error = %v, want ErrNotRevivable", err)
	}
	if after := scalar(t, db, `SELECT status || '/' || version::text || '/' || undo_revives::text FROM orchestrator.sagas WHERE id = ?`, id.String()); after != before {
		t.Fatalf("a refused revive changed the saga: %s -> %s", before, after)
	}
}

func TestAdminReviveOnlyTouchesStuckSagasAndPicksTheRightRunningStatus(t *testing.T) {
	db := testDB(t)
	running, forward, undo := uuid.New(), uuid.New(), uuid.New()
	far := time.Now().Add(time.Hour)
	seedSaga(t, db, running, "TAKE_PAYMENT", 2, far) // RUNNING, not stuck
	seedSaga(t, db, forward, "TAKE_PAYMENT", 5, far)
	seedSaga(t, db, undo, "REFUND_PAYMENT", 5, far)
	makeStuck(t, db, forward, time.Minute)
	makeStuck(t, db, undo, time.Minute)

	if err := admin.Revive(context.Background(), db, running, time.Minute); !errors.Is(err, admin.ErrNotRevivable) {
		t.Fatalf("reviving a RUNNING saga: %v, want ErrNotRevivable", err)
	}
	if err := admin.Revive(context.Background(), db, uuid.New(), time.Minute); !errors.Is(err, admin.ErrNotRevivable) {
		t.Fatalf("reviving an unknown saga: %v, want ErrNotRevivable", err)
	}
	if err := admin.Revive(context.Background(), db, forward, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := admin.Revive(context.Background(), db, undo, time.Minute); err != nil {
		t.Fatal(err)
	}
	if got := scalar(t, db, `SELECT status FROM orchestrator.sagas WHERE id = ?`, forward.String()); got != "RUNNING" {
		t.Fatalf("revived forward saga = %s, want RUNNING", got)
	}
	if got := scalar(t, db, `SELECT status FROM orchestrator.sagas WHERE id = ?`, undo.String()); got != "COMPENSATING" {
		t.Fatalf("revived undo saga = %s, want COMPENSATING (a refund step is on the undo path)", got)
	}
	if got := scalar(t, db, `SELECT attempts::text FROM orchestrator.saga_steps WHERE saga_id = ? AND step = 'REFUND_PAYMENT'`, undo.String()); got != "1" {
		t.Fatalf("attempts after revive = %s, want a fresh 1", got)
	}
}

func TestRevivedUndoSagaIsActuallyResentByTheNextSweep(t *testing.T) {
	db := testDB(t)
	id := uuid.New()
	seedSaga(t, db, id, "CANCEL_PAID_ORDER", 5, time.Now().Add(time.Hour))
	makeStuck(t, db, id, 5*time.Minute)
	sweeper := undoSweeper(t, db)

	sweeper.SweepOnce(context.Background()) // revives, and the same pass resends

	if got := scalar(t, db, `SELECT status FROM orchestrator.sagas WHERE id = ?`, id.String()); got != "COMPENSATING" {
		t.Fatalf("saga = %s, want COMPENSATING", got)
	}
	sweeper.SweepOnce(context.Background())
	if got := scalar(t, db, `SELECT count(*)::text FROM orchestrator.outbox WHERE payload ->> 'order_id' = ?`, id.String()); got != "1" {
		t.Fatalf("queued commands after revive = %s, want exactly 1 (a second pass must not duplicate it)", got)
	}
}

// ---- two orchestrator replicas share one database ----

func TestTwoSweepersNeverResendTheSameStepTwice(t *testing.T) {
	db := testDB(t)
	const sagas = 24
	ids := make([]uuid.UUID, sagas)
	for i := range ids {
		ids[i] = uuid.New()
		seedSaga(t, db, ids[i], "CREATE_ORDER", 1, time.Now().Add(time.Hour))
	}
	first, err := NewSagaSweeper(db, sagaConfig())
	if err != nil {
		t.Fatal(err)
	}
	second, _ := NewSagaSweeper(db, sagaConfig())

	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, replica := range []*SagaSweeper{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			replica.SweepOnce(context.Background())
		}()
	}
	close(start)
	wg.Wait()

	for _, id := range ids {
		if got := scalar(t, db, `SELECT count(*)::text FROM orchestrator.outbox WHERE payload ->> 'order_id' = ?`, id.String()); got != "1" {
			t.Fatalf("saga %s: %s commands queued by two concurrent replicas, want exactly 1", id, got)
		}
		if got := scalar(t, db, `SELECT attempts::text FROM orchestrator.saga_steps WHERE saga_id = ? AND step = 'CREATE_ORDER'`, id.String()); got != "2" {
			t.Fatalf("saga %s attempts = %s, want 2 (one resend counted once)", id, got)
		}
	}
}

func TestTwoSweepersMarkAnExhaustedSagaStuckOnce(t *testing.T) {
	db := testDB(t)
	id := uuid.New()
	seedSaga(t, db, id, "TAKE_PAYMENT", 5, time.Now().Add(time.Hour))
	first, _ := NewSagaSweeper(db, sagaConfig())
	second, _ := NewSagaSweeper(db, sagaConfig())

	var wg sync.WaitGroup
	for _, replica := range []*SagaSweeper{first, second} {
		wg.Add(1)
		go func() { defer wg.Done(); replica.SweepOnce(context.Background()) }()
	}
	wg.Wait()

	if got := scalar(t, db, `SELECT status || '/' || version::text FROM orchestrator.sagas WHERE id = ?`, id.String()); got != "STUCK/2" {
		t.Fatalf("saga = %s, want STUCK at version 2: two replicas must make exactly one transition", got)
	}
}

// ---- resend backoff ----

func TestResendDelayGrowsThenStopsAtTheCapWithBoundedJitter(t *testing.T) {
	const timeout, limit = 30 * time.Second, 5 * time.Minute
	low := func(d time.Duration) time.Duration { return time.Duration(float64(d) * (1 - backoffJitter)) }
	high := func(d time.Duration) time.Duration { return time.Duration(float64(d) * (1 + backoffJitter)) }

	cases := []struct {
		attempts int
		base     time.Duration
	}{
		{0, 30 * time.Second}, {1, time.Minute}, {2, 2 * time.Minute}, {3, 4 * time.Minute},
		{4, limit}, {5, limit}, {29, limit}, {30, limit}, {1000, limit}, // shifts that would overflow still cap
	}
	for _, c := range cases {
		for i := 0; i < 200; i++ {
			got := resendDelay(timeout, limit, c.attempts)
			if got < low(c.base) || got > high(c.base) {
				t.Fatalf("attempts=%d: delay %v outside %v..%v", c.attempts, got, low(c.base), high(c.base))
			}
		}
	}
}

func TestStepResendAtTheBackoffCapNeverSchedulesBeyondTheCap(t *testing.T) {
	db := testDB(t)
	config := sagaConfig()
	config.StepMaxAttempts = 50
	config.ResendBackoffCap = 2 * time.Minute
	sweeper, err := NewSagaSweeper(db, config)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	seedSaga(t, db, id, "CREATE_ORDER", 20, time.Now().Add(time.Hour)) // 30s << 20 is far above the cap

	before := time.Now()
	sweeper.SweepOnce(context.Background())

	var secondsAhead float64
	if err := db.Raw(`SELECT extract(epoch FROM next_attempt_at) - ? FROM orchestrator.saga_steps WHERE saga_id = ? AND step = 'CREATE_ORDER'`,
		float64(before.UnixNano())/1e9, id.String()).Scan(&secondsAhead).Error; err != nil {
		t.Fatal(err)
	}
	if max := (config.ResendBackoffCap + time.Duration(float64(config.ResendBackoffCap)*backoffJitter)).Seconds() + 2; secondsAhead > max || secondsAhead < 0 {
		t.Fatalf("next resend is %.0fs ahead, want 0..%.0fs (the cap plus jitter)", secondsAhead, max)
	}
	if got := scalar(t, db, `SELECT attempts::text FROM orchestrator.saga_steps WHERE saga_id = ? AND step = 'CREATE_ORDER'`, id.String()); got != "21" {
		t.Fatalf("attempts = %s, want 21", got)
	}
}

// ---- reservation sweeper: the COMPENSATED safety net ----

type fakeReservations struct {
	mutex    sync.Mutex
	listed   []clients.ReservationSummary
	released []uuid.UUID
	listErr  error
	failFor  map[uuid.UUID]error
}

func (f *fakeReservations) ListReserved(_ context.Context, _ time.Time, after *clients.ReservationCursor, limit int) ([]clients.ReservationSummary, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if after != nil {
		return nil, nil
	}
	return f.listed, nil
}

func (f *fakeReservations) Release(_ context.Context, orderID uuid.UUID) (clients.ChangeReservationResult, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if err := f.failFor[orderID]; err != nil {
		return clients.ChangeReservationResult{}, err
	}
	f.released = append(f.released, orderID)
	return clients.ChangeReservationResult{OrderID: orderID, Status: "RELEASED"}, nil
}

func (f *fakeReservations) releasedIDs() map[uuid.UUID]bool {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	out := map[uuid.UUID]bool{}
	for _, id := range f.released {
		out[id] = true
	}
	return out
}

func reservationSweeper(t *testing.T, db *gorm.DB, inventory clients.ReservationClient) *ReservationSweeper {
	t.Helper()
	sweeper, err := NewReservationSweeper(inventory, db, config.ReservationSweeperConfig{
		ReservationTTL: time.Minute, Interval: time.Second, BatchSize: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	return sweeper
}

func seedSagaWithStatus(t *testing.T, db *gorm.DB, id uuid.UUID, status, step string) {
	t.Helper()
	seedSaga(t, db, id, step, 1, time.Now().Add(time.Hour))
	exec(t, db, `UPDATE orchestrator.sagas SET status = ? WHERE id = ?`, status, id.String())
}

func TestReservationSweeperReleasesOnlyCompensatedAndOrphanedReservations(t *testing.T) {
	db := testDB(t)
	compensated, running, completed, compensating, stuck, orphan := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	seedSagaWithStatus(t, db, compensated, "COMPENSATED", "RELEASE_STOCK")
	seedSagaWithStatus(t, db, running, "RUNNING", "TAKE_PAYMENT")
	seedSagaWithStatus(t, db, completed, "COMPLETED", "CONFIRM_STOCK")
	seedSagaWithStatus(t, db, compensating, "COMPENSATING", "RELEASE_STOCK")
	seedSagaWithStatus(t, db, stuck, "STUCK", "TAKE_PAYMENT")
	old := time.Now().Add(-time.Hour)
	inventory := &fakeReservations{}
	for _, id := range []uuid.UUID{compensated, running, completed, compensating, stuck, orphan} {
		inventory.listed = append(inventory.listed, clients.ReservationSummary{ReservationID: uuid.New(), OrderID: id, Status: "RESERVED", CreatedAt: old})
	}

	if err := reservationSweeper(t, db, inventory).Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	released := inventory.releasedIDs()
	if !released[compensated] {
		t.Fatal("a reservation still held by a COMPENSATED saga must be released (safety net)")
	}
	if !released[orphan] {
		t.Fatal("a reservation with no saga and no live attempt must be released")
	}
	for name, id := range map[string]uuid.UUID{"RUNNING": running, "COMPLETED": completed, "COMPENSATING": compensating, "STUCK": stuck} {
		if released[id] {
			t.Fatalf("a reservation owned by a %s saga was released: time alone must not release it", name)
		}
	}
	if len(released) != 2 {
		t.Fatalf("released %d reservations, want exactly 2", len(released))
	}
}

func TestReservationSweeperKeepsGoingAfterOneReleaseFailsAndReportsIt(t *testing.T) {
	db := testDB(t)
	broken, healthy := uuid.New(), uuid.New()
	seedSagaWithStatus(t, db, broken, "COMPENSATED", "RELEASE_STOCK")
	seedSagaWithStatus(t, db, healthy, "COMPENSATED", "RELEASE_STOCK")
	old := time.Now().Add(-time.Hour)
	inventory := &fakeReservations{
		failFor: map[uuid.UUID]error{broken: errors.New("inventory unavailable")},
		listed: []clients.ReservationSummary{
			{ReservationID: uuid.New(), OrderID: broken, CreatedAt: old},
			{ReservationID: uuid.New(), OrderID: healthy, CreatedAt: old},
		},
	}

	err := reservationSweeper(t, db, inventory).Sweep(context.Background())

	if err == nil {
		t.Fatal("a failed release must be reported, not swallowed")
	}
	if !inventory.releasedIDs()[healthy] {
		t.Fatal("one failing release must not stop the others")
	}
}

func TestReservationSweeperIsIdempotentAcrossRepeatedSweeps(t *testing.T) {
	db := testDB(t)
	id := uuid.New()
	seedSagaWithStatus(t, db, id, "COMPENSATED", "RELEASE_STOCK")
	inventory := &fakeReservations{listed: []clients.ReservationSummary{{ReservationID: uuid.New(), OrderID: id, CreatedAt: time.Now().Add(-time.Hour)}}}
	sweeper := reservationSweeper(t, db, inventory)

	for i := 0; i < 3; i++ {
		if err := sweeper.Sweep(context.Background()); err != nil {
			t.Fatalf("sweep %d: %v", i, err)
		}
	}

	if got := scalar(t, db, `SELECT status FROM orchestrator.sagas WHERE id = ?`, id.String()); got != "COMPENSATED" {
		t.Fatalf("saga = %s, the safety net must never change saga state", got)
	}
	if len(inventory.released) != 3 {
		t.Fatalf("release calls = %d: repeated sweeps are allowed to repeat the (idempotent) release call", len(inventory.released))
	}
}
