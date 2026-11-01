// Package businessmetrics holds the order-service business metrics. Counters are
// incremented after the transaction that caused them commits; gauges are read from
// PostgreSQL (the authority) at scrape time.
package businessmetrics

import (
	"database/sql"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/chrisfonseca08/distri/order-service/pkg/observability"
)

var (
	// Transitions counts committed order state changes by target status
	// (created is PENDING_PAYMENT, paid is PAID, cancelled is CANCELLED).
	Transitions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "orders_transitions_total", Help: "Committed order transitions by target status."}, []string{"to"})
	// IdempotentReplays counts commands whose effect was already applied.
	IdempotentReplays = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "orders_idempotent_replays_total", Help: "Order commands that changed nothing because they were already applied."}, []string{"command"})
	// Conflicts counts commands that disagree with stored state (a different order under the same id, or a transition out of a terminal state).
	Conflicts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "orders_conflicts_total", Help: "Order commands rejected because they conflict with stored state."}, []string{"command"})
	// TransitionFailures counts transitions that failed (database errors, lost guarded updates).
	TransitionFailures = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "orders_transition_failures_total", Help: "Order transitions that failed, by target status and kind."}, []string{"to", "kind"})
)

// Register adds the order metrics and the PostgreSQL-backed gauges.
func Register(db *sql.DB) {
	observability.Registry.MustRegister(Transitions, IdempotentReplays, Conflicts, TransitionFailures)
	specs := []observability.GaugeSpec{
		{
			Name: "orders_by_status", Help: "Orders by status.", Labels: []string{"status"},
			SQL:  `SELECT status, count(*)::float8 FROM orders.orders GROUP BY status`,
			Zero: [][]string{{"PENDING_PAYMENT"}, {"PAID"}, {"CANCELLED"}, {"EXPIRED"}},
		},
		{
			Name: "orders_oldest_pending_payment_age_seconds", Help: "Age of the oldest order still waiting for payment; 0 when none.",
			SQL: `SELECT COALESCE(EXTRACT(EPOCH FROM now() - min(created_at)), 0)::float8 FROM orders.orders WHERE status = 'PENDING_PAYMENT'`,
		},
	}
	specs = append(specs, observability.OutboxSpecs("orders.outbox")...)
	observability.NewGaugeCollector(db, 5*time.Second, specs...)
}
