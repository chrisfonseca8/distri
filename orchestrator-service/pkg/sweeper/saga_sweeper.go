// Package sweeper holds the orchestrator's background repair loops: resending
// unanswered saga commands, parking sagas that cannot make progress, and
// releasing orphaned stock reservations.
package sweeper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/messages"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/outbox"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/saga"
	"gorm.io/gorm"
)

const backoffJitter = 0.2

type SagaConfig struct {
	Interval         time.Duration
	Batch            int
	StepTimeout      time.Duration
	StepMaxAttempts  int
	ResendBackoffCap time.Duration
	// StuckRefundAfter and Deadline: see config.SagaConfig. Zero StuckRefundAfter disables
	// the refund of a saga that stays STUCK after the customer has paid.
	StuckRefundAfter time.Duration
	Deadline         time.Duration
	// UndoReviveAfter / UndoMaxRevives: see config.SagaConfig. Zero UndoReviveAfter disables.
	UndoReviveAfter time.Duration
	UndoMaxRevives  int
}

// validate rejects zero or negative sweeper settings.
func (c SagaConfig) validate() error {
	if c.Interval <= 0 {
		return errors.New("saga sweep interval must be greater than zero")
	}
	if c.Batch <= 0 {
		return errors.New("saga sweep batch must be greater than zero")
	}
	if c.StepTimeout <= 0 {
		return errors.New("saga step timeout must be greater than zero")
	}
	if c.StepMaxAttempts <= 0 {
		return errors.New("saga step max attempts must be greater than zero")
	}
	if c.ResendBackoffCap < c.StepTimeout {
		return errors.New("saga resend backoff cap must not be below the step timeout")
	}
	if c.StuckRefundAfter < 0 || (c.StuckRefundAfter > 0 && c.Deadline <= 0) {
		return errors.New("saga stuck refund needs a non-negative delay and a deadline for the new steps")
	}
	if c.UndoReviveAfter < 0 || c.UndoMaxRevives < 0 || (c.UndoReviveAfter > 0 && c.Deadline <= 0) {
		return errors.New("saga undo revive needs non-negative settings and a deadline for the revived step")
	}
	return nil
}

// SagaSweeper resends commands whose reply is overdue and marks sagas STUCK
// when they run out of attempts or time. It never cancels orders, releases
// stock or touches payments: STUCK is a hand-off to an operator, and a late
// reply for the current step can still move the saga on.
type SagaSweeper struct {
	db       *gorm.DB
	config   SagaConfig
	failer   StepFailer
	notifier statusNotifier
}

type statusNotifier interface {
	Notify(orderID string)
}

// StepFailer fails a saga step on behalf of the sweeper so the saga compensates.
type StepFailer interface {
	FailStep(ctx context.Context, orderID uuid.UUID, step messages.Step) error
}

// SagaOption customises a SagaSweeper.
type SagaOption func(*SagaSweeper)

// WithStepFailer makes the sweeper compensate, instead of marking STUCK, a saga whose
// CREATE_ORDER step runs out of attempts or time. That step runs before payment, so
// no money is involved and cancelling the order and releasing the stock is safe.
// From TAKE_PAYMENT on the sweeper still marks STUCK: the payment service alone
// decides what happened to the money.
func WithStepFailer(failer StepFailer) SagaOption {
	return func(s *SagaSweeper) { s.failer = failer }
}

// WithStatusNotifier publishes committed sweeper transitions to SSE subscribers.
func WithStatusNotifier(notifier statusNotifier) SagaOption {
	return func(s *SagaSweeper) { s.notifier = notifier }
}

// failsBeforePayment reports whether step can be compensated automatically.
func (s *SagaSweeper) failsBeforePayment(step string) bool {
	return s.failer != nil && step == string(saga.CREATE_ORDER)
}

// failStep asks the failer to fail one step and logs the result.
func (s *SagaSweeper) failStep(ctx context.Context, sagaID uuid.UUID, step string, reason string) {
	if err := s.failer.FailStep(ctx, sagaID, messages.Step(step)); err != nil {
		slog.Error("saga sweeper: could not compensate a failed step", "saga_id", sagaID, "step", step, "error", err)
		return
	}
	slog.Warn("saga compensating: step failed before payment", "saga_id", sagaID, "step", step, "reason", reason)
}

