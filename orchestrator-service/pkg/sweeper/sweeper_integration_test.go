package sweeper

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/admin"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/config"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/messages"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/messaging"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/services/reply"
	"gorm.io/gorm"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("RUN_SWEEPER_INTEGRATION") != "1" {
		t.Skip("set RUN_SWEEPER_INTEGRATION=1 to run database integration tests")
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

func exec(t *testing.T, db *gorm.DB, query string, args ...any) {
	t.Helper()
	if err := db.Exec(query, args...).Error; err != nil {
		t.Fatalf("seed: %v\n%s", err, query)
	}
}

func scalar(t *testing.T, db *gorm.DB, query string, args ...any) string {
	t.Helper()
	var value string
	if err := db.Raw(query, args...).Scan(&value).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	return value
}

type recordingFailer struct {
	mutex sync.Mutex
	calls map[uuid.UUID]messages.Step
}

func (f *recordingFailer) FailStep(_ context.Context, orderID uuid.UUID, step messages.Step) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if f.calls == nil {
		f.calls = map[uuid.UUID]messages.Step{}
	}
	f.calls[orderID] = step
	return nil
}

func (f *recordingFailer) calledFor(id uuid.UUID) (messages.Step, bool) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	step, ok := f.calls[id]
	return step, ok
}

func seedSaga(t *testing.T, db *gorm.DB, id uuid.UUID, step string, attempts int, deadline time.Time) {
	t.Helper()
	command := json.RawMessage(`{"order_id":"` + id.String() + `"}`)
	exec(t, db, `INSERT INTO orchestrator.sagas (id, status, current_step, version, deadline_at, created_at, updated_at, total_paise, currency)
		VALUES (?, 'RUNNING', ?, 1, ?, now(), now(), 100, 'INR')`, id.String(), step, deadline)
	exec(t, db, `INSERT INTO orchestrator.saga_steps (saga_id, step, status, attempts, next_attempt_at, routing_key, command_payload, started_at, created_at, updated_at)
		VALUES (?, ?, 'RUNNING', ?, now() - interval '1 minute', 'x.command', ?::jsonb, now(), now(), now())`, id.String(), step, attempts, string(command))
	t.Cleanup(func() {
		db.Exec(`DELETE FROM orchestrator.sagas WHERE id = ?`, id.String())
		db.Exec(`DELETE FROM orchestrator.outbox WHERE payload ->> 'order_id' = ?`, id.String())
	})
}

func sagaConfig() SagaConfig {
	return SagaConfig{Interval: time.Second, Batch: 50, StepTimeout: 30 * time.Second, StepMaxAttempts: 5, ResendBackoffCap: 5 * time.Minute}
}

func TestCreateOrderThatRunsOutOfAttemptsIsCompensatedNotStuck(t *testing.T) {
	db := testDB(t)
	failer := &recordingFailer{}
	sweeper, err := NewSagaSweeper(db, sagaConfig(), WithStepFailer(failer))
	if err != nil {
		t.Fatal(err)
	}
	exhausted, payment, resendable := uuid.New(), uuid.New(), uuid.New()
	seedSaga(t, db, exhausted, "CREATE_ORDER", 5, time.Now().Add(10*time.Minute))
	seedSaga(t, db, payment, "TAKE_PAYMENT", 5, time.Now().Add(10*time.Minute))
	seedSaga(t, db, resendable, "CREATE_ORDER", 1, time.Now().Add(10*time.Minute))

	sweeper.SweepOnce(context.Background())

	if step, ok := failer.calledFor(exhausted); !ok || step != messages.CREATE_ORDER {
		t.Fatalf("an exhausted CREATE_ORDER must be failed so it compensates: called=%v step=%v", ok, step)
	}
	if got := scalar(t, db, `SELECT status FROM orchestrator.sagas WHERE id = ?`, exhausted.String()); got != "RUNNING" {
		t.Fatalf("exhausted CREATE_ORDER saga status = %s: the failer, not the sweeper, moves it on", got)
	}
	if _, ok := failer.calledFor(payment); ok {
		t.Fatal("TAKE_PAYMENT must never be failed by the sweeper: the payment service decides the payment")
	}
	if got := scalar(t, db, `SELECT status FROM orchestrator.sagas WHERE id = ?`, payment.String()); got != "STUCK" {
		t.Fatalf("an exhausted TAKE_PAYMENT saga must be STUCK for a person, got %s", got)
	}
	if _, ok := failer.calledFor(resendable); ok {
		t.Fatal("a CREATE_ORDER step with attempts left must be resent, not failed")
	}
	if got := scalar(t, db, `SELECT count(*)::text FROM orchestrator.outbox WHERE payload ->> 'order_id' = ?`, resendable.String()); got != "1" {
		t.Fatalf("the resendable step should have queued its command once, queued %s", got)
	}
}

