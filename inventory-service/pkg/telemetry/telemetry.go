package telemetry

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"runtime"
	"sort"
	"sync"
	"time"
)

const endpoint = "/debug/load-metrics"

type Recorder struct {
	mu          sync.Mutex
	db          *sql.DB
	requests    int64
	errors      int64
	inFlight    int64
	maxInFlight int64
	latencies   []time.Duration
	dbBaseline  sql.DBStats
	memBaseline runtime.MemStats
}

type snapshot struct {
	Inbound requestMetrics `json:"inbound"`
	Runtime runtimeMetrics `json:"runtime"`
	DBPool  dbPoolMetrics  `json:"db_pool"`
}

type requestMetrics struct {
	Requests    int64          `json:"requests"`
	Errors      int64          `json:"errors"`
	InFlight    int64          `json:"in_flight"`
	MaxInFlight int64          `json:"max_in_flight"`
	Latency     latencySummary `json:"latency"`
}

type latencySummary struct {
	Samples int64   `json:"samples"`
	MinMS   float64 `json:"min_ms"`
	MeanMS  float64 `json:"mean_ms"`
	P50MS   float64 `json:"p50_ms"`
	P95MS   float64 `json:"p95_ms"`
	P99MS   float64 `json:"p99_ms"`
	MaxMS   float64 `json:"max_ms"`
}

type runtimeMetrics struct {
	Goroutines     int64  `json:"goroutines"`
	HeapAllocBytes uint64 `json:"heap_alloc_bytes"`
	HeapInUseBytes uint64 `json:"heap_in_use_bytes"`
	GCCycles       uint32 `json:"gc_cycles"`
	GCPauseTotalNS uint64 `json:"gc_pause_total_ns"`
}

type dbPoolMetrics struct {
	OpenConnections int           `json:"open_connections"`
	InUse           int           `json:"in_use"`
	Idle            int           `json:"idle"`
	WaitCount       int64         `json:"wait_count"`
	WaitDuration    time.Duration `json:"wait_duration"`
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func Enabled() bool { return os.Getenv("LOAD_TEST_TELEMETRY") == "true" }

func New(db *sql.DB) *Recorder {
	r := &Recorder{db: db}
	r.Reset()
	return r
}

func (r *Recorder) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET "+endpoint, r.handleGet)
	mux.HandleFunc("POST "+endpoint+"/reset", r.handleReset)
}

func (r *Recorder) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/health" || request.URL.Path == endpoint || request.URL.Path == endpoint+"/reset" {
			next.ServeHTTP(w, request)
			return
		}
		started := time.Now()
		r.mu.Lock()
		r.requests++
		r.inFlight++
		if r.inFlight > r.maxInFlight {
			r.maxInFlight = r.inFlight
		}
		r.mu.Unlock()

		writer := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(writer, request)

		r.mu.Lock()
		r.inFlight--
		if writer.status >= 500 {
			r.errors++
		}
		r.latencies = append(r.latencies, time.Since(started))
		r.mu.Unlock()
	})
}

func (r *Recorder) Reset() {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	r.mu.Lock()
	r.requests = 0
	r.errors = 0
	r.inFlight = 0
	r.maxInFlight = 0
	r.latencies = nil
	r.dbBaseline = r.db.Stats()
	r.memBaseline = memory
	r.mu.Unlock()
}

func (r *Recorder) handleReset(w http.ResponseWriter, _ *http.Request) {
	r.Reset()
	w.WriteHeader(http.StatusNoContent)
}

func (r *Recorder) handleGet(w http.ResponseWriter, _ *http.Request) {
	r.mu.Lock()
	requests, errors, inFlight, maxInFlight := r.requests, r.errors, r.inFlight, r.maxInFlight
	latencies := append([]time.Duration(nil), r.latencies...)
	dbBaseline, memBaseline := r.dbBaseline, r.memBaseline
	r.mu.Unlock()

	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	dbStats := r.db.Stats()
	result := snapshot{
		Inbound: requestMetrics{Requests: requests, Errors: errors, InFlight: inFlight, MaxInFlight: maxInFlight, Latency: summarize(latencies)},
		Runtime: runtimeMetrics{
			Goroutines: int64(runtime.NumGoroutine()), HeapAllocBytes: memory.HeapAlloc, HeapInUseBytes: memory.HeapInuse,
			GCCycles: memory.NumGC - memBaseline.NumGC, GCPauseTotalNS: memory.PauseTotalNs - memBaseline.PauseTotalNs,
		},
		DBPool: dbPoolMetrics{
			OpenConnections: dbStats.OpenConnections, InUse: dbStats.InUse, Idle: dbStats.Idle,
			WaitCount: dbStats.WaitCount - dbBaseline.WaitCount, WaitDuration: dbStats.WaitDuration - dbBaseline.WaitDuration,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func summarize(values []time.Duration) latencySummary {
	if len(values) == 0 {
		return latencySummary{}
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	var total time.Duration
	for _, value := range values {
		total += value
	}
	ms := func(value time.Duration) float64 { return float64(value.Microseconds()) / 1000 }
	percentile := func(p float64) float64 {
		index := int(float64(len(values)-1)*p + 0.999999999)
		return ms(values[index])
	}
	return latencySummary{
		Samples: int64(len(values)), MinMS: ms(values[0]), MeanMS: ms(total / time.Duration(len(values))),
		P50MS: percentile(.50), P95MS: percentile(.95), P99MS: percentile(.99), MaxMS: ms(values[len(values)-1]),
	}
}