// NewSagaSweeper creates a sweeper that resends overdue steps and flags expired sagas.
func NewSagaSweeper(db *gorm.DB, config SagaConfig, options ...SagaOption) (*SagaSweeper, error) {
	if db == nil {
		return nil, errors.New("saga sweeper database is required")
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	sweeper := &SagaSweeper{db: db, config: config}
	for _, option := range options {
		option(sweeper)
	}
	return sweeper, nil
}

// Run sweeps on every interval until ctx is cancelled.
func (s *SagaSweeper) Run(ctx context.Context) {
	slog.Info("saga sweeper started", "interval", s.config.Interval, "batch", s.config.Batch)
	defer slog.Info("saga sweeper stopped")

	ticker := time.NewTicker(s.config.Interval)
	defer ticker.Stop()
	for {
		s.SweepOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// SweepOnce does one pass: resend overdue steps, then mark expired sagas STUCK.
func (s *SagaSweeper) SweepOnce(ctx context.Context) {
	if err := s.resendDueSteps(ctx); err != nil && ctx.Err() == nil {
		slog.Error("saga sweeper: resend pass failed", "error", err)
	}
	if err := s.markExpiredSagasStuck(ctx); err != nil && ctx.Err() == nil {
		slog.Error("saga sweeper: deadline pass failed", "error", err)
	}
	if err := s.refundStuckAfterPayment(ctx); err != nil && ctx.Err() == nil {
		slog.Error("saga sweeper: refund pass failed", "error", err)
	}
	if err := s.reviveStuckUndo(ctx); err != nil && ctx.Err() == nil {
		slog.Error("saga sweeper: undo revive pass failed", "error", err)
	}
}

// reviveStuckUndo retries sagas STUCK on an undo step (cancel, release, refund), a few
// times with a pause in between, before leaving them for a person.
//
// An undo step has no money decision left to make: it only has to land. Most causes of a
// stuck one are temporary (a service was down), so after UndoReviveAfter the step is put
// back to work with fresh attempts and a fresh deadline. The counter lives on the saga row,
// so a restart or a second replica cannot give it extra tries; after UndoMaxRevives the
// saga stays STUCK for `admin revive`, which resets the counter.
func (s *SagaSweeper) reviveStuckUndo(ctx context.Context) error {
	if s.config.UndoReviveAfter <= 0 || s.config.UndoMaxRevives <= 0 {
		return nil
	}
	for handled := 0; handled < s.config.Batch; handled++ {
		found, err := s.reviveOneStuckUndo(ctx)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
	}
	return nil
}

const stuckUndoQuery = `
SELECT id, current_step, version, undo_revives
FROM orchestrator.sagas
WHERE status = 'STUCK'
	AND current_step IN ('MARK_ORDER_CANCELLED', 'RELEASE_STOCK', 'REFUND_PAYMENT', 'CANCEL_PAID_ORDER')
	AND undo_revives < ?
	AND updated_at < now() - (? * interval '1 microsecond')
ORDER BY updated_at
LIMIT 1
FOR UPDATE SKIP LOCKED`

// reviveOneStuckUndo revives one saga; it returns false if none is due.
func (s *SagaSweeper) reviveOneStuckUndo(ctx context.Context) (bool, error) {
	var found bool
	var sagaID uuid.UUID
	var step string
	var tries int
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []struct {
			ID          uuid.UUID
			CurrentStep string
			Version     int64
			UndoRevives int
		}
		if err := tx.Raw(stuckUndoQuery, s.config.UndoMaxRevives, s.config.UndoReviveAfter.Microseconds()).Scan(&rows).Error; err != nil {
			return fmt.Errorf("find sagas stuck on an undo step: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}
		found = true
		sagaID, step, tries = rows[0].ID, rows[0].CurrentStep, rows[0].UndoRevives+1
		now := time.Now().UTC()

		// The step must be sendable again; if not, the saga stays STUCK for a person, and the
		// counter is spent so the same row is not picked up every pass.
		made := tx.Exec(`
UPDATE orchestrator.saga_steps
SET attempts = 1, next_attempt_at = ?, updated_at = ?
WHERE saga_id = ? AND step = ? AND status = 'RUNNING' AND routing_key IS NOT NULL AND command_payload IS NOT NULL`,
			now, now, sagaID.String(), step)
		if made.Error != nil {
			return fmt.Errorf("make the stuck undo step due: %w", made.Error)
		}
		if made.RowsAffected != 1 {
			return tx.Exec(`UPDATE orchestrator.sagas SET undo_revives = ?, updated_at = ? WHERE id = ? AND status = 'STUCK' AND version = ?`,
				s.config.UndoMaxRevives, now, sagaID.String(), rows[0].Version).Error
		}
		move := tx.Exec(`
UPDATE orchestrator.sagas
SET status = 'COMPENSATING', version = version + 1, deadline_at = ?, updated_at = ?, undo_revives = ?
WHERE id = ? AND status = 'STUCK' AND version = ?`, now.Add(s.config.Deadline), now, tries, sagaID.String(), rows[0].Version)
		if move.Error != nil {
			return fmt.Errorf("revive stuck undo saga: %w", move.Error)
		}
		if move.RowsAffected != 1 {
			return errors.New("saga changed while it was locked")
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	if found {
		if s.notifier != nil {
			s.notifier.Notify(sagaID.String())
		}
		slog.Warn("saga stuck on an undo step: retrying automatically",
			"saga_id", sagaID, "step", step, "revive", tries, "max_revives", s.config.UndoMaxRevives)
	}
	return found, nil
}

// refundStuckAfterPayment unwinds sagas that have stayed STUCK, after the customer paid,
// for longer than StuckRefundAfter, so nobody waits forever.
//
// The customer's money is taken and the saga cannot finish (the order cannot be marked
// paid, or the stock cannot be confirmed). Rather than hold the order in limbo, the saga
// is moved, in one guarded transaction, onto the undo path: refund the payment, cancel the
// paid order, give the stock back. Moving the saga off the stuck step first is what makes
// this safe: a late reply for the old step is then ignored, so the saga can never
// complete after the money has been refunded.
func (s *SagaSweeper) refundStuckAfterPayment(ctx context.Context) error {
	if s.config.StuckRefundAfter <= 0 {
		return nil
	}
	for handled := 0; handled < s.config.Batch; handled++ {
		found, err := s.refundOneStuck(ctx)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
	}
	return nil
}

const stuckAfterPaymentQuery = `
SELECT id, current_step, version
FROM orchestrator.sagas
WHERE status = 'STUCK'
	AND current_step IN ('MARK_ORDER_PAID', 'CONFIRM_STOCK')
	AND updated_at < now() - (? * interval '1 microsecond')
ORDER BY updated_at
LIMIT 1
FOR UPDATE SKIP LOCKED`

// refundOneStuck moves one saga onto the refund path; it returns false if none is due.
func (s *SagaSweeper) refundOneStuck(ctx context.Context) (bool, error) {
	var found bool
	var sagaID uuid.UUID
	var stuckStep string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []struct {
			ID          uuid.UUID
			CurrentStep string
			Version     int64
		}
		if err := tx.Raw(stuckAfterPaymentQuery, s.config.StuckRefundAfter.Microseconds()).Scan(&rows).Error; err != nil {
			return fmt.Errorf("find sagas stuck after payment: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}
		found = true
		sagaID, stuckStep = rows[0].ID, rows[0].CurrentStep
		now := time.Now().UTC()

		move := tx.Exec(`
UPDATE orchestrator.sagas
SET status = 'COMPENSATING', current_step = 'REFUND_PAYMENT', version = version + 1, deadline_at = ?, updated_at = ?
WHERE id = ? AND status = 'STUCK' AND version = ?`, now.Add(s.config.Deadline), now, sagaID.String(), rows[0].Version)
		if move.Error != nil {
			return fmt.Errorf("move saga onto the refund path: %w", move.Error)
		}
		if move.RowsAffected != 1 {
			return errors.New("saga changed while it was locked")
		}
		if err := tx.Exec(`
UPDATE orchestrator.saga_steps
SET status = 'FAILED', last_error = ?, finished_at = ?, updated_at = ?
WHERE saga_id = ? AND step = ? AND status IN ('RUNNING', 'AMBIGUOUS')`,
			"gave up after the saga stayed stuck; the payment is being refunded", now, now, sagaID.String(), stuckStep).Error; err != nil {
			return fmt.Errorf("close the stuck step: %w", err)
		}
		command, err := json.Marshal(messages.PaymentRefund{OrderID: sagaID})
		if err != nil {
			return fmt.Errorf("encode refund command: %w", err)
		}
		if err := tx.Exec(`
INSERT INTO orchestrator.saga_steps (saga_id, step, status, attempts, next_attempt_at, routing_key, command_payload, started_at, created_at, updated_at)
VALUES (?, 'REFUND_PAYMENT', 'RUNNING', 1, ?, ?, ?::jsonb, ?, ?, ?)`,
			sagaID.String(), now.Add(s.config.StepTimeout), messages.PaymentRefundRoutingKey, string(command), now, now, now).Error; err != nil {
			return fmt.Errorf("create the refund step: %w", err)
		}
		return outbox.Enqueue(tx, outbox.Message{
			MessageID:  uuid.New(),
			RoutingKey: messages.PaymentRefundRoutingKey,
			Payload:    command,
		})
	})
	if err != nil {
		return false, err
	}
	if found {
		if s.notifier != nil {
			s.notifier.Notify(sagaID.String())
		}
		slog.Warn("saga stuck after payment: refunding the payment and unwinding the checkout",
			"saga_id", sagaID, "stuck_step", stuckStep, "stuck_for_at_least", s.config.StuckRefundAfter)
	}
	return found, nil
}

type dueStep struct {
	StepID         uuid.UUID
	SagaID         uuid.UUID
	Step           string
	Attempts       int
	RoutingKey     *string
	CommandPayload []byte
	SagaVersion    int64
}

// Each due step is handled in its own short transaction: the row locks are
// taken with SKIP LOCKED, so replicas never work on the same step, and nothing
// in the transaction calls out of the database.
const dueStepQuery = `
SELECT st.id AS step_id,
	st.saga_id,
	st.step,
	st.attempts,
	st.routing_key,
	st.command_payload,
	sg.version AS saga_version
FROM orchestrator.saga_steps AS st
JOIN orchestrator.sagas AS sg ON sg.id = st.saga_id
WHERE st.status = 'RUNNING'
	AND st.next_attempt_at <= now()
	AND sg.status IN ('RUNNING', 'COMPENSATING')
	AND sg.current_step = st.step
ORDER BY st.next_attempt_at
LIMIT 1
FOR UPDATE OF st, sg SKIP LOCKED`

// resendDueSteps resends overdue steps, up to one batch per pass.
func (s *SagaSweeper) resendDueSteps(ctx context.Context) error {
	for handled := 0; handled < s.config.Batch; handled++ {
		found, err := s.handleOneDueStep(ctx)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
	}
	return nil
}

// handleOneDueStep locks one overdue step and either re-queues its command or marks the saga STUCK; it returns false if no step is due.
func (s *SagaSweeper) handleOneDueStep(ctx context.Context) (bool, error) {
	var found bool
	var afterCommit func()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []dueStep
		if err := tx.Raw(dueStepQuery).Scan(&rows).Error; err != nil {
			return fmt.Errorf("claim due saga step: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}
		found = true
		step := rows[0]

		noStoredCommand := len(step.CommandPayload) == 0 || step.RoutingKey == nil || *step.RoutingKey == ""
		if s.failsBeforePayment(step.Step) && (noStoredCommand || step.Attempts >= s.config.StepMaxAttempts) {
			// Push the step out so a failing compensation is not retried in a tight loop;
			// a successful one changes the step itself.
			if err := tx.Table("orchestrator.saga_steps").
				Where("id = ? AND status = ?", step.StepID, "RUNNING").
				Update("next_attempt_at", time.Now().UTC().Add(s.config.StepTimeout)).Error; err != nil {
				return fmt.Errorf("defer compensation retry: %w", err)
			}
			reason := "attempts used up"
			if noStoredCommand {
				reason = "no stored command to resend"
			}
			sagaID, stepName := step.SagaID, step.Step
			afterCommit = func() { s.failStep(ctx, sagaID, stepName, reason) }
			return nil
		}

		if len(step.CommandPayload) == 0 || step.RoutingKey == nil || *step.RoutingKey == "" {
			if err := markStuck(tx, step.SagaID, step.SagaVersion); err != nil {
				return err
			}
			afterCommit = func() {
				if s.notifier != nil {
					s.notifier.Notify(step.SagaID.String())
				}
				slog.Error(
					"saga STUCK: step has no stored command to resend",
					"saga_id", step.SagaID,
					"step", step.Step,
					"attempts", step.Attempts,
				)
			}
			return nil
		}

		if step.Attempts >= s.config.StepMaxAttempts {
			if err := markStuck(tx, step.SagaID, step.SagaVersion); err != nil {
				return err
			}
			afterCommit = func() {
				if s.notifier != nil {
					s.notifier.Notify(step.SagaID.String())
				}
				slog.Error(
					"saga STUCK: step exhausted its attempts",
					"saga_id", step.SagaID,
					"step", step.Step,
					"attempts", step.Attempts,
					"max_attempts", s.config.StepMaxAttempts,
				)
			}
			return nil
		}

		if err := outbox.Enqueue(tx, outbox.Message{
			MessageID:  uuid.New(),
			RoutingKey: *step.RoutingKey,
			Payload:    json.RawMessage(step.CommandPayload),
		}); err != nil {
			return err
		}
		now := time.Now().UTC()
		nextAttemptAt := now.Add(resendDelay(
			s.config.StepTimeout,
			s.config.ResendBackoffCap,
			step.Attempts,
		))
		update := tx.Table("orchestrator.saga_steps").
			Where("id = ? AND status = ?", step.StepID, "RUNNING").
			Updates(map[string]any{
				"attempts":        gorm.Expr("attempts + 1"),
				"next_attempt_at": nextAttemptAt,
				"updated_at":      now,
			})
		if update.Error != nil {
			return fmt.Errorf("reschedule saga step: %w", update.Error)
		}
		if update.RowsAffected != 1 {
			return errors.New("saga step changed while it was locked")
		}
		afterCommit = func() {
			slog.Info(
				"resent saga command",
				"saga_id", step.SagaID,
				"step", step.Step,
				"attempt", step.Attempts+1,
				"next_attempt_at", nextAttemptAt,
			)
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	if afterCommit != nil {
		afterCommit()
	}
	return found, nil
}

// markStuck sets a saga to STUCK, only if its version has not changed.
func markStuck(tx *gorm.DB, sagaID uuid.UUID, version int64) error {
	update := tx.Table("orchestrator.sagas").
		Where("id = ? AND version = ?", sagaID, version).
		Updates(map[string]any{
			"status":     string(saga.SagaStuck),
			"version":    version + 1,
			"updated_at": time.Now().UTC(),
		})
	if update.Error != nil {
		return fmt.Errorf("mark saga stuck: %w", update.Error)
	}
	if update.RowsAffected != 1 {
		return errors.New("saga changed while it was locked")
	}
	return nil
}

// resendDelay is timeout * 2^attempts capped at limit, with +-20% jitter so
// replicas and sagas created together do not resend in lockstep.
func resendDelay(timeout, limit time.Duration, attempts int) time.Duration {
	delay := limit
	if attempts < 30 {
		if scaled := timeout << attempts; scaled > 0 && scaled < limit {
			delay = scaled
		}
	}
	factor := 1 + backoffJitter*(2*rand.Float64()-1)
	return time.Duration(float64(delay) * factor)
}

type expiredSaga struct {
	ID          uuid.UUID
	CurrentStep string
	Attempts    int
}

const expireSagasQuery = `
UPDATE orchestrator.sagas AS sg
SET status = 'STUCK', version = sg.version + 1, updated_at = now()
WHERE sg.id IN (
	SELECT id
	FROM orchestrator.sagas
	WHERE status IN ('RUNNING', 'COMPENSATING')
		AND deadline_at < now()
		AND NOT (? AND status = 'RUNNING' AND current_step = 'CREATE_ORDER')
	ORDER BY deadline_at
	LIMIT ?
	FOR UPDATE SKIP LOCKED
)
RETURNING sg.id,
	sg.current_step,
	COALESCE((
		SELECT st.attempts
		FROM orchestrator.saga_steps AS st
		WHERE st.saga_id = sg.id AND st.step = sg.current_step
	), 0) AS attempts`

// markExpiredSagasStuck parks every saga past its deadline. current_step is left
// alone so a late reply for that step can still resume the saga.
func (s *SagaSweeper) markExpiredSagasStuck(ctx context.Context) error {
	var expired []expiredSaga
	if err := s.db.WithContext(ctx).
		Raw(expireSagasQuery, s.failer != nil, s.config.Batch).
		Scan(&expired).Error; err != nil {
		return fmt.Errorf("mark expired sagas stuck: %w", err)
	}
	for _, row := range expired {
		if s.notifier != nil {
			s.notifier.Notify(row.ID.String())
		}
		slog.Error(
			"saga STUCK: deadline passed",
			"saga_id", row.ID,
			"step", row.CurrentStep,
			"attempts", row.Attempts,
		)
	}
	return s.compensateExpiredBeforePayment(ctx)
}

// compensateExpiredBeforePayment fails the CREATE_ORDER step of every saga that ran out
// of time before payment started, so it cancels the order and releases the stock.
func (s *SagaSweeper) compensateExpiredBeforePayment(ctx context.Context) error {
	if s.failer == nil {
		return nil
	}
	var ids []uuid.UUID
	if err := s.db.WithContext(ctx).Raw(`
SELECT id FROM orchestrator.sagas
WHERE status = 'RUNNING' AND current_step = 'CREATE_ORDER' AND deadline_at < now()
ORDER BY deadline_at
LIMIT ?`, s.config.Batch).Scan(&ids).Error; err != nil {
		return fmt.Errorf("list expired sagas before payment: %w", err)
	}
	for _, id := range ids {
		s.failStep(ctx, id, string(saga.CREATE_ORDER), "deadline passed")
	}
	return nil
}
