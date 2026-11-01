// Package businessmetrics holds the inventory-service business metrics. Event
// counters are incremented in the service code; stock and reservation gauges are
// read from PostgreSQL (the authority) at scrape time.
package businessmetrics

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/chrisfonseca08/distri/inventory-service/pkg/observability"
)

var (
	// Reservations counts create-reservation outcomes: reserved, replayed,
	// insufficient_stock, invalid, error.
	Reservations = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "inventory_reservations_total", Help: "Create-reservation outcomes."}, []string{"result"})
	// InsufficientStock counts insufficient-stock responses (also in Reservations).
	InsufficientStock = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "inventory_insufficient_stock_total", Help: "Reservation requests refused because stock was insufficient."})
	ReservationDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "inventory_reservation_duration_seconds", Help: "Create-reservation duration including conflict retries.",
		Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}}, []string{"result"})
	// Commands counts confirm/release outcomes (succeeded, failed, replayed).
	Commands = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "inventory_commands_total", Help: "Confirm/release command outcomes."}, []string{"command", "result"})
	// StockInvariantFailures counts operations that hit the stock invariant guard.
	StockInvariantFailures = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "inventory_stock_invariant_failures_total", Help: "Stock movements refused or failed by the stock invariant guard."})
)

// Register adds the inventory metrics and the PostgreSQL-backed gauges.
func Register(db *sql.DB) {
	observability.Registry.MustRegister(Reservations, InsufficientStock, ReservationDuration, Commands, StockInvariantFailures)

	ttl := 10 * time.Minute
	if d, err := time.ParseDuration(os.Getenv("RESERVATION_SWEEP_TTL")); err == nil && d > 0 {
		ttl = d
	}
	specs := []observability.GaugeSpec{
		{
			Name: "inventory_units", Help: "Stock units by state (on_hand, reserved, allocated, available).", Labels: []string{"state"},
			SQL: `SELECT s.state, s.n::float8 FROM (
				SELECT 'on_hand' AS state, COALESCE(sum(on_hand),0) AS n FROM inventory.inventories
				UNION ALL SELECT 'reserved', COALESCE(sum(reserved),0) FROM inventory.inventories
				UNION ALL SELECT 'allocated', COALESCE(sum(allocated),0) FROM inventory.inventories
				UNION ALL SELECT 'available', COALESCE(sum(on_hand - reserved - allocated),0) FROM inventory.inventories) s`,
			Zero: [][]string{{"on_hand"}, {"reserved"}, {"allocated"}, {"available"}},
		},
		{
			Name: "inventory_reservations_by_status", Help: "Reservations by status.", Labels: []string{"status"},
			SQL:  `SELECT status, count(*)::float8 FROM inventory.reservations GROUP BY status`,
			Zero: [][]string{{"PENDING"}, {"RESERVED"}, {"CONFIRMED"}, {"RELEASED"}},
		},
		{
			Name: "inventory_expired_reservation_backlog", Help: "Unconfirmed reservations older than RESERVATION_SWEEP_TTL that the sweeper has not released yet.",
			SQL: `SELECT count(*)::float8 FROM inventory.reservations WHERE status IN ('PENDING','RESERVED') AND created_at < now() - ` +
				strconv.FormatFloat(ttl.Seconds(), 'f', 0, 64) + ` * interval '1 second'`,
		},
		{
			Name: "inventory_stock_invariant_violations", Help: "Products whose stock breaks reserved >= 0, allocated >= 0, reserved + allocated <= on_hand.",
			SQL: `SELECT count(*)::float8 FROM inventory.inventories WHERE on_hand < 0 OR reserved < 0 OR allocated < 0 OR reserved + allocated > on_hand`,
			After: func(ctx context.Context, rows []observability.GaugeRow) {
				if len(rows) == 1 && rows[0].Value > 0 {
					observability.Event(ctx, slog.LevelError, observability.EventStockInvariantFailed,
						"stock invariant violated in inventory.inventories", "violating_products", rows[0].Value)
				}
			},
		},
	}
	specs = append(specs, observability.OutboxSpecs("inventory.outbox")...)
	observability.NewGaugeCollector(db, 5*time.Second, specs...)
}
