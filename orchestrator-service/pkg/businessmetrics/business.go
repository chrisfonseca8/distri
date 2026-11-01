// Package businessmetrics holds the orchestrator business metrics. Event counters
// are incremented after the causing transaction commits; saga gauges are read from
// PostgreSQL (the authority) at scrape time.
package businessmetrics

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/observability"
)

var durationBuckets = []float64{.01, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 300, 900}

var (
	SagasStarted = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "sagas_started_total", Help: "Sagas created."})
	SagaCompletions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "saga_completions_total", Help: "Sagas that reached a final status (COMPLETED, COMPENSATED)."}, []string{"status"})
	SagaDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "saga_duration_seconds", Help: "Saga creation to final status.", Buckets: durationBuckets}, []string{"status"})
	StepCompletions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "saga_step_completions_total", Help: "Saga steps finished, by step and outcome."}, []string{"step", "outcome"})
	StepDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "saga_step_duration_seconds", Help: "Saga step start to reply.", Buckets: durationBuckets}, []string{"step", "outcome"})
	StepRetries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "saga_step_retries_total", Help: "Saga step commands sent again (reply decision or sweeper resend)."}, []string{"step"})
	StuckTransitions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "saga_stuck_transitions_total", Help: "Sagas moved to STUCK, by reason."}, []string{"reason"})
	CompensationsStarted = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "saga_compensations_started_total", Help: "Sagas that began compensating."})
	Compensations = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "saga_compensation_steps_total", Help: "Compensation steps finished, by step and outcome."}, []string{"step", "outcome"})
	CompensationDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "saga_compensation_step_duration_seconds", Help: "Compensation step start to reply.", Buckets: durationBuckets}, []string{"step"})
)

// CompensationSteps are the undo steps of the checkout saga.
var CompensationSteps = map[string]bool{
	"RELEASE_STOCK": true, "MARK_ORDER_CANCELLED": true, "REFUND_PAYMENT": true, "CANCEL_PAID_ORDER": true,
}

// Register adds the orchestrator metrics and the PostgreSQL-backed gauges.
func Register(db *sql.DB) {
	observability.Registry.MustRegister(SagasStarted, SagaCompletions, SagaDuration, StepCompletions, StepDuration,
		StepRetries, StuckTransitions, CompensationsStarted, Compensations, CompensationDuration)
	specs := []observability.GaugeSpec{
		{
			Name: "sagas_by_status", Help: "Sagas by status.", Labels: []string{"status"},
			SQL:  `SELECT status, count(*)::float8 FROM orchestrator.sagas GROUP BY status`,
			Zero: [][]string{{"RUNNING"}, {"COMPLETED"}, {"COMPENSATING"}, {"COMPENSATED"}, {"STUCK"}},
			After: func(ctx context.Context, rows []observability.GaugeRow) {
				for _, r := range rows {
					if r.Labels[0] == "STUCK" && r.Value > 0 {
						slog.Error("sagas are STUCK", "event", observability.EventSagaStuck, "stuck_sagas", r.Value)
					}
				}
			},
		},
		{
			Name: "sagas_by_current_step", Help: "Unfinished sagas by current step and status.", Labels: []string{"step", "status"},
			SQL: `SELECT current_step, status, count(*)::float8 FROM orchestrator.sagas WHERE status IN ('RUNNING','COMPENSATING','STUCK') GROUP BY current_step, status`,
		},
		{
			Name: "saga_oldest_active_age_seconds", Help: "Age of the oldest RUNNING or COMPENSATING saga; 0 when none.",
			SQL: `SELECT COALESCE(EXTRACT(EPOCH FROM now() - min(created_at)), 0)::float8 FROM orchestrator.sagas WHERE status IN ('RUNNING','COMPENSATING')`,
		},
		{
			Name: "saga_steps_by_status", Help: "Saga steps of unfinished sagas by status.", Labels: []string{"status"},
			SQL: `SELECT st.status, count(*)::float8 FROM orchestrator.saga_steps st JOIN orchestrator.sagas sg ON sg.id = st.saga_id WHERE sg.status IN ('RUNNING','COMPENSATING','STUCK') GROUP BY st.status`,
		},
		{
			Name: "checkout_attempts_by_status", Help: "Checkout idempotency records by status.", Labels: []string{"status"},
			SQL:  `SELECT status, count(*)::float8 FROM orchestrator.checkout_attempts GROUP BY status`,
			Zero: [][]string{{"in_progress"}, {"completed"}},
		},
	}
	specs = append(specs, observability.OutboxSpecs("orchestrator.outbox")...)
	observability.NewGaugeCollector(db, 5*time.Second, specs...)
}
