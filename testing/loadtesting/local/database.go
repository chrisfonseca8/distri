package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const fixtureStock int64 = 1_000_000

type databaseSampler struct {
	pool                *pgxpool.Pool
	every               time.Duration
	mu                  sync.Mutex
	samples             int64
	connections         gaugeAccumulator
	active              gaugeAccumulator
	idleInTx            gaugeAccumulator
	lockWaiters         gaugeAccumulator
	ungranted           gaugeAccumulator
	deadlocksBefore     int64
	databaseBytesBefore int64
	sessionsBefore      int64
	err                 error
}

type gaugeAccumulator struct {
	total float64
	max   int64
}

// readServerCompletionLatency returns the persisted time from saga creation to
// its terminal transition. The fixture is reset before every measured run, so
// all terminal rows belong to the current experiment.
func readServerCompletionLatency(ctx context.Context, pool *pgxpool.Pool) (LatencySummary, error) {
	rows, err := pool.Query(ctx, `SELECT EXTRACT(EPOCH FROM (updated_at - created_at)) * 1000
		FROM orchestrator.sagas
		WHERE status IN ('COMPLETED', 'COMPENSATED')`)
	if err != nil {
		return LatencySummary{}, fmt.Errorf("query server completion latency: %w", err)
	}
	defer rows.Close()

	latencies := make([]time.Duration, 0)
	for rows.Next() {
		var milliseconds float64
		if err := rows.Scan(&milliseconds); err != nil {
			return LatencySummary{}, fmt.Errorf("scan server completion latency: %w", err)
		}
		latencies = append(latencies, time.Duration(milliseconds*float64(time.Millisecond)))
	}
	if err := rows.Err(); err != nil {
		return LatencySummary{}, fmt.Errorf("read server completion latency: %w", err)
	}
	return SummarizeLatencies(latencies), nil
}

// add records one reading, keeping the total and maximum.
func (g *gaugeAccumulator) add(value int64) {
	g.total += float64(value)
	if value > g.max {
		g.max = value
	}
}

// resetFixture restores the test data in one transaction and returns the product IDs.
func resetFixture(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin fixture reset: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `TRUNCATE TABLE
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
		return nil, fmt.Errorf("truncate test rows: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE inventory.inventories
		SET on_hand=$1, reserved=0, allocated=0, updated_at=now()`, fixtureStock); err != nil {
		return nil, fmt.Errorf("reset inventory: %w", err)
	}

	rows, err := tx.Query(ctx, `SELECT p.id::text
		FROM inventory.products p
		JOIN inventory.inventories i ON i.product_id=p.id
		ORDER BY p.id`)
	if err != nil {
		return nil, fmt.Errorf("load products: %w", err)
	}
	defer rows.Close()
	products := make([]string, 0, 50)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan product: %w", err)
		}
		products = append(products, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}
	if len(products) != 50 {
		return nil, fmt.Errorf("load fixture requires 50 products, found %d", len(products))
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit fixture reset: %w", err)
	}
	return products, nil
}

// configureWorkloadFixture sets the hot-product stock for contention cases.
func configureWorkloadFixture(ctx context.Context, pool *pgxpool.Pool, cfg config, products []string) error {
	if cfg.HotStock <= 0 {
		return nil
	}
	result, err := pool.Exec(ctx, `UPDATE inventory.inventories
		SET on_hand=$1, reserved=0, allocated=0, updated_at=now()
		WHERE product_id=$2`, cfg.HotStock, products[0])
	if err != nil {
		return fmt.Errorf("configure hot-product stock: %w", err)
	}
	if result.RowsAffected() != 1 {
		return fmt.Errorf("configure hot-product stock: updated %d rows", result.RowsAffected())
	}
	return nil
}

