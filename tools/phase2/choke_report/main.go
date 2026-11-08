package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type distribution struct {
	Samples int64   `json:"samples"`
	MeanMS  float64 `json:"mean_ms"`
	P50MS   float64 `json:"p50_ms"`
	P95MS   float64 `json:"p95_ms"`
	P99MS   float64 `json:"p99_ms"`
	MaxMS   float64 `json:"max_ms"`
}

type rawCollection struct {
	Runs []rawRun `json:"runs"`
}
type rawRun struct {
	Label      string    `json:"label"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	Client     struct {
		Offered       int64        `json:"offered"`
		Accepted      int64        `json:"accepted"`
		Completed     int64        `json:"completed"`
		Pending       int64        `json:"pending_at_drain"`
		AcceptedRate  float64      `json:"accepted_per_second"`
		CompletedRate float64      `json:"completed_orders_per_second"`
		Checkout      distribution `json:"checkout_latency"`
		EndToEnd      distribution `json:"end_to_end_latency"`
		RunnerCPU     *float64     `json:"runner_cpu_percent"`
	} `json:"client"`
	Services map[string]struct {
		DBPool *struct {
			WaitCount      int64   `json:"wait_count"`
			WaitDurationNS float64 `json:"wait_duration"`
		} `json:"db_pool"`
		Container *struct {
			CPUMean    *float64 `json:"cpu_mean_percent"`
			CPUMax     *float64 `json:"cpu_max_percent"`
			MemoryMean *uint64  `json:"memory_mean_bytes"`
			MemoryMax  *uint64  `json:"memory_max_bytes"`
		} `json:"container"`
	} `json:"services"`
	Database struct {
		Postgres *struct {
			CPUMean *float64 `json:"cpu_mean_percent"`
			CPUMax  *float64 `json:"cpu_max_percent"`
		} `json:"postgres_container"`
	} `json:"database"`
}

type diagnostic struct {
	Clients                 int                           `json:"clients"`
	IntentionalDelayMS      float64                       `json:"intentional_customer_delay_ms"`
	Offered                 int64                         `json:"offered"`
	Accepted                int64                         `json:"accepted"`
	Completed               int64                         `json:"completed"`
	Pending                 int64                         `json:"pending_at_drain"`
	AcceptedRate            float64                       `json:"accepted_per_second"`
	CompletedRate           float64                       `json:"completed_per_second"`
	Checkout                distribution                  `json:"checkout_latency"`
	EndToEnd                distribution                  `json:"end_to_end_latency_including_customer_delay"`
	AdjustedEndToEnd        distribution                  `json:"adjusted_end_to_end_latency"`
	SagaSteps               map[string]distribution       `json:"saga_step_round_trip"`
	AdjustedSagaSteps       map[string]distribution       `json:"adjusted_saga_step_round_trip"`
	CommandPublish          map[string]distribution       `json:"command_outbox_publish_lag"`
	CommandDelivery         map[string]distribution       `json:"command_broker_plus_handler"`
	ReplyDelivery           map[string]distribution       `json:"reply_broker_plus_orchestrator_handler"`
	PaymentQueueWait        distribution                  `json:"payment_job_queue_wait"`
	PaymentWorkerProcessing distribution                  `json:"payment_worker_processing"`
	PaymentJobStatuses      map[string]int64              `json:"payment_job_statuses_at_end"`
	OldestPaymentBacklogMS  float64                       `json:"oldest_payment_backlog_ms_at_end"`
	RunnerCPU               *float64                      `json:"runner_cpu_percent,omitempty"`
	ServiceCPU              map[string]map[string]float64 `json:"service_cpu_percent"`
	DBPoolWaits             map[string]map[string]float64 `json:"db_pool_waits"`
	PostgresCPU             map[string]float64            `json:"postgres_cpu_percent"`
}

func main() {
	clients := flag.Int("clients", 0, "closed-loop users")
	rawPath := flag.String("raw", "", "load-test.json")
	outPath := flag.String("out", "", "diagnostic json")
	delayMS := flag.Float64("customer-delay-ms", 0, "intentional client delay to subtract (post-OR9 default: none)")
	flag.Parse()
	if *clients <= 0 || *rawPath == "" || *outPath == "" {
		panic("-clients, -raw and -out are required")
	}

	var collection rawCollection
	raw, err := os.ReadFile(*rawPath)
	must(err)
	must(json.Unmarshal(raw, &collection))
	if len(collection.Runs) != 1 {
		panic(fmt.Sprintf("want one run, got %d", len(collection.Runs)))
	}
	r := collection.Runs[0]
	ctx := context.Background()
	db, err := pgx.Connect(ctx, required("DATABASE_URL"))
	must(err)
	defer db.Close(ctx)

	d := diagnostic{Clients: *clients, IntentionalDelayMS: *delayMS,
		Offered: r.Client.Offered, Accepted: r.Client.Accepted, Completed: r.Client.Completed, Pending: r.Client.Pending,
		AcceptedRate: r.Client.AcceptedRate, CompletedRate: r.Client.CompletedRate,
		Checkout: r.Client.Checkout, EndToEnd: r.Client.EndToEnd,
		AdjustedEndToEnd: subtract(r.Client.EndToEnd, *delayMS), RunnerCPU: r.Client.RunnerCPU,
		SagaSteps: map[string]distribution{}, AdjustedSagaSteps: map[string]distribution{}, CommandPublish: map[string]distribution{},
		CommandDelivery: map[string]distribution{}, ReplyDelivery: map[string]distribution{},
		PaymentJobStatuses: map[string]int64{},
		ServiceCPU:         map[string]map[string]float64{}, DBPoolWaits: map[string]map[string]float64{}, PostgresCPU: map[string]float64{},
	}
	for _, step := range []string{"CREATE_ORDER", "TAKE_PAYMENT", "MARK_ORDER_PAID", "CONFIRM_STOCK"} {
		d.SagaSteps[step] = dist(ctx, db, `SELECT extract(epoch FROM (finished_at-started_at))*1000 AS ms FROM orchestrator.saga_steps WHERE step=$1 AND finished_at IS NOT NULL`, step)
		d.AdjustedSagaSteps[step] = d.SagaSteps[step]
	}
	d.AdjustedSagaSteps["TAKE_PAYMENT"] = subtract(d.AdjustedSagaSteps["TAKE_PAYMENT"], *delayMS)
	for _, routing := range []string{"order.create", "payment.initiate", "order.mark-paid", "inventory.confirm"} {
		d.CommandPublish[routing] = dist(ctx, db, `SELECT extract(epoch FROM (published_at-created_at))*1000 AS ms FROM orchestrator.outbox WHERE routing_key=$1 AND published_at IS NOT NULL`, routing)
		var target string
		switch {
		case strings.HasPrefix(routing, "order."):
			target = "orders"
		case strings.HasPrefix(routing, "payment."):
			target = "payments"
		default:
			target = "inventory"
		}
		query := fmt.Sprintf(`SELECT extract(epoch FROM (p.processed_at-o.published_at))*1000 AS ms FROM orchestrator.outbox o JOIN %s.processed_messages p ON p.message_id=o.message_id::text WHERE o.routing_key=$1 AND o.published_at IS NOT NULL`, target)
		d.CommandDelivery[routing] = dist(ctx, db, query, routing)
	}
	for _, source := range []string{"orders", "payments", "inventory"} {
		query := fmt.Sprintf(`SELECT extract(epoch FROM (p.processed_at-o.published_at))*1000 AS ms FROM %s.outbox o JOIN orchestrator.processed_messages p ON p.message_id=o.message_id::text WHERE o.published_at IS NOT NULL`, source)
		d.ReplyDelivery[source] = dist(ctx, db, query)
	}
	// The payment-job transition audit is test-only instrumentation. Keep the
	// rest of the diagnostic useful when it has not been installed yet (for
	// example on the first cloud baseline), and make the missing samples
	// explicit as zero rather than aborting after a successful load run.
	if relationExists(ctx, db, "public.phase2_payment_job_audit") {
		d.PaymentQueueWait = dist(ctx, db, `SELECT extract(epoch FROM (a.running_at-j.created_at))*1000 AS ms FROM payments.jobs j JOIN public.phase2_payment_job_audit a ON a.job_id=j.id WHERE a.running_at IS NOT NULL AND j.created_at BETWEEN $1 AND $2`, r.StartedAt, r.FinishedAt)
		d.PaymentWorkerProcessing = dist(ctx, db, `SELECT extract(epoch FROM (a.succeeded_at-a.running_at))*1000 AS ms FROM payments.jobs j JOIN public.phase2_payment_job_audit a ON a.job_id=j.id WHERE a.running_at IS NOT NULL AND a.succeeded_at IS NOT NULL AND j.created_at BETWEEN $1 AND $2`, r.StartedAt, r.FinishedAt)
	}
	rows, err := db.Query(ctx, `SELECT status, count(*) FROM payments.jobs WHERE created_at BETWEEN $1 AND $2 GROUP BY status`, r.StartedAt, r.FinishedAt)
	must(err)
	for rows.Next() {
		var status string
		var count int64
		must(rows.Scan(&status, &count))
		d.PaymentJobStatuses[status] = count
	}
	rows.Close()
	must(rows.Err())
	must(db.QueryRow(ctx, `SELECT coalesce(extract(epoch FROM ($2-min(created_at)))*1000, 0) FROM payments.jobs WHERE status IN ('pending','processing','queued','running') AND created_at BETWEEN $1 AND $2`, r.StartedAt, r.FinishedAt).Scan(&d.OldestPaymentBacklogMS))
	for name, s := range r.Services {
		if s.Container != nil && s.Container.CPUMean != nil && s.Container.CPUMax != nil {
			d.ServiceCPU[name] = map[string]float64{"mean": *s.Container.CPUMean, "max": *s.Container.CPUMax}
		}
		if s.DBPool != nil {
			d.DBPoolWaits[name] = map[string]float64{"count": float64(s.DBPool.WaitCount), "duration_ms": s.DBPool.WaitDurationNS / 1e6}
		}
	}
	if r.Database.Postgres != nil && r.Database.Postgres.CPUMean != nil && r.Database.Postgres.CPUMax != nil {
		d.PostgresCPU = map[string]float64{"mean": *r.Database.Postgres.CPUMean, "max": *r.Database.Postgres.CPUMax}
	}
	encoded, err := json.MarshalIndent(d, "", "  ")
	must(err)
	must(os.MkdirAll(filepath.Dir(*outPath), 0755))
	must(os.WriteFile(*outPath, append(encoded, '\n'), 0644))
	printSummary(d)
}

func dist(ctx context.Context, db *pgx.Conn, base string, args ...any) distribution {
	query := `WITH x AS (` + base + `) SELECT count(*),coalesce(avg(ms),0),coalesce(percentile_cont(.5) within group(order by ms),0),coalesce(percentile_cont(.95) within group(order by ms),0),coalesce(percentile_cont(.99) within group(order by ms),0),coalesce(max(ms),0) FROM x`
	var d distribution
	must(db.QueryRow(ctx, query, args...).Scan(&d.Samples, &d.MeanMS, &d.P50MS, &d.P95MS, &d.P99MS, &d.MaxMS))
	return d
}
func subtract(d distribution, value float64) distribution {
	d.MeanMS = max(0, d.MeanMS-value)
	d.P50MS = max(0, d.P50MS-value)
	d.P95MS = max(0, d.P95MS-value)
	d.P99MS = max(0, d.P99MS-value)
	d.MaxMS = max(0, d.MaxMS-value)
	return d
}
func relationExists(ctx context.Context, db *pgx.Conn, name string) bool {
	var exists bool
	must(db.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, name).Scan(&exists))
	return exists
}
func printSummary(d diagnostic) {
	keys := make([]string, 0, len(d.SagaSteps))
	for k := range d.SagaSteps {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Printf("clients=%d offered=%d completed=%d rate=%.2f/s adjusted-p95=%.1fms payment-queue-p95=%.1fms payment-worker-p95=%.1fms", d.Clients, d.Offered, d.Completed, d.CompletedRate, d.AdjustedEndToEnd.P95MS, d.PaymentQueueWait.P95MS, d.PaymentWorkerProcessing.P95MS)
	for _, k := range keys {
		fmt.Printf(" %s-p95=%.1fms", k, d.SagaSteps[k].P95MS)
	}
	fmt.Println()
}
func required(k string) string {
	v := os.Getenv(k)
	if v == "" {
		panic(k + " required")
	}
	return v
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
