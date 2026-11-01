package observability

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry holds every metric of the process. No const labels are added here:
// Prometheus attaches service, environment and test_run_id as target labels, so
// the application never needs to know which run it is part of.
//
// Order ids, saga ids and message ids must NEVER be label values (unbounded
// cardinality); they belong in logs and traces.
var Registry = prometheus.NewRegistry()

var latencyBuckets = []float64{.001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30}

// Common metrics every service exposes.
var (
	HTTPRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total", Help: "Inbound HTTP requests by route, method and response class."},
		[]string{"route", "method", "code_class"})
	HTTPDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "http_request_duration_seconds", Help: "Inbound HTTP request duration.", Buckets: latencyBuckets},
		[]string{"route", "method"})
	HTTPInFlight = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "http_requests_active", Help: "Inbound HTTP requests currently being served."})
	HTTPClientRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_client_requests_total", Help: "Outbound HTTP calls by peer and result (code class or error)."},
		[]string{"peer", "result"})
	HTTPClientDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "http_client_request_duration_seconds", Help: "Outbound HTTP call duration.", Buckets: latencyBuckets},
		[]string{"peer"})

	Errors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "service_errors_total", Help: "Errors by component and kind."}, []string{"component", "kind"})
	Retries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "service_retries_total", Help: "Retries by component."}, []string{"component"})

	DBQueryDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "db_query_duration_seconds", Help: "GORM statement duration by operation.", Buckets: latencyBuckets},
		[]string{"operation"})
	DBErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "db_errors_total", Help: "Failed statements by kind (deadlock and serialization_failure are transaction failures)."},
		[]string{"operation", "kind"})
	DBTransactions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "db_transactions_total", Help: "Transactions run through observability.Transaction by name and result."},
		[]string{"name", "result"})

	RabbitPublish = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "rabbitmq_publish_total", Help: "Publish attempts by routing key and result (confirmed, nacked, unroutable, error)."},
		[]string{"routing_key", "result"})
	RabbitConsume = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "rabbitmq_consume_total", Help: "Deliveries handled by queue and outcome (acked, retried, dead_lettered, permanent_failure, requeued)."},
		[]string{"queue", "result"})
	RabbitConsumeDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "rabbitmq_consume_duration_seconds", Help: "Handler duration per delivery.", Buckets: latencyBuckets},
		[]string{"queue", "result"})
	RabbitRetries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "rabbitmq_retries_total", Help: "Deliveries re-published to a retry queue."}, []string{"queue"})
	RabbitDeadLetters = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "rabbitmq_dead_letters_total", Help: "Deliveries dead-lettered by queue and reason."}, []string{"queue", "reason"})
	RabbitConsumerConnected = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "rabbitmq_consumer_connected", Help: "Consumers on the queue currently holding a broker connection."}, []string{"queue"})

	OutboxPublish = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "outbox_publish_total", Help: "Outbox relay results (published, transient_failure, permanent_failure, dead, lease_lost)."},
		[]string{"result"})
	OutboxLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "outbox_publish_latency_seconds", Help: "Time from outbox row creation to confirmed publish.", Buckets: latencyBuckets},
		[]string{"routing_key"})
)

var (
	readyMu    sync.Mutex
	readyCheck func(context.Context) error
	readyAt    time.Time
	readyValue float64
)

// SetReadinessCheck sets the check behind the service_ready gauge (cached 3s).
func SetReadinessCheck(check func(context.Context) error) {
	readyMu.Lock()
	readyCheck = check
	readyMu.Unlock()
}

func readiness() float64 {
	readyMu.Lock()
	defer readyMu.Unlock()
	if readyCheck == nil {
		return 1
	}
	if time.Since(readyAt) < 3*time.Second {
		return readyValue
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	readyValue = 1
	if err := readyCheck(ctx); err != nil {
		readyValue = 0
		slog.WarnContext(ctx, "readiness check failed", "error", err)
	}
	readyAt = time.Now()
	return readyValue
}

var initMetricsOnce sync.Once

// InitMetrics registers the common collectors: Go runtime, process, readiness and the metrics above.
func InitMetrics() {
	initMetricsOnce.Do(func() {
		Registry.MustRegister(
			collectors.NewGoCollector(),
			collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
			prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "service_ready", Help: "1 when the readiness check passes."}, readiness),
			prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "service_build_info", Help: "Constant 1 labelled with the process identity."}, []string{"service_name", "environment_name"}),
			HTTPRequests, HTTPDuration, HTTPInFlight, HTTPClientRequests, HTTPClientDuration,
			Errors, Retries,
			DBQueryDuration, DBErrors, DBTransactions,
			RabbitPublish, RabbitConsume, RabbitConsumeDuration, RabbitRetries, RabbitDeadLetters, RabbitConsumerConnected,
			OutboxPublish, OutboxLatency,
		)
	})
}

// MetricsHandler serves the Prometheus exposition format.
func MetricsHandler() http.Handler {
	return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{ErrorLog: nil, Registry: Registry})
}

// ServeMetrics serves /metrics on addr until ctx ends. The listener is separate
// from the public API on purpose: it is published to loopback only and reached
// through an SSH tunnel. An empty addr disables it.
func ServeMetrics(ctx context.Context, addr string) {
	if addr == "" {
		return
	}
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", MetricsHandler())
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		slog.Info("metrics listening", "addr", addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("metrics server stopped", "error", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
}