// newDatabaseSampler records the database baseline and creates the sampler.
func newDatabaseSampler(ctx context.Context, pool *pgxpool.Pool, every time.Duration) (*databaseSampler, error) {
	s := &databaseSampler{pool: pool, every: every}
	if err := pool.QueryRow(ctx, `SELECT deadlocks, pg_database_size(current_database()), sessions
		FROM pg_stat_database WHERE datname=current_database()`).Scan(&s.deadlocksBefore, &s.databaseBytesBefore, &s.sessionsBefore); err != nil {
		return nil, fmt.Errorf("read database baseline: %w", err)
	}
	return s, nil
}

// run samples the database on each tick until ctx is cancelled.
func (s *databaseSampler) run(ctx context.Context) {
	s.sample(ctx)
	ticker := time.NewTicker(s.every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sample(ctx)
		}
	}
}

// sample reads connection, lock and activity counts from Postgres.
func (s *databaseSampler) sample(ctx context.Context) {
	var connections, active, idleInTx, lockWaiters, ungranted int64
	err := s.pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM pg_stat_activity WHERE datname=current_database()),
		(SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND pid<>pg_backend_pid()),
		(SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND state='idle in transaction'),
		(SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock'),
		(SELECT count(*) FROM pg_locks WHERE NOT granted)`).Scan(&connections, &active, &idleInTx, &lockWaiters, &ungranted)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		if s.err == nil {
			s.err = fmt.Errorf("sample PostgreSQL: %w", err)
		}
		return
	}
	s.samples++
	s.connections.add(connections)
	s.active.add(active)
	s.idleInTx.add(idleInTx)
	s.lockWaiters.add(lockWaiters)
	s.ungranted.add(ungranted)
}

// finish takes a last sample and returns the database metrics for the run.
func (s *databaseSampler) finish(ctx context.Context) (DatabaseMetrics, error) {
	s.sample(ctx)
	var deadlocksAfter, bytesAfter, sessionsAfter int64
	if err := s.pool.QueryRow(ctx, `SELECT deadlocks, pg_database_size(current_database()), sessions
		FROM pg_stat_database WHERE datname=current_database()`).Scan(&deadlocksAfter, &bytesAfter, &sessionsAfter); err != nil {
		return DatabaseMetrics{}, fmt.Errorf("read final database metrics: %w", err)
	}

	pgStats := OptionalTelemetry{}
	var extensionAvailable bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname='pg_stat_statements')`).Scan(&extensionAvailable); err != nil {
		return DatabaseMetrics{}, fmt.Errorf("check pg_stat_statements: %w", err)
	}
	if extensionAvailable {
		var calls int64
		var totalExecMS float64
		if err := s.pool.QueryRow(ctx, `SELECT COALESCE(sum(calls),0), COALESCE(sum(total_exec_time),0)
			FROM pg_stat_statements WHERE dbid=(SELECT oid FROM pg_database WHERE datname=current_database())`).Scan(&calls, &totalExecMS); err != nil {
			pgStats.UnavailableReason = "extension exists but query failed: " + err.Error()
		} else {
			pgStats.Available = true
			pgStats.Values = map[string]any{"calls": calls, "total_exec_time_ms": totalExecMS}
		}
	} else {
		pgStats.UnavailableReason = "pg_stat_statements is not enabled in the local PostgreSQL image"
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return DatabaseMetrics{}, s.err
	}
	mean := func(g gaugeAccumulator) float64 {
		if s.samples == 0 {
			return 0
		}
		return g.total / float64(s.samples)
	}
	sessionsOpened := sessionsAfter - s.sessionsBefore
	containerUnavailable := ContainerMetrics{UnavailableReason: "Docker container CPU and memory sampling is not wired into the in-container runner"}
	return DatabaseMetrics{
		Available:           true,
		Samples:             s.samples,
		Connections:         GaugeSummary{Mean: mean(s.connections), Max: s.connections.max},
		ActiveConnections:   GaugeSummary{Mean: mean(s.active), Max: s.active.max},
		IdleInTransaction:   GaugeSummary{Mean: mean(s.idleInTx), Max: s.idleInTx.max},
		LockWaitingSessions: GaugeSummary{Mean: mean(s.lockWaiters), Max: s.lockWaiters.max},
		UngrantedLocks:      GaugeSummary{Mean: mean(s.ungranted), Max: s.ungranted.max},
		DeadlocksBefore:     s.deadlocksBefore,
		DeadlocksAfter:      deadlocksAfter,
		DatabaseBytesBefore: s.databaseBytesBefore,
		DatabaseBytesAfter:  bytesAfter,
		SessionsOpened:      &sessionsOpened,
		PostgresContainer:   containerUnavailable,
		PGStatStatements:    pgStats,
	}, nil
}

