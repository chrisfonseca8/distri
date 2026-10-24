// Package admin holds the operator actions for work the automatic loops hand over to a
// person: refunds in REVIEW, webhook events that could not be applied, dead jobs.
// Nothing here runs by itself; every change is one guarded statement, so a mistake or a
// repeat changes nothing it should not, and every refund decision stays in the
// compensation history.
package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"uuid"

	"gorm.io/gorm"
)

// ReviewRecord is a refund that needs a person.
type ReviewRecord struct {
	ID               uuid.UUID
	OrderID          uuid.UUID
	Gateway          string
	GatewayPaymentID string
	AmountPaise      int64
	Currency         string
	Reason           string
	ReviewReason     *string
	RefundID         *string
	ProviderStatus   *string
	UpdatedAt        time.Time
}

// ListReview returns the refund records waiting for a person, oldest first.
func ListReview(ctx context.Context, db *gorm.DB, limit int) ([]ReviewRecord, error) {
	var rows []ReviewRecord
	err := db.WithContext(ctx).Raw(`
SELECT id, order_id, gateway, gateway_payment_id, amount_paise, currency, reason, review_reason, refund_id, provider_status, updated_at
FROM payments.compensation_records WHERE status = 'REVIEW' ORDER BY updated_at LIMIT ?`, limit).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list refunds in review: %w", err)
	}
	return rows, nil
}

// HistoryEntry is one step of a refund record's history.
type HistoryEntry struct {
	At         time.Time
	FromStatus string
	ToStatus   string
	Detail     string
}

// History returns the history of one refund record, oldest first.
func History(ctx context.Context, db *gorm.DB, id uuid.UUID) ([]HistoryEntry, error) {
	var rows []HistoryEntry
	if err := db.WithContext(ctx).Raw(`
SELECT at, from_status, to_status, detail FROM payments.compensation_events WHERE record_id = ? ORDER BY at, id`, id.String()).
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load refund history: %w", err)
	}
	return rows, nil
}

// ErrNotRetryable means the record is not in REVIEW, or it already holds a provider
// refund that must be looked at on the provider side first.
var ErrNotRetryable = errors.New("record is not in review, or it holds a provider refund that needs a person to check the provider")

// RetryReview sends a refund in REVIEW back to the worker as a new operation.
//
// It refuses a record that already holds a provider refund id: that refund exists (or
// existed) at the provider and must be checked there, not repeated. Resubmitting a
// record without one cannot refund twice, because the worker reads the payment's
// refunded amount before it sends and the provider refuses a refund beyond what was
// captured. A note is required and is written to the history.
func RetryReview(ctx context.Context, db *gorm.DB, id uuid.UUID, note string) error {
	note = strings.TrimSpace(note)
	if note == "" {
		return errors.New("a note explaining the decision is required")
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		update := tx.Exec(`
UPDATE payments.compensation_records
SET status = 'REQUIRED', review_reason = NULL, failure_count = 0, retry_count = 0,
	next_attempt_at = now(), locked_by = NULL, locked_until = NULL, submitted_at = NULL,
	unknown_since = NULL, last_error = NULL, updated_at = now()
WHERE id = ? AND status = 'REVIEW' AND refund_id IS NULL`, id.String())
		if update.Error != nil {
			return fmt.Errorf("retry refund: %w", update.Error)
		}
		if update.RowsAffected != 1 {
			return ErrNotRetryable
		}
		return tx.Exec(`INSERT INTO payments.compensation_events (record_id, from_status, to_status, detail, at) VALUES (?, 'REVIEW', 'REQUIRED', ?, now())`,
			id.String(), "retried by an operator: "+note).Error
	})
}

// ParkedEvent is a webhook event that could not be applied.
type ParkedEvent struct {
	EventID        string
	EventType      string
	ParkReason     *string
	GatewayOrderID *string
	LastError      *string
	ReceivedAt     time.Time
}

// ListParked returns parked webhook events, oldest first.
func ListParked(ctx context.Context, db *gorm.DB, limit int) ([]ParkedEvent, error) {
	var rows []ParkedEvent
	if err := db.WithContext(ctx).Raw(`
SELECT event_id, event_type, park_reason, gateway_order_id, last_error, received_at
FROM payments.webhook_events WHERE status = 'PARKED' ORDER BY received_at LIMIT ?`, limit).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list parked events: %w", err)
	}
	return rows, nil
}

// ErrNotParked means there is no parked event with that id.
var ErrNotParked = errors.New("no parked webhook event with that id")

// ReplayParked puts one parked webhook event back in the queue. Applying an event is
// guarded, so replaying one that no longer changes anything is harmless.
func ReplayParked(ctx context.Context, db *gorm.DB, eventID string) error {
	update := db.WithContext(ctx).Exec(`
UPDATE payments.webhook_events
SET status = 'PENDING', attempts = 0, next_attempt_at = now(), park_reason = NULL,
	locked_by = NULL, locked_until = NULL, processed_at = NULL, last_error = 'replayed by an operator'
WHERE event_id = ? AND status = 'PARKED'`, eventID)
	if update.Error != nil {
		return fmt.Errorf("replay parked event: %w", update.Error)
	}
	if update.RowsAffected != 1 {
		return ErrNotParked
	}
	return nil
}

// DeadJob is a payment job that ran out of attempts.
type DeadJob struct {
	ID        uuid.UUID
	PaymentID uuid.UUID
	Attempts  int
	LastError *string
	UpdatedAt time.Time
}

// ListDeadJobs returns dead payment jobs, oldest first.
func ListDeadJobs(ctx context.Context, db *gorm.DB, limit int) ([]DeadJob, error) {
	var rows []DeadJob
	if err := db.WithContext(ctx).Raw(`
SELECT id, payment_id, attempts, last_error, updated_at FROM payments.jobs WHERE status = 'dead' ORDER BY updated_at LIMIT ?`, limit).
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("list dead jobs: %w", err)
	}
	return rows, nil
}
