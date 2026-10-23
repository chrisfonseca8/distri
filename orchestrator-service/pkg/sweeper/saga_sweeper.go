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
}

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
	return nil
}

// SagaSweeper resends commands whose reply is overdue and marks sagas STUCK
// when they run out of attempts or time. It never cancels orders, releases
// stock or touches payments: STUCK is a hand-off to an operator, and a late
// reply for the current step can still move the saga on.
type SagaSweeper struct {
	db     *gorm.DB
	config SagaConfig
}

func NewSagaSweeper(db *gorm.DB, config SagaConfig) (*SagaSweeper, error) {
	if db == nil {
		return nil, errors.New("saga sweeper database is required")
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	return &SagaSweeper{db: db, config: config}, nil
}

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

func (s *SagaSweeper) SweepOnce(ctx context.Context) {
	if err := s.resendDueSteps(ctx); err != nil && ctx.Err() == nil {
		slog.Error("saga sweeper: resend pass failed", "error", err)
	}
	if err := s.markExpiredSagasStuck(ctx); err != nil && ctx.Err() == nil {
		slog.Error("saga sweeper: deadline pass failed", "error", err)
	}
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

		if len(step.CommandPayload) == 0 || step.RoutingKey == nil || *step.RoutingKey == "" {
			if err := markStuck(tx, step.SagaID, step.SagaVersion); err != nil {
				return err
			}
			afterCommit = func() {
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
		Raw(expireSagasQuery, s.config.Batch).
		Scan(&expired).Error; err != nil {
		return fmt.Errorf("mark expired sagas stuck: %w", err)
	}
	for _, row := range expired {
		slog.Error(
			"saga STUCK: deadline passed",
			"saga_id", row.ID,
			"step", row.CurrentStep,
			"attempts", row.Attempts,
		)
	}
	return nil
}
