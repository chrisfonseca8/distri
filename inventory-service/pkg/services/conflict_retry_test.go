package services

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestRetryOnConflictIsBounded(t *testing.T) {
	calls := 0
	err := retryOnConflict(context.Background(), "test", func() error {
		calls++
		return fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: "40P01"})
	})
	if calls != maxConflictAttempts {
		t.Fatalf("calls = %d, want %d", calls, maxConflictAttempts)
	}
	if retryableConflictCode(err) != "40P01" {
		t.Fatalf("final error lost its SQLSTATE: %v", err)
	}
}

func TestRetryOnConflictRetriesOnlyConflictsAndStopsOnSuccess(t *testing.T) {
	calls := 0
	if err := retryOnConflict(context.Background(), "test", func() error {
		calls++
		if calls < 2 {
			return &pgconn.PgError{Code: "40001"}
		}
		return nil
	}); err != nil || calls != 2 {
		t.Fatalf("serialization failure: err=%v calls=%d, want nil/2", err, calls)
	}

	calls = 0
	other := errors.New("insufficient stock")
	if err := retryOnConflict(context.Background(), "test", func() error { calls++; return other }); !errors.Is(err, other) || calls != 1 {
		t.Fatalf("business error: err=%v calls=%d, want unchanged/1", err, calls)
	}
}