// checkInvariants runs the database checks that must hold after every run, such as stock never negative.
func checkInvariants(ctx context.Context, pool *pgxpool.Pool, client ClientMetrics, strictWorkflowCounts bool) ([]InvariantResult, error) {
	checks := []struct {
		name  string
		query string
	}{
		{"inventory counters stay within bounds", `SELECT count(*) FROM inventory.inventories WHERE on_hand < 0 OR reserved < 0 OR allocated < 0 OR reserved + allocated > on_hand`},
		{"one reservation per order", `SELECT count(*) FROM (SELECT order_id FROM inventory.reservations GROUP BY order_id HAVING count(*) > 1) x`},
		{"one payment per order", `SELECT count(*) FROM (SELECT order_id FROM payments.payment_attempts GROUP BY order_id HAVING count(*) > 1) x`},
		{"no duplicate product line per order", `SELECT count(*) FROM (SELECT order_id, product_id FROM orders.order_items GROUP BY order_id, product_id HAVING count(*) > 1) x`},
		{"paid orders have successful payment", `SELECT count(*) FROM orders.orders o LEFT JOIN payments.payment_attempts p ON p.order_id=o.id AND p.status='SUCCEEDED' WHERE o.status='PAID' AND p.id IS NULL`},
		{"cancelled orders have failed payment", `SELECT count(*) FROM orders.orders o LEFT JOIN payments.payment_attempts p ON p.order_id=o.id AND p.status='FAILED' WHERE o.status='CANCELLED' AND p.id IS NULL`},
		{"paid reservations are confirmed", `SELECT count(*) FROM orders.orders o JOIN inventory.reservations r ON r.order_id=o.id WHERE o.status='PAID' AND r.status<>'CONFIRMED'`},
		{"cancelled reservations are released", `SELECT count(*) FROM orders.orders o JOIN inventory.reservations r ON r.order_id=o.id WHERE o.status='CANCELLED' AND r.status<>'RELEASED'`},
		{"unknown payments retain pending orders", `SELECT count(*) FROM payments.payment_attempts p JOIN orders.orders o ON o.id=p.order_id WHERE p.status='UNKNOWN' AND o.status<>'PENDING_PAYMENT'`},
		{"payment amounts match order totals in paise", `SELECT count(*) FROM payments.payment_attempts p JOIN orders.orders o ON o.id=p.order_id WHERE p.amount_paise<>o.total_paise OR p.currency<>o.currency`},
	}
	results := make([]InvariantResult, 0, len(checks))
	for _, check := range checks {
		var violations int64
		if err := pool.QueryRow(ctx, check.query).Scan(&violations); err != nil {
			return nil, fmt.Errorf("check invariant %q: %w", check.name, err)
		}
		results = append(results, InvariantResult{Name: check.name, Passed: violations == 0, Violations: violations})
	}
	counts := []struct {
		name     string
		query    string
		expected int64
	}{}
	if strictWorkflowCounts {
		counts = append(counts,
			struct {
				name, query string
				expected    int64
			}{"one order per accepted checkout", `SELECT count(*) FROM orders.orders`, client.Accepted},
			struct {
				name, query string
				expected    int64
			}{"one confirmed reservation per paid order", `SELECT count(*) FROM inventory.reservations WHERE status='CONFIRMED'`, client.Paid},
			struct {
				name, query string
				expected    int64
			}{"one successful payment per paid order", `SELECT count(*) FROM payments.payment_attempts WHERE status='SUCCEEDED'`, client.Paid},
			struct {
				name, query string
				expected    int64
			}{"one released reservation per cancelled order", `SELECT count(*) FROM inventory.reservations WHERE status='RELEASED'`, client.Cancelled},
			struct {
				name, query string
				expected    int64
			}{"one failed payment per cancelled order", `SELECT count(*) FROM payments.payment_attempts WHERE status='FAILED'`, client.Cancelled},
			struct {
				name, query string
				expected    int64
			}{"one unique completed checkout attempt per offered workflow", `SELECT count(*) FROM orchestrator.checkout_attempts WHERE status='completed'`, client.Offered},
		)
	}
	for _, check := range counts {
		var actual int64
		if err := pool.QueryRow(ctx, check.query).Scan(&actual); err != nil {
			return nil, fmt.Errorf("check workload count %q: %w", check.name, err)
		}
		violations := actual - check.expected
		if violations < 0 {
			violations = -violations
		}
		results = append(results, InvariantResult{Name: check.name, Passed: actual == check.expected, Violations: violations})
	}
	return results, nil
}