func TestCreateOrderPastItsDeadlineIsCompensatedAndLaterStepsAreStuck(t *testing.T) {
	db := testDB(t)
	failer := &recordingFailer{}
	sweeper, _ := NewSagaSweeper(db, sagaConfig(), WithStepFailer(failer))
	early, late := uuid.New(), uuid.New()
	past := time.Now().Add(-time.Minute)
	seedSaga(t, db, early, "CREATE_ORDER", 1, past)
	seedSaga(t, db, late, "MARK_ORDER_PAID", 1, past)

	sweeper.SweepOnce(context.Background())

	if _, ok := failer.calledFor(early); !ok {
		t.Fatal("a CREATE_ORDER saga past its deadline must be compensated")
	}
	if got := scalar(t, db, `SELECT status FROM orchestrator.sagas WHERE id = ?`, early.String()); got == "STUCK" {
		t.Fatal("it must not be marked STUCK first")
	}
	if got := scalar(t, db, `SELECT status FROM orchestrator.sagas WHERE id = ?`, late.String()); got != "STUCK" {
		t.Fatalf("a saga past its deadline after payment started must be STUCK, got %s", got)
	}
}

func TestWithoutAStepFailerTheOldBehaviourIsKept(t *testing.T) {
	db := testDB(t)
	sweeper, _ := NewSagaSweeper(db, sagaConfig())
	id := uuid.New()
	seedSaga(t, db, id, "CREATE_ORDER", 5, time.Now().Add(10*time.Minute))
	sweeper.SweepOnce(context.Background())
	if got := scalar(t, db, `SELECT status FROM orchestrator.sagas WHERE id = ?`, id.String()); got != "STUCK" {
		t.Fatalf("got %s, want STUCK", got)
	}
}

// ---- reservation sweeper: the orphan claim (EDGE-9 and EDGE-10) ----

