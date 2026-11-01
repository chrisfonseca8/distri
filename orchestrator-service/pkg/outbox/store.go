package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/orchestrator-service/database/models"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/observability"
)

type Message struct {
	MessageID  uuid.UUID
	RoutingKey string
	Payload    json.RawMessage
}

// Enqueue stores a message using the caller's transaction. State changes and
// their outgoing message therefore commit or roll back together.
func Enqueue(tx *gorm.DB, message Message) error {
	if tx == nil {
		return errors.New("outbox transaction is required")
	}
	if message.MessageID == uuid.Nil() {
		return errors.New("outbox message ID is required")
	}
	if strings.TrimSpace(message.RoutingKey) == "" {
		return errors.New("outbox routing key is required")
	}
	if !json.Valid(message.Payload) {
		return errors.New("outbox payload must be valid JSON")
	}

	row := models.Outbox{
		MessageID:  message.MessageID,
		RoutingKey: message.RoutingKey,
		Payload:    datatypes.JSON(message.Payload),
	}
	// The caller's transaction carries the request/message context; store its
	// correlation ids and trace so the relay publishes them as headers.
	if ctx := tx.Statement.Context; ctx != nil {
		if encoded, err := json.Marshal(observability.CorrelationFromContext(ctx)); err == nil {
			row.Correlation = datatypes.JSON(encoded)
		}
	}
	if err := tx.Create(&row).Error; err != nil {
		return fmt.Errorf("enqueue outbox message: %w", err)
	}
	return nil
}

type Store struct {
	db *gorm.DB
}

// NewStore creates an outbox store backed by db.
func NewStore(db *gorm.DB) *Store {
	return &Store{db: db.Session(&gorm.Session{SkipDefaultTransaction: true})}
}

// Claim leases up to limit due rows to workerID.
func (s *Store) Claim(
	ctx context.Context,
	workerID string,
	limit int,
	lease time.Duration,
) ([]models.Outbox, error) {
	if strings.TrimSpace(workerID) == "" {
		return nil, errors.New("outbox worker ID is required")
	}
	if limit <= 0 {
		return nil, errors.New("outbox claim limit must be greater than zero")
	}
	if lease < time.Microsecond {
		return nil, errors.New("outbox lease must be at least one microsecond")
	}

	const query = `
WITH due AS (
	SELECT id
	FROM orchestrator.outbox
	WHERE published_at IS NULL
		AND dead_at IS NULL
		AND next_attempt_at <= now()
		AND (locked_until IS NULL OR locked_until < now())
	ORDER BY created_at, id
	FOR UPDATE SKIP LOCKED
	LIMIT ?
)
UPDATE orchestrator.outbox AS outbox
SET locked_by = ?,
	locked_until = now() + (? * interval '1 microsecond'),
	attempts = outbox.attempts + 1
FROM due
WHERE outbox.id = due.id
RETURNING outbox.*`

	var claimed []models.Outbox
	result := s.db.WithContext(ctx).
		Raw(query, limit, workerID, lease.Microseconds()).
		Scan(&claimed)
	if result.Error != nil {
		return nil, fmt.Errorf("claim outbox messages: %w", result.Error)
	}
	return claimed, nil
}

// RecordUnpublishable records a permanent publishing failure and dead-letters the row at the limit.
func (s *Store) RecordUnpublishable(ctx context.Context, rowID uuid.UUID, workerID string, maxAttempts int, delay time.Duration, lastError string) (bool, bool, error) {
	if maxAttempts <= 0 || delay < 0 {
		return false, false, errors.New("invalid outbox unpublishable failure policy")
	}
	var rows []struct{ Dead bool }
	result := s.db.WithContext(ctx).Raw(`UPDATE orchestrator.outbox SET unpublishable_attempts = unpublishable_attempts + 1, dead_at = CASE WHEN unpublishable_attempts + 1 >= ? THEN now() ELSE NULL END, next_attempt_at = now() + (? * interval '1 microsecond'), locked_by = NULL, locked_until = NULL, last_error = ? WHERE id = ? AND locked_by = ? AND published_at IS NULL AND dead_at IS NULL RETURNING dead_at IS NOT NULL AS dead`, maxAttempts, delay.Microseconds(), lastError, rowID, workerID).Scan(&rows)
	if result.Error != nil {
		return false, false, fmt.Errorf("record unpublishable outbox message: %w", result.Error)
	}
	if len(rows) == 0 {
		return false, false, nil
	}
	return true, rows[0].Dead, nil
}

// Requeue revives one dead row and resets only its permanent-failure counter.
func (s *Store) Requeue(ctx context.Context, rowID uuid.UUID) (bool, error) {
	result := s.db.WithContext(ctx).Table("orchestrator.outbox").Where("id = ? AND published_at IS NULL AND dead_at IS NOT NULL", rowID).Updates(map[string]any{"dead_at": nil, "unpublishable_attempts": 0, "next_attempt_at": time.Now(), "locked_by": nil, "locked_until": nil, "last_error": nil})
	if result.Error != nil {
		return false, fmt.Errorf("requeue dead outbox message: %w", result.Error)
	}
	return result.RowsAffected == 1, nil
}

// MarkPublished marks a row as sent, only if workerID still holds its lease.
func (s *Store) MarkPublished(
	ctx context.Context,
	rowID uuid.UUID,
	workerID string,
) (bool, error) {
	result := s.db.WithContext(ctx).
		Table("orchestrator.outbox").
		Where("id = ? AND locked_by = ? AND published_at IS NULL", rowID, workerID).
		Updates(map[string]any{
			"published_at": time.Now(),
			"locked_by":    nil,
			"locked_until": nil,
			"last_error":   nil,
		})
	if result.Error != nil {
		return false, fmt.Errorf("mark outbox message published: %w", result.Error)
	}
	return result.RowsAffected == 1, nil
}

// Reschedule puts a row back to be retried after delay, only if workerID still holds its lease.
func (s *Store) Reschedule(
	ctx context.Context,
	rowID uuid.UUID,
	workerID string,
	delay time.Duration,
	lastError string,
) (bool, error) {
	if delay < 0 {
		return false, errors.New("outbox retry delay must not be negative")
	}

	result := s.db.WithContext(ctx).
		Table("orchestrator.outbox").
		Where("id = ? AND locked_by = ? AND published_at IS NULL", rowID, workerID).
		Updates(map[string]any{
			"next_attempt_at": gorm.Expr(
				"now() + (? * interval '1 microsecond')",
				delay.Microseconds(),
			),
			"locked_by":    nil,
			"locked_until": nil,
			"last_error":   lastError,
		})
	if result.Error != nil {
		return false, fmt.Errorf("reschedule outbox message: %w", result.Error)
	}
	return result.RowsAffected == 1, nil
}