// checkWorkloadInvariants runs the extra checks that belong to specific cases.
func checkWorkloadInvariants(ctx context.Context, pool *pgxpool.Pool, cfg config, products []string, client ClientMetrics) ([]InvariantResult, error) {
	if cfg.CaseName == "C11" {
		var changed int64
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM inventory.inventories WHERE on_hand<>$1 OR reserved<>0 OR allocated<>0`, fixtureStock).Scan(&changed); err != nil {
			return nil, fmt.Errorf("check decline inventory restoration: %w", err)
		}
		return []InvariantResult{{Name: "declined payments release every reservation without consuming stock", Passed: changed == 0 && client.Cancelled == client.Offered, Violations: changed}}, nil
	}
	if cfg.CaseName == "C12" {
		var pendingOrders, reservedReservations, reservedUnits, unknownPayments int64
		err := pool.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM orders.orders WHERE status='PENDING_PAYMENT'),
			(SELECT count(*) FROM inventory.reservations WHERE status='RESERVED'),
			(SELECT COALESCE(sum(i.reserved),0) FROM inventory.inventories i),
			(SELECT count(*) FROM payments.payment_attempts WHERE status='UNKNOWN')`).Scan(&pendingOrders, &reservedReservations, &reservedUnits, &unknownPayments)
		if err != nil {
			return nil, fmt.Errorf("check random-mode pending stock: %w", err)
		}
		violations := int64(0)
		if pendingOrders != client.PendingAtDrain {
			violations++
		}
		if reservedReservations != pendingOrders {
			violations++
		}
		if reservedUnits != pendingOrders {
			violations++
		}
		if unknownPayments != pendingOrders {
			violations++
		}
		return []InvariantResult{{Name: "random timeouts remain pending and hold exactly one reserved unit", Passed: violations == 0, Violations: violations}}, nil
	}
	if cfg.CaseName != "C5" || cfg.ExactOrders == 0 {
		return nil, nil
	}
	var onHand, reserved, allocated int64
	if err := pool.QueryRow(ctx, `SELECT on_hand, reserved, allocated FROM inventory.inventories WHERE product_id=$1`, products[0]).Scan(&onHand, &reserved, &allocated); err != nil {
		return nil, fmt.Errorf("check C5b hot-product inventory: %w", err)
	}
	violations := int64(0)
	// Confirming a reservation consumes on_hand and clears reserved in this
	// inventory model. The allocated column is not incremented by confirmation.
	if onHand != 0 {
		violations++
	}
	if reserved != 0 {
		violations++
	}
	if allocated != 0 {
		violations++
	}
	if client.Accepted != cfg.HotStock || client.Completed != cfg.HotStock {
		violations++
	}
	expectedConflicts := int64(cfg.ExactOrders) - cfg.HotStock
	if client.CheckoutStatusMix.Business409 != expectedConflicts {
		violations++
	}
	return []InvariantResult{{Name: "C5b consumes exactly available stock without oversell and rejects the remainder with 409", Passed: violations == 0, Violations: violations}}, nil
}
