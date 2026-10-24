package sweeper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/clients"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/config"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/saga"
	"gorm.io/gorm"
)

const maxSweepPages = 100

// ReservationSweeper releases inventory reservations that no live saga owns.
// It reads only the orchestrator's own sagas table and calls inventory over
// HTTP; it sends no inventory.release command because an orphan has no saga to
// receive the reply.
type ReservationSweeper struct {
	inventory clients.ReservationClient
	db        *gorm.DB
	config    config.ReservationSweeperConfig
	now       func() time.Time
}

// NewReservationSweeper creates a sweeper that releases old reservations whose saga is missing or already compensated.
func NewReservationSweeper(
	inventory clients.ReservationClient,
	db *gorm.DB,
	sweeperConfig config.ReservationSweeperConfig,
) (*ReservationSweeper, error) {
	if inventory == nil {
		return nil, errors.New("reservation sweeper inventory client is required")
	}
	if db == nil {
		return nil, errors.New("reservation sweeper database is required")
	}
	if err := sweeperConfig.Validate(); err != nil {
		return nil, fmt.Errorf("validate reservation sweeper configuration: %w", err)
	}
	return &ReservationSweeper{
		inventory: inventory,
		db:        db,
		config:    sweeperConfig,
		now:       time.Now,
	}, nil
}

// Run sweeps on every interval until ctx is cancelled.
func (s *ReservationSweeper) Run(ctx context.Context) {
	slog.Info(
		"reservation sweeper started",
		"ttl", s.config.ReservationTTL,
		"interval", s.config.Interval,
		"batch_size", s.config.BatchSize,
	)
	defer slog.Info("reservation sweeper stopped")

	s.sweepAndLog(ctx)
	ticker := time.NewTicker(s.config.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweepAndLog(ctx)
		}
	}
}

// sweepAndLog runs one sweep and logs any error.
func (s *ReservationSweeper) sweepAndLog(ctx context.Context) {
	if err := s.Sweep(ctx); err != nil && ctx.Err() == nil {
		slog.Error("sweep expired reservations", "error", err)
	}
}

// Sweep releases reservations older than the TTL whose saga is missing or compensated; sagas still in progress keep theirs.
func (s *ReservationSweeper) Sweep(ctx context.Context) error {
	cutoff := s.now().UTC().Add(-s.config.ReservationTTL)
	var sweepErrors []error
	var cursor *clients.ReservationCursor
	for page := 0; page < maxSweepPages; page++ {
		reservations, err := s.inventory.ListReserved(ctx, cutoff, cursor, s.config.BatchSize)
		if err != nil {
			return errors.Join(append(
				sweepErrors,
				fmt.Errorf("list expired inventory reservations: %w", err),
			)...)
		}

		for _, reservation := range reservations {
			if ctx.Err() != nil {
				return errors.Join(append(sweepErrors, ctx.Err())...)
			}

			sagaStatus, found, lookupErr := s.sagaStatus(ctx, reservation.OrderID)
			switch {
			case lookupErr == nil && !found:
				// "No saga" was read a moment ago and may no longer be true: a retry of the
				// same checkout key can create the saga at any time. Claim the orphan
				// attempt with ONE guarded statement first; only the winner releases.
				release, err := s.claimOrphan(ctx, reservation.OrderID)
				if err != nil {
					sweepErrors = append(sweepErrors, err)
					break
				}
				if !release {
					break
				}
				if err := s.release(ctx, reservation, "orphan"); err != nil {
					sweepErrors = append(sweepErrors, err)
				}
			case lookupErr != nil:
				sweepErrors = append(sweepErrors, fmt.Errorf(
					"look up saga %s for reservation %s: %w",
					reservation.OrderID,
					reservation.ReservationID,
					lookupErr,
				))
			case sagaStatus == saga.SagaCompensated:
				// Safety net: the saga already released, so this is idempotent.
				if err := s.release(ctx, reservation, string(sagaStatus)); err != nil {
					sweepErrors = append(sweepErrors, err)
				}
			default:
				// RUNNING, COMPLETED, COMPENSATING and STUCK sagas own their
				// reservation; time alone must not release it.
			}
		}

		if len(reservations) < s.config.BatchSize {
			return errors.Join(sweepErrors...)
		}
		last := reservations[len(reservations)-1]
		cursor = &clients.ReservationCursor{CreatedAt: last.CreatedAt, ID: last.ReservationID}
	}
	return errors.Join(append(
		sweepErrors,
		fmt.Errorf("reservation sweep stopped after page cap of %d", maxSweepPages),
	)...)
}

