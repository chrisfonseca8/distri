// Package businessmetrics holds the payment-service business metrics. Event
// counters are incremented in the service code; backlog and status gauges are read
// from PostgreSQL (the authority) at scrape time.
package businessmetrics

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/chrisfonseca08/distri/payment-service/pkg/observability"
)

var (
	GatewayCalls = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "payment_gateway_calls_total", Help: "Gateway calls by operation and result (ok, rejected, not_sent, ambiguous)."},
		[]string{"operation", "result"})
	GatewayDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "payment_gateway_call_duration_seconds", Help: "Gateway call duration by operation.",
		Buckets: []float64{.01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30}}, []string{"operation"})
	// Unconfirmed counts attempts failed as UNCONFIRMED (no answer from the gateway
	// until the grace period ended).
	Unconfirmed = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "payment_unconfirmed_total", Help: "Payment attempts decided FAILED/UNCONFIRMED."})
	// RefundsRequired counts compensation records created, by reason.
	RefundsRequired = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "payment_refunds_required_total", Help: "Compensation (refund) records created, by reason."}, []string{"reason"})
	WebhookResults = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "payment_webhook_events_processed_total", Help: "Webhook events finished, by result (processed, ignored, parked, retried)."}, []string{"result"})
	WebhookDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "payment_webhook_processing_duration_seconds", Help: "Time to apply one webhook event.",
		Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5}})
)

// Register adds the payment metrics and the PostgreSQL-backed gauges.
func Register(db *sql.DB) {
	observability.Registry.MustRegister(GatewayCalls, GatewayDuration, Unconfirmed, RefundsRequired, WebhookResults, WebhookDuration)

	specs := []observability.GaugeSpec{
		{
			Name: "payment_attempts_by_status", Help: "Payment attempts by status.", Labels: []string{"status"},
			SQL:  `SELECT status, count(*)::float8 FROM payments.payment_attempts GROUP BY status`,
			Zero: [][]string{{"CREATED"}, {"PENDING"}, {"AWAITING_PAYMENT"}, {"SUCCEEDED"}, {"FAILED"}, {"UNKNOWN"}},
		},
		{
			Name: "payment_attempts_unconfirmed", Help: "Attempts that ended FAILED with failure_code UNCONFIRMED (the gateway never answered).",
			SQL: `SELECT count(*)::float8 FROM payments.payment_attempts WHERE status = 'FAILED' AND failure_code = 'UNCONFIRMED'`,
		},
		{
			Name: "payment_attempts_unknown_oldest_age_seconds", Help: "Age of the oldest attempt still UNKNOWN; 0 when none.",
			SQL: `SELECT COALESCE(EXTRACT(EPOCH FROM now() - min(updated_at)), 0)::float8 FROM payments.payment_attempts WHERE status = 'UNKNOWN'`,
		},
		{
			Name: "payment_webhook_backlog", Help: "Webhook events waiting to be applied.",
			SQL: `SELECT count(*)::float8 FROM payments.webhook_events WHERE status = 'PENDING'`,
		},
		{
			Name: "payment_webhook_oldest_pending_age_seconds", Help: "Age of the oldest webhook event waiting to be applied; 0 when none.",
			SQL: `SELECT COALESCE(EXTRACT(EPOCH FROM now() - min(received_at)), 0)::float8 FROM payments.webhook_events WHERE status = 'PENDING'`,
		},
		{
			Name: "payment_webhook_parked", Help: "Webhook events parked for an operator.",
			SQL: `SELECT count(*)::float8 FROM payments.webhook_events WHERE status = 'PARKED'`,
		},
		{
			Name: "payment_jobs", Help: "Payment jobs by status.", Labels: []string{"status"},
			SQL:  `SELECT status, count(*)::float8 FROM payments.jobs GROUP BY status`,
			Zero: [][]string{{"queued"}, {"running"}, {"succeeded"}, {"dead"}},
		},
		{
			Name: "payment_jobs_oldest_queued_age_seconds", Help: "Age of the oldest job that is queued and due; 0 when none.",
			SQL: `SELECT COALESCE(EXTRACT(EPOCH FROM now() - min(next_run_at)), 0)::float8 FROM payments.jobs WHERE status = 'queued' AND next_run_at <= now()`,
		},
		{
			Name: "payment_refunds_by_status", Help: "Compensation (refund) records by status.", Labels: []string{"status"},
			SQL:  `SELECT status, count(*)::float8 FROM payments.compensation_records GROUP BY status`,
			Zero: [][]string{{"REQUIRED"}, {"SUBMITTING"}, {"UNKNOWN"}, {"PENDING"}, {"PROCESSED"}, {"FAILED"}, {"REVIEW"}},
		},
		{
			Name: "payment_refunds_incomplete", Help: "Refunds required but not completed (any status other than PROCESSED).",
			SQL: `SELECT count(*)::float8 FROM payments.compensation_records WHERE status <> 'PROCESSED'`,
			After: func(ctx context.Context, rows []observability.GaugeRow) {
				if len(rows) == 1 && rows[0].Value > 0 {
					slog.Warn("refunds required but not completed", "incomplete_refunds", rows[0].Value)
				}
			},
		},
		{
			Name: "payment_refunds_oldest_incomplete_age_seconds", Help: "Age of the oldest refund that is required but not completed; 0 when none.",
			SQL: `SELECT COALESCE(EXTRACT(EPOCH FROM now() - min(created_at)), 0)::float8 FROM payments.compensation_records WHERE status <> 'PROCESSED'`,
		},
	}
	specs = append(specs, observability.OutboxSpecs("payments.outbox")...)
	observability.NewGaugeCollector(db, 5*time.Second, specs...)
}
