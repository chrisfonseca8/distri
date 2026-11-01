package main

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const correctnessFixtureStock int64 = 1_000_000

func correctnessResetRequested() (bool, error) {
	raw := strings.TrimSpace(os.Getenv("CORRECTNESS_RESET_DATA"))
	if raw == "" {
		return false, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("CORRECTNESS_RESET_DATA must be true or false: %w", err)
	}
	return value, nil
}

// resetTestData gives each gateway-mode suite the fresh disposable database
// required by phase 1. The explicit hostname allow-list prevents this destructive
// operation from running against an unintended database.
func (s *suite) resetTestData() error {
	databaseURL := requiredEnv("DATABASE_URL")
	parsed, err := url.Parse(databaseURL)
	if err != nil || parsed.Hostname() == "" {
		return fmt.Errorf("DATABASE_URL is not a valid PostgreSQL URL")
	}
	allowedHost := strings.TrimSpace(os.Getenv("CORRECTNESS_ALLOWED_DB_HOST"))
	if allowedHost == "" || parsed.Hostname() != allowedHost {
		return fmt.Errorf("refusing reset: database host %q does not exactly match CORRECTNESS_ALLOWED_DB_HOST %q", parsed.Hostname(), allowedHost)
	}

	tx, err := s.db.Begin(s.ctx)
	if err != nil {
		return fmt.Errorf("begin reset: %w", err)
	}
	defer tx.Rollback(s.ctx)
	if _, err := tx.Exec(s.ctx, `TRUNCATE TABLE
		orders.order_items,
		orders.orders,
		orders.order_tombstones,
		orders.outbox,
		orders.processed_messages,
		orchestrator.checkout_attempts,
		orchestrator.saga_steps,
		orchestrator.sagas,
		orchestrator.outbox,
		orchestrator.processed_messages,
		inventory.reservation_items,
		inventory.reservations,
		inventory.outbox,
		inventory.processed_messages,
		payments.jobs,
		payments.webhook_events,
		payments.outbox,
		payments.processed_messages,
		payments.compensation_events,
		payments.compensation_records,
		payments.payment_attempts
		RESTART IDENTITY CASCADE`); err != nil {
		return fmt.Errorf("truncate test rows: %w", err)
	}
	if _, err := tx.Exec(s.ctx, `UPDATE inventory.inventories
		SET on_hand=$1, reserved=0, allocated=0, updated_at=now()`, correctnessFixtureStock); err != nil {
		return fmt.Errorf("reset inventory: %w", err)
	}
	if err := tx.Commit(s.ctx); err != nil {
		return fmt.Errorf("commit reset: %w", err)
	}
	return nil
}
