// Package admin holds the operator actions for work the automatic loops hand over to a
// person. Nothing here runs by itself; every change is one guarded statement so a
// mistake or a repeat changes nothing it should not.
package admin

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"gorm.io/gorm"
)

// StuckSaga is a saga the sweeper gave up on.
type StuckSaga struct {
	ID          uuid.UUID
	CurrentStep string
	StepStatus  string
	Attempts    int
	LastError   *string
	TotalPaise  int64
	Currency    string
	UpdatedAt   time.Time
}

// ListStuck returns the STUCK sagas, oldest first.
func ListStuck(ctx context.Context, db *gorm.DB, limit int) ([]StuckSaga, error) {
	var rows []StuckSaga
	err := db.WithContext(ctx).Raw(`
SELECT sg.id, sg.current_step, COALESCE(st.status, '') AS step_status, COALESCE(st.attempts, 0) AS attempts,
	st.last_error, sg.total_paise, sg.currency, sg.updated_at
FROM orchestrator.sagas AS sg
LEFT JOIN orchestrator.saga_steps AS st ON st.saga_id = sg.id AND st.step = sg.current_step
WHERE sg.status = 'STUCK'
ORDER BY sg.updated_at
LIMIT ?`, limit).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list stuck sagas: %w", err)
	}
	return rows, nil
}

// compensationSteps are the saga steps that belong to the undo path.
var compensationSteps = map[string]bool{
	"MARK_ORDER_CANCELLED": true, "RELEASE_STOCK": true, "REFUND_PAYMENT": true, "CANCEL_PAID_ORDER": true,
}

// ErrNotRevivable means the saga is not STUCK, or its current step is not one that
// can simply be sent again.
var ErrNotRevivable = errors.New("saga is not stuck on a step that can be sent again")

// Revive puts a STUCK saga back to work: the running status the step belongs to, a
// fresh deadline, and the current step due for resend now (attempts start again). Use it
// after the cause is fixed, for example the order service was down. It does not decide
// anything about money; the saga's own rules and the payment service still do.
func Revive(ctx context.Context, db *gorm.DB, orderID uuid.UUID, deadline time.Duration) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var saga struct {
			CurrentStep string
			Version     int64
		}
		if err := tx.Raw(`SELECT current_step, version FROM orchestrator.sagas WHERE id = ? AND status = 'STUCK' FOR UPDATE`, orderID.String()).
			Scan(&saga).Error; err != nil {
			return fmt.Errorf("load saga: %w", err)
		}
		if saga.CurrentStep == "" {
			return ErrNotRevivable
		}
		status := "RUNNING"
		if compensationSteps[saga.CurrentStep] {
			status = "COMPENSATING"
		}
		now := time.Now().UTC()
		step := tx.Exec(`
UPDATE orchestrator.saga_steps
SET attempts = 1, next_attempt_at = ?, updated_at = ?
WHERE saga_id = ? AND step = ? AND status = 'RUNNING' AND routing_key IS NOT NULL AND command_payload IS NOT NULL`,
			now, now, orderID.String(), saga.CurrentStep)
		if step.Error != nil {
			return fmt.Errorf("make the step due: %w", step.Error)
		}
		if step.RowsAffected != 1 {
			return ErrNotRevivable
		}
		update := tx.Exec(`
UPDATE orchestrator.sagas
SET status = ?, version = version + 1, deadline_at = ?, updated_at = ?, undo_revives = 0
WHERE id = ? AND status = 'STUCK' AND version = ?`, status, now.Add(deadline), now, orderID.String(), saga.Version)
		if update.Error != nil {
			return fmt.Errorf("revive saga: %w", update.Error)
		}
		if update.RowsAffected != 1 {
			return ErrNotRevivable
		}
		return nil
	})
}
