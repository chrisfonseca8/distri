package jobs

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/payment-service/database/models"
	"github.com/chrisfonseca08/distri/payment-service/pkg/jobtypes"
	"gorm.io/gorm"
)

type Store struct {
	db *gorm.DB
}

func NewStore(db *gorm.DB) *Store {
	return &Store{db: db.Session(&gorm.Session{SkipDefaultTransaction: true})}
}

// Claim intentionally selects only process_payment jobs in 6a. notify_order
// rows remain durable and queued until notification delivery is implemented.
func (s *Store) Claim(
	ctx context.Context,
	workerID string,
	limit int,
	lease time.Duration,
) ([]models.Job, error) {
	if workerID == "" {
		return nil, errors.New("worker ID is required")
	}
	if limit <= 0 {
		return nil, errors.New("claim limit must be greater than zero")
	}
	if lease < time.Microsecond {
		return nil, errors.New("claim lease must be at least one microsecond")
	}

	const query = `
WITH due AS (
	SELECT id
	FROM payments.jobs
	WHERE type = ?
		AND next_run_at <= now()
		AND (
			status = 'queued'
			OR (status = 'running' AND locked_until < now())
		)
	ORDER BY next_run_at, id
	FOR UPDATE SKIP LOCKED
	LIMIT ?
)
UPDATE payments.jobs AS jobs
SET status = 'running',
	locked_by = ?,
	locked_until = now() + (? * interval '1 microsecond'),
	attempts = jobs.attempts + 1
FROM due
WHERE jobs.id = due.id
RETURNING jobs.*`

	var claimed []models.Job
	result := s.db.WithContext(ctx).
		Raw(query, jobtypes.ProcessPayment, limit, workerID, lease.Microseconds()).
		Scan(&claimed)
	if result.Error != nil {
		return nil, result.Error
	}
	return claimed, nil
}

func (s *Store) Succeed(ctx context.Context, jobID uuid.UUID, workerID string) (bool, error) {
	result := s.db.WithContext(ctx).
		Table("payments.jobs").
		Where("id = ? AND locked_by = ? AND status = ?", jobID, workerID, "running").
		Updates(map[string]any{
			"status":       "succeeded",
			"locked_by":    nil,
			"locked_until": nil,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func (s *Store) Reschedule(
	ctx context.Context,
	jobID uuid.UUID,
	workerID string,
	delay time.Duration,
	countAttempt bool,
	lastError string,
) (bool, error) {
	if delay < 0 {
		return false, errors.New("reschedule delay must not be negative")
	}
	updates := map[string]any{
		"status":       "queued",
		"next_run_at":  gorm.Expr("now() + (? * interval '1 microsecond')", delay.Microseconds()),
		"locked_by":    nil,
		"locked_until": nil,
		"last_error":   lastError,
	}
	if !countAttempt {
		updates["attempts"] = gorm.Expr("attempts - 1")
	}
	result := s.db.WithContext(ctx).
		Table("payments.jobs").
		Where("id = ? AND locked_by = ? AND status = ?", jobID, workerID, "running").
		Updates(updates)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func (s *Store) Kill(
	ctx context.Context,
	jobID uuid.UUID,
	workerID string,
	lastError string,
) (bool, error) {
	result := s.db.WithContext(ctx).
		Table("payments.jobs").
		Where("id = ? AND locked_by = ? AND status = ?", jobID, workerID, "running").
		Updates(map[string]any{
			"status":       "dead",
			"locked_by":    nil,
			"locked_until": nil,
			"last_error":   lastError,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func (s *Store) ListDead(ctx context.Context, limit int) ([]models.Job, error) {
	if limit <= 0 {
		return nil, errors.New("dead job limit must be greater than zero")
	}
	var jobs []models.Job
	result := s.db.WithContext(ctx).
		Table("payments.jobs").
		Where("status = ?", "dead").
		Order("created_at ASC").
		Order("id ASC").
		Limit(limit).
		Find(&jobs)
	if result.Error != nil {
		return nil, result.Error
	}
	return jobs, nil
}

func (s *Store) Requeue(ctx context.Context, jobID uuid.UUID) (bool, error) {
	result := s.db.WithContext(ctx).
		Table("payments.jobs").
		Where("id = ? AND status = ?", jobID, "dead").
		Updates(map[string]any{
			"status":       "queued",
			"attempts":     0,
			"next_run_at":  gorm.Expr("now()"),
			"locked_by":    nil,
			"locked_until": nil,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}