// sagaStatus reports whether the saga exists. A missing saga is a normal
// answer here, so the query avoids Take, whose not-found error GORM logs.
func (s *ReservationSweeper) sagaStatus(ctx context.Context, orderID uuid.UUID) (saga.Status, bool, error) {
	var statuses []string
	if err := s.db.WithContext(ctx).
		Table("orchestrator.sagas").
		Where("id = ?", orderID).
		Limit(1).
		Pluck("status", &statuses).Error; err != nil {
		return "", false, err
	}
	if len(statuses) == 0 {
		return "", false, nil
	}
	return saga.Status(statuses[0]), true, nil
}

// orphanResponse is what a client sees when it retries a checkout key whose request
// died before the saga existed and whose reservation was then released.
const orphanResponse = `{"error": "checkout expired before it was completed, retry with a new idempotency key"}`

// claimOrphan decides atomically whether the reservation of orderID may be released.
//
// The attempt row is the arbiter. Creating the saga completes the attempt in the same
// transaction and only if it is still in_progress, so exactly one of two things
// happens: the saga transaction flips the attempt (a live saga now owns the stock), or
// this statement closes the attempt (the saga can never be created for this key).
// The NOT EXISTS clause also keeps a saga that already exists out of reach.
func (s *ReservationSweeper) claimOrphan(ctx context.Context, orderID uuid.UUID) (bool, error) {
	result := s.db.WithContext(ctx).Exec(`
UPDATE orchestrator.checkout_attempts
SET status = 'completed', response_code = 409, response_body = ?::jsonb, updated_at = now()
WHERE order_id = ? AND status = 'in_progress'
	AND NOT EXISTS (SELECT 1 FROM orchestrator.sagas WHERE id = ?)`,
		orphanResponse, orderID.String(), orderID.String())
	if result.Error != nil {
		return false, fmt.Errorf("claim orphan checkout attempt for order %s: %w", orderID, result.Error)
	}
	if result.RowsAffected == 1 {
		return true, nil
	}
	// Not closed by us: either the saga exists (a live checkout owns the stock), or the
	// attempt was already completed or never recorded, so no saga can appear for it.
	_, exists, err := s.sagaStatus(ctx, orderID)
	if err != nil {
		return false, fmt.Errorf("re-check saga for order %s: %w", orderID, err)
	}
	return !exists, nil
}

// release releases one reservation and logs the result.
func (s *ReservationSweeper) release(
	ctx context.Context,
	reservation clients.ReservationSummary,
	reason string,
) error {
	result, err := s.inventory.Release(ctx, reservation.OrderID)
	if err != nil {
		return fmt.Errorf(
			"release reservation %s for order %s (%s): %w",
			reservation.ReservationID,
			reservation.OrderID,
			reason,
			err,
		)
	}
	if result.Status != "RELEASED" {
		return fmt.Errorf(
			"release reservation %s for order %s (%s): inventory returned status %q",
			reservation.ReservationID,
			reservation.OrderID,
			reason,
			result.Status,
		)
	}
	slog.Info(
		"released expired inventory reservation",
		"order_id", reservation.OrderID.String(),
		"reservation_id", reservation.ReservationID.String(),
		"reason", reason,
	)
	return nil
}
