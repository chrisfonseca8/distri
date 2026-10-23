package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/payment-service/pkg/jobtypes"
	"gorm.io/gorm"
)

const resolveBatch = 50

// PaymentResolver answers for payments whose process_payment job died. Without
// it nobody would send a reply and the saga would wait for its deadline. It
// never calls the gateway: a payment that may have reached the gateway becomes
// UNKNOWN (ambiguous), one that never did becomes FAILED.
type PaymentResolver struct {
	db       *gorm.DB
	interval time.Duration
}

func NewPaymentResolver(db *gorm.DB, interval time.Duration) (*PaymentResolver, error) {
	if db == nil {
		return nil, errors.New("payment resolver database is required")
	}
	if interval <= 0 {
		return nil, errors.New("payment resolver interval must be greater than zero")
	}
	return &PaymentResolver{db: db, interval: interval}, nil
}

func (r *PaymentResolver) Run(ctx context.Context) {
	slog.Info("payment resolver started", "interval", r.interval)
	defer slog.Info("payment resolver stopped")

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		if err := r.ResolveOnce(ctx); err != nil && ctx.Err() == nil {
			slog.Error("payment resolver pass failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

const deadJobPaymentQuery = `
SELECT p.id, p.status
FROM payments.payment_attempts AS p
JOIN payments.jobs AS j ON j.payment_id = p.id AND j.type = ? AND j.status = 'dead'
WHERE p.status IN ('CREATED', 'PENDING')
ORDER BY p.created_at
LIMIT 1
FOR UPDATE OF p SKIP LOCKED`

type deadJobPayment struct {
	ID     uuid.UUID
	Status string
}

// ResolveOnce resolves up to one batch of payments, one short transaction each.
func (r *PaymentResolver) ResolveOnce(ctx context.Context) error {
	for range resolveBatch {
		found, err := r.resolveOne(ctx)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
	}
	return nil
}

func (r *PaymentResolver) resolveOne(ctx context.Context) (bool, error) {
	var (
		found    bool
		resolved bool
		payment  deadJobPayment
		outcome  string
	)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []deadJobPayment
		if err := tx.Raw(deadJobPaymentQuery, jobtypes.ProcessPayment).Scan(&rows).Error; err != nil {
			return fmt.Errorf("claim payment with a dead job: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}
		found = true
		payment = rows[0]

		var updates map[string]any
		switch payment.Status {
		case "CREATED":
			outcome = "Failed"
			updates = map[string]any{"status": "FAILED", "failure_code": "PROCESSING_ERROR"}
		case "PENDING":
			outcome = "Ambiguous"
			updates = map[string]any{"status": "UNKNOWN", "failure_code": nil}
		default:
			return fmt.Errorf("unexpected payment status %q", payment.Status)
		}

		changed, err := writePaymentResult(tx, payment.ID, payment.Status, updates)
		resolved = changed
		return err
	})
	if err != nil {
		return false, err
	}
	if resolved {
		slog.Warn(
			"resolved payment whose job died",
			"payment_id", payment.ID,
			"from_status", payment.Status,
			"outcome", outcome,
		)
	}
	return found, nil
}