func seedAttempt(t *testing.T, db *gorm.DB, key string, orderID uuid.UUID, status string) {
	t.Helper()
	if status == "completed" {
		exec(t, db, `INSERT INTO orchestrator.checkout_attempts (idempotency_key, request_hash, status, order_id, response_code, response_body, created_at, updated_at)
			VALUES (?, 'h', 'completed', ?, 202, '{}'::jsonb, now(), now())`, key, orderID.String())
	} else {
		exec(t, db, `INSERT INTO orchestrator.checkout_attempts (idempotency_key, request_hash, status, order_id, created_at, updated_at)
			VALUES (?, 'h', 'in_progress', ?, now(), now())`, key, orderID.String())
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM orchestrator.checkout_attempts WHERE idempotency_key = ?`, key) })
}

func TestClaimOrphanClosesTheAttemptSoItsKeyAnswersCleanly(t *testing.T) {
	db := testDB(t)
	s := &ReservationSweeper{db: db}
	orderID, key := uuid.New(), "orphan-"+uuid.New().String()
	seedAttempt(t, db, key, orderID, "in_progress")

	release, err := s.claimOrphan(context.Background(), orderID)
	if err != nil || !release {
		t.Fatalf("claim = %v, %v; want the orphan claimed", release, err)
	}
	if got := scalar(t, db, `SELECT status || ' ' || response_code::text FROM orchestrator.checkout_attempts WHERE idempotency_key = ?`, key); got != "completed 409" {
		t.Fatalf("attempt = %q, want completed 409", got)
	}
	// The other side of the race: a saga transaction guarded on in_progress now changes nothing,
	// so no live saga can appear for this key after the stock was released.
	result := db.Exec(`UPDATE orchestrator.checkout_attempts SET status = 'completed', response_code = 202 WHERE idempotency_key = ? AND status = 'in_progress'`, key)
	if result.RowsAffected != 0 {
		t.Fatal("completing the attempt after the claim must change nothing")
	}
}

func TestClaimOrphanLeavesALiveSagasReservationAlone(t *testing.T) {
	db := testDB(t)
	s := &ReservationSweeper{db: db}
	orderID, key := uuid.New(), "live-"+uuid.New().String()
	seedAttempt(t, db, key, orderID, "in_progress")
	seedSaga(t, db, orderID, "CREATE_ORDER", 1, time.Now().Add(10*time.Minute))

	release, err := s.claimOrphan(context.Background(), orderID)
	if err != nil || release {
		t.Fatalf("claim = %v, %v; a saga exists so the stock must not be released", release, err)
	}
	if got := scalar(t, db, `SELECT status FROM orchestrator.checkout_attempts WHERE idempotency_key = ?`, key); got != "in_progress" {
		t.Fatalf("attempt status = %s, must be untouched", got)
	}
}

func TestClaimOrphanAllowsReleaseWhenNoSagaCanAppear(t *testing.T) {
	db := testDB(t)
	s := &ReservationSweeper{db: db}

	closed, key := uuid.New(), "closed-"+uuid.New().String()
	seedAttempt(t, db, key, closed, "completed") // answered without a saga (a definitive failure)
	if release, err := s.claimOrphan(context.Background(), closed); err != nil || !release {
		t.Fatalf("completed attempt without a saga: release=%v err=%v, want true", release, err)
	}
	if release, err := s.claimOrphan(context.Background(), uuid.New()); err != nil || !release {
		t.Fatalf("no attempt at all: release=%v err=%v, want true", release, err)
	}
}

// ---- a saga stuck after the customer paid is refunded after the configured time ----

func makeStuck(t *testing.T, db *gorm.DB, id uuid.UUID, age time.Duration) {
	t.Helper()
	exec(t, db, `UPDATE orchestrator.sagas SET status = 'STUCK', updated_at = ? WHERE id = ?`, time.Now().Add(-age), id.String())
}

func refundSweeper(t *testing.T, db *gorm.DB) *SagaSweeper {
	t.Helper()
	config := sagaConfig()
	config.StuckRefundAfter, config.Deadline = time.Minute, 15*time.Minute
	sweeper, err := NewSagaSweeper(db, config)
	if err != nil {
		t.Fatal(err)
	}
	return sweeper
}

func TestSagaStuckAfterPaymentIsRefundedOnlyAfterTheConfiguredTime(t *testing.T) {
	db := testDB(t)
	paid, confirm, recent, beforePayment := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	far := time.Now().Add(time.Hour)
	for id, step := range map[uuid.UUID]string{paid: "MARK_ORDER_PAID", confirm: "CONFIRM_STOCK", recent: "MARK_ORDER_PAID", beforePayment: "TAKE_PAYMENT"} {
		seedSaga(t, db, id, step, 5, far)
	}
	makeStuck(t, db, paid, 5*time.Minute)
	makeStuck(t, db, confirm, 5*time.Minute)
	makeStuck(t, db, recent, 10*time.Second)
	makeStuck(t, db, beforePayment, 5*time.Minute)

	refundSweeper(t, db).SweepOnce(context.Background())

	for _, id := range []uuid.UUID{paid, confirm} {
		if got := scalar(t, db, `SELECT status || '/' || current_step FROM orchestrator.sagas WHERE id = ?`, id.String()); got != "COMPENSATING/REFUND_PAYMENT" {
			t.Fatalf("saga stuck after payment = %s, want COMPENSATING/REFUND_PAYMENT", got)
		}
		if got := scalar(t, db, `SELECT status FROM orchestrator.saga_steps WHERE saga_id = ? AND step IN ('MARK_ORDER_PAID','CONFIRM_STOCK')`, id.String()); got != "FAILED" {
			t.Fatalf("the stuck step is %s, want FAILED", got)
		}
		if got := scalar(t, db, `SELECT status || ' ' || routing_key FROM orchestrator.saga_steps WHERE saga_id = ? AND step = 'REFUND_PAYMENT'`, id.String()); got != "RUNNING payment.refund" {
			t.Fatalf("refund step = %q, want RUNNING payment.refund", got)
		}
		if got := scalar(t, db, `SELECT count(*)::text FROM orchestrator.outbox WHERE routing_key = 'payment.refund' AND payload ->> 'order_id' = ?`, id.String()); got != "1" {
			t.Fatalf("refund commands queued = %s, want exactly 1", got)
		}
	}
	if got := scalar(t, db, `SELECT status FROM orchestrator.sagas WHERE id = ?`, recent.String()); got != "STUCK" {
		t.Fatalf("a saga stuck for only 10 s = %s, want it left alone until the configured time has passed", got)
	}
	if got := scalar(t, db, `SELECT status FROM orchestrator.sagas WHERE id = ?`, beforePayment.String()); got != "STUCK" {
		t.Fatalf("a saga stuck BEFORE payment = %s: this rule is only for sagas where the customer has paid", got)
	}

	// a second pass changes nothing
	refundSweeper(t, db).SweepOnce(context.Background())
	if got := scalar(t, db, `SELECT count(*)::text FROM orchestrator.outbox WHERE routing_key = 'payment.refund' AND payload ->> 'order_id' = ?`, paid.String()); got != "1" {
		t.Fatalf("refund commands after a second pass = %s, want still 1", got)
	}
}

func TestUnwindingAPaidCheckoutRunsRefundThenCancelThenReleaseAndALateReplyCannotCompleteIt(t *testing.T) {
	db := testDB(t)
	id := uuid.New()
	seedSaga(t, db, id, "CONFIRM_STOCK", 5, time.Now().Add(time.Hour))
	makeStuck(t, db, id, 5*time.Minute)
	refundSweeper(t, db).SweepOnce(context.Background())

	handler := reply.NewHandler(db, 30*time.Second)
	send := func(step messages.Step, outcome messages.Outcome) {
		t.Helper()
		body, _ := json.Marshal(messages.Reply{OrderID: id, Step: step, Outcome: outcome})
		if err := handler.Handle(context.Background(), messaging.Delivery{MessageId: "unwind-" + uuid.New().String(), Body: body}); err != nil {
			t.Fatalf("reply %s %s: %v", step, outcome, err)
		}
	}
	state := func() string {
		return scalar(t, db, `SELECT status || '/' || current_step FROM orchestrator.sagas WHERE id = ?`, id.String())
	}

	// the stock confirmation the saga gave up on answers late: it must NOT complete the saga
	send(messages.CONFIRM_STOCK, messages.Succeeded)
	if got := state(); got != "COMPENSATING/REFUND_PAYMENT" {
		t.Fatalf("after a late CONFIRM_STOCK reply the saga is %s, want it still unwinding", got)
	}

	send(messages.REFUND_PAYMENT, messages.Succeeded)
	if got := state(); got != "COMPENSATING/CANCEL_PAID_ORDER" {
		t.Fatalf("after the refund is recorded: %s, want COMPENSATING/CANCEL_PAID_ORDER", got)
	}
	var payload string
	db.Raw(`SELECT payload::text FROM orchestrator.outbox WHERE routing_key = 'order.cancel' AND payload ->> 'order_id' = ?`, id.String()).Scan(&payload)
	var cancel messages.OrderCancel
	if err := json.Unmarshal([]byte(payload), &cancel); err != nil || !cancel.Refunded {
		t.Fatalf("cancel command = %q (%v), want refunded=true", payload, err)
	}

	send(messages.CANCEL_PAID_ORDER, messages.Succeeded)
	if got := state(); got != "COMPENSATING/RELEASE_STOCK" {
		t.Fatalf("after the paid order is cancelled: %s, want COMPENSATING/RELEASE_STOCK", got)
	}
	send(messages.RELEASE_STOCK, messages.Succeeded)
	if got := state(); got != "COMPENSATED/RELEASE_STOCK" {
		t.Fatalf("at the end: %s, want COMPENSATED", got)
	}
}

// ---- a saga stuck on an undo step is retried a few times, then left for a person ----

func undoSweeper(t *testing.T, db *gorm.DB) *SagaSweeper {
	t.Helper()
	config := sagaConfig()
	config.UndoReviveAfter, config.UndoMaxRevives, config.Deadline = time.Minute, 3, 15*time.Minute
	sweeper, err := NewSagaSweeper(db, config)
	if err != nil {
		t.Fatal(err)
	}
	return sweeper
}

func TestSagaStuckOnAnUndoStepIsRevivedAfterTheDelayAtMostThreeTimes(t *testing.T) {
	db := testDB(t)
	release, recent, payment := uuid.New(), uuid.New(), uuid.New()
	far := time.Now().Add(time.Hour)
	seedSaga(t, db, release, "RELEASE_STOCK", 5, far)
	seedSaga(t, db, recent, "MARK_ORDER_CANCELLED", 5, far)
	seedSaga(t, db, payment, "TAKE_PAYMENT", 5, far)
	makeStuck(t, db, release, 5*time.Minute)
	makeStuck(t, db, recent, 10*time.Second)
	makeStuck(t, db, payment, 5*time.Minute)

	sweeper := undoSweeper(t, db)
	sweeper.SweepOnce(context.Background())

	if got := scalar(t, db, `SELECT status || '/' || undo_revives FROM orchestrator.sagas WHERE id = ?`, release.String()); got != "COMPENSATING/1" {
		t.Fatalf("stuck undo saga = %s, want COMPENSATING/1", got)
	}
	if got := scalar(t, db, `SELECT attempts::text FROM orchestrator.saga_steps WHERE saga_id = ? AND step = 'RELEASE_STOCK'`, release.String()); got != "1" {
		t.Fatalf("attempts after revive = %s, want a fresh 1", got)
	}
	if got := scalar(t, db, `SELECT status FROM orchestrator.sagas WHERE id = ?`, recent.String()); got != "STUCK" {
		t.Fatalf("saga stuck for only 10 s = %s, want it left alone until the delay has passed", got)
	}
	if got := scalar(t, db, `SELECT status FROM orchestrator.sagas WHERE id = ?`, payment.String()); got != "STUCK" {
		t.Fatalf("saga stuck BEFORE payment = %s: this rule is only for undo steps", got)
	}

	// it gets stuck again twice more: revived each time (counter 2, then 3)
	for want := 2; want <= 3; want++ {
		makeStuck(t, db, release, 5*time.Minute)
		sweeper.SweepOnce(context.Background())
		if got := scalar(t, db, `SELECT status || '/' || undo_revives FROM orchestrator.sagas WHERE id = ?`, release.String()); got != fmt.Sprintf("COMPENSATING/%d", want) {
			t.Fatalf("revive %d: saga = %s", want, got)
		}
	}

	// a fourth time it stays STUCK for a person
	makeStuck(t, db, release, 5*time.Minute)
	sweeper.SweepOnce(context.Background())
	if got := scalar(t, db, `SELECT status || '/' || undo_revives FROM orchestrator.sagas WHERE id = ?`, release.String()); got != "STUCK/3" {
		t.Fatalf("after the limit the saga = %s, want STUCK/3 (left for a person)", got)
	}

	// an operator revive gives a fresh budget
	if err := admin.Revive(context.Background(), db, release, 15*time.Minute); err != nil {
		t.Fatal(err)
	}
	if got := scalar(t, db, `SELECT status || '/' || undo_revives FROM orchestrator.sagas WHERE id = ?`, release.String()); got != "COMPENSATING/0" {
		t.Fatalf("after an operator revive = %s, want COMPENSATING/0", got)
	}
}
