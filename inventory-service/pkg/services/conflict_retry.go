package services

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// maxConflictAttempts bounds the retry of a transaction PostgreSQL aborted for a
// deadlock (40P01) or serialization failure (40001). The whole transaction is re-run,
// so the work inside must be idempotent (reservations are: keyed by order id).
const maxConflictAttempts = 3

// conflictRetries counts transactions re-run after a conflict (visible to tests and, later, telemetry).
var conflictRetries atomic.Int64

func retryableConflictCode(err error) string {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && (pgError.Code == "40P01" || pgError.Code == "40001") {
		return pgError.Code
	}
	return ""
}

func retryOnConflict(ctx context.Context, operation string, run func() error) error {
	var err error
	for attempt := 1; attempt <= maxConflictAttempts; attempt++ {
		if err = run(); err == nil {
			return nil
		}
		code := retryableConflictCode(err)
		if code == "" {
			return err
		}
		if attempt == maxConflictAttempts {
			slog.Error("transaction conflict, attempts exhausted", "operation", operation, "sqlstate", code, "attempts", attempt)
			return err
		}
		slog.Warn("transaction conflict, retrying", "operation", operation, "sqlstate", code, "attempt", attempt, "max_attempts", maxConflictAttempts)
		conflictRetries.Add(1)
		pause := time.Duration(attempt)*20*time.Millisecond + time.Duration(rand.IntN(20))*time.Millisecond
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pause):
		}
	}
	return err
}
