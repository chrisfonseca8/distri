// Package loadtest defines the stable report contract for local load tests.
// Collectors populate these structures; unavailable telemetry is explicit.
package main

import "time"

type Report struct {
	Suite       string                    `json:"suite"`
	RunID       string                    `json:"run_id"`
	Label       string                    `json:"label"`
	StartedAt   time.Time                 `json:"started_at"`
	FinishedAt  time.Time                 `json:"finished_at"`
	Config      RunConfig                 `json:"config"`
	Client      ClientMetrics             `json:"client"`
	Services    map[string]ServiceMetrics `json:"services"`
	Database    DatabaseMetrics           `json:"database"`
	Diagnostics []string                  `json:"diagnostics,omitempty"`
	Invariants  []InvariantResult         `json:"invariants"`
	// PreviousRuns holds the headline numbers of earlier runs of the same label, so
	// repeats and their spread stay visible after a run is replaced.
	PreviousRuns []RunSummary `json:"previous_runs,omitempty"`
}

// RunSummary is the headline of one earlier run of a label.
type RunSummary struct {
	StartedAt        time.Time `json:"started_at"`
	CompletedRate    float64   `json:"completed_per_second"`
	EndToEndP95MS    float64   `json:"end_to_end_p95_ms"`
	SessionsOpened   *int64    `json:"sessions_opened,omitempty"`
	GatewayMode      string    `json:"gateway_mode"`
	GatewayLatencyMS int64     `json:"gateway_latency_ms"`
	DBPoolSize       int       `json:"db_pool_size,omitempty"`
	DBIdleSize       int       `json:"db_idle_size,omitempty"`
}

type ReportCollection struct {
	Suite            string            `json:"suite"`
	UpdatedAt        time.Time         `json:"updated_at"`
	Runs             []Report          `json:"runs"`
	BaselineVariance *BaselineVariance `json:"baseline_variance,omitempty"`
	C3Sweep          []SweepPoint      `json:"c3_client_sweep,omitempty"`
	C3KneeClients    *int              `json:"c3_knee_clients,omitempty"`
	C4Sweep          []SweepPoint      `json:"c4_rate_sweep,omitempty"`
	C4SaturationRate *float64          `json:"c4_saturation_rate,omitempty"`
	ContentionWave   []ContentionPoint `json:"c5_c7_contention_wave,omitempty"`
	BehaviourWave    []ScenarioPoint   `json:"c9_c12_behaviour_wave,omitempty"`
	C13LatencySweep  []ScenarioPoint   `json:"c13_gateway_latency_sweep,omitempty"`
	C14PoolSweep     []ScenarioPoint   `json:"c14_pool_size_sweep,omitempty"`
}

// ScenarioPoint is one run flattened for the C9-C14 summary tables.
type ScenarioPoint struct {
	Label                  string  `json:"label"`
	Case                   string  `json:"case"`
	GatewayMode            string  `json:"gateway_mode"`
	GatewayLatencyMS       int64   `json:"gateway_latency_ms"`
	DBPoolSize             int     `json:"db_pool_size,omitempty"`
	DBIdleSize             int     `json:"db_idle_size,omitempty"`
	Clients                int     `json:"clients,omitempty"`
	SessionsOpened         *int64  `json:"sessions_opened,omitempty"`
	PoolWaitMSPerOrder     float64 `json:"pool_wait_ms_per_order"`
	PaymentReqPerOrder     float64 `json:"payment_requests_per_order"`
	Offered                int64   `json:"offered"`
	Paid                   int64   `json:"paid"`
	Cancelled              int64   `json:"cancelled"`
	Pending                int64   `json:"pending_at_drain"`
	CompletedRate          float64 `json:"completed_per_second"`
	CheckoutP99MS          float64 `json:"checkout_p99_ms"`
	PayP50MS               float64 `json:"pay_p50_ms"`
	PayP99MS               float64 `json:"pay_p99_ms"`
	EndToEndP99MS          float64 `json:"end_to_end_p99_ms"`
	MaxInFlightRequests    int64   `json:"max_in_flight_requests"`
	Server5xx              int64   `json:"server_5xx"`
	TransportErrors        int64   `json:"transport_errors"`
	OrderPoolWaits         int64   `json:"order_pool_waits"`
	OrderPoolWaitMS        float64 `json:"order_pool_wait_ms"`
	InventoryPoolWaits     int64   `json:"inventory_pool_waits"`
	InventoryPoolWaitMS    float64 `json:"inventory_pool_wait_ms"`
	PaymentPoolWaits       int64   `json:"payment_pool_waits"`
	PaymentPoolWaitMS      float64 `json:"payment_pool_wait_ms"`
	PostgresConnectionsMax int64   `json:"postgres_connections_max"`
	PostgresActiveMax      int64   `json:"postgres_active_max"`
	IdleInTransactionMax   int64   `json:"idle_in_transaction_max"`
	LockWaitersMax         int64   `json:"lock_waiters_max"`
	DeadlocksDelta         int64   `json:"deadlocks_delta"`
}

type ContentionPoint struct {
	Label            string  `json:"label"`
	Offered          int64   `json:"offered"`
	CompletedRate    float64 `json:"completed_per_second"`
	Checkout409      int64   `json:"checkout_409"`
	CheckoutP99MS    float64 `json:"checkout_p99_ms"`
	EndToEndP99MS    float64 `json:"end_to_end_p99_ms"`
	LockWaitersMax   int64   `json:"lock_waiters_max"`
	DeadlocksDelta   int64   `json:"deadlocks_delta"`
	OrderDBWaitCount int64   `json:"order_db_wait_count"`
	OrderDBWaitMS    float64 `json:"order_db_wait_ms"`
}

type SweepPoint struct {
	Label           string  `json:"label"`
	Clients         int     `json:"clients,omitempty"`
	OfferedRate     float64 `json:"offered_per_second"`
	AcceptedRate    float64 `json:"accepted_per_second"`
	CompletedRate   float64 `json:"completed_per_second"`
	EndToEndP99MS   float64 `json:"end_to_end_p99_ms"`
	MaxInFlight     int64   `json:"max_in_flight"`
	Server5xx       int64   `json:"server_5xx"`
	TransportErrors int64   `json:"transport_errors"`
}

type BaselineVariance struct {
	Runs          int             `json:"runs"`
	AcceptedRate  VarianceSummary `json:"accepted_per_second"`
	CompletedRate VarianceSummary `json:"completed_orders_per_second"`
	CheckoutP95MS VarianceSummary `json:"checkout_p95_ms"`
	PayP95MS      VarianceSummary `json:"pay_p95_ms"`
	EndToEndP95MS VarianceSummary `json:"end_to_end_p95_ms"`
}

type VarianceSummary struct {
	Mean           float64 `json:"mean"`
	Min            float64 `json:"min"`
	Max            float64 `json:"max"`
	StdDev         float64 `json:"standard_deviation"`
	CoefficientPct float64 `json:"coefficient_of_variation_percent"`
}

type InvariantResult struct {
	Name       string `json:"name"`
	Passed     bool   `json:"passed"`
	Violations int64  `json:"violations"`
}

type RunConfig struct {
	Case                 string        `json:"case"`
	Clients              int           `json:"clients"`
	ExactOrders          int           `json:"exact_orders,omitempty"`
	ProductCount         int           `json:"product_count"`
	Workload             string        `json:"workload"`
	HotStock             int64         `json:"hot_stock,omitempty"`
	OfferedRatePerSecond float64       `json:"offered_rate_per_second"`
	Warmup               time.Duration `json:"warmup"`
	Measurement          time.Duration `json:"measurement"`
	DrainTimeout         time.Duration `json:"drain_timeout"`
	StatusPollInterval   time.Duration `json:"status_poll_interval"`
	CompletionMode       string        `json:"completion_mode"`
	SampleInterval       time.Duration `json:"sample_interval"`
	MaxInFlight          int           `json:"max_in_flight"`
	GatewayMode          string        `json:"gateway_mode"`
	GatewayLatency       time.Duration `json:"gateway_latency"`
	Behavior             string        `json:"behavior,omitempty"`
	DBPoolSize           int           `json:"db_pool_size,omitempty"`
	DBIdleSize           int           `json:"db_idle_size,omitempty"`
	Environment          string        `json:"environment,omitempty"`
	MaxConnections       int           `json:"max_connections,omitempty"`
	Hardware             HardwareInfo  `json:"hardware"`
}

type HardwareInfo struct {
	LogicalCPUs int    `json:"logical_cpu_count"`
	MemoryBytes uint64 `json:"memory_bytes"`
	Source      string `json:"source"`
}

type ClientMetrics struct {
	Offered         int64          `json:"offered"`
	OfferedRate     float64        `json:"offered_rate_per_second"`
	Accepted        int64          `json:"accepted"`
	AcceptedRate    float64        `json:"accepted_per_second"`
	Completed       int64          `json:"completed"`
	CompletedRate   float64        `json:"completed_orders_per_second"`
	Paid            int64          `json:"paid"`
	Cancelled       int64          `json:"cancelled"`
	PendingAtDrain  int64          `json:"pending_at_drain"`
	CheckoutLatency LatencySummary `json:"checkout_latency"`
	PayLatency      LatencySummary `json:"pay_latency"`
	EndToEndLatency LatencySummary `json:"end_to_end_latency"`
	// ServerCompletionLatency measures terminal saga updated_at-created_at. Unlike
	// EndToEndLatency it excludes client polling and HTTP observation delay.
	ServerCompletionLatency LatencySummary    `json:"server_completion_latency"`
	CompletionObservation   CompletionMetrics `json:"completion_observation"`
	CheckoutStatusMix       StatusMix         `json:"checkout_status_mix"`
	PayStatusMix            StatusMix         `json:"pay_status_mix"`
	InFlight                InFlightSummary   `json:"in_flight"`
	LittleLaw               LittleLawCheck    `json:"little_law"`
	Duplicate               DuplicateMetrics  `json:"duplicate,omitempty"`
	// RunnerCPUPercent and RunnerMemoryBytes describe the load generator itself
	// (percent of one core over the measured window; cgroup memory at the end), so a
	// saturated generator is visible instead of being mistaken for server limits.
	RunnerCPUPercent  *float64 `json:"runner_cpu_percent,omitempty"`
	RunnerMemoryBytes *uint64  `json:"runner_memory_bytes,omitempty"`
}

// CompletionMetrics proves whether an SSE-configured run actually observed
// terminal events over streams or had to recover through status polling.
type CompletionMetrics struct {
	SSEAttempts       int64 `json:"sse_attempts"`
	SSETerminalEvents int64 `json:"sse_terminal_events"`
	PollingFallbacks  int64 `json:"polling_fallbacks"`
}

type DuplicateMetrics struct {
	InjectedWorkflows int64 `json:"injected_workflows"`
	ExtraRequests     int64 `json:"extra_requests"`
	Replay200         int64 `json:"replay_200"`
	Mismatches        int64 `json:"mismatches"`
}

type LatencySummary struct {
	Samples int64   `json:"samples"`
	MinMS   float64 `json:"min_ms"`
	MeanMS  float64 `json:"mean_ms"`
	P50MS   float64 `json:"p50_ms"`
	P95MS   float64 `json:"p95_ms"`
	P99MS   float64 `json:"p99_ms"`
	MaxMS   float64 `json:"max_ms"`
}

type StatusMix struct {
	ByHTTPStatus    map[string]int64 `json:"by_http_status"`
	Accepted2xx     int64            `json:"accepted_2xx"`
	Business409     int64            `json:"business_conflict_409"`
	Server5xx       int64            `json:"server_5xx"`
	TransportErrors int64            `json:"transport_errors"`
	DecodeErrors    int64            `json:"decode_errors"`
}

type InFlightSummary struct {
	MeanRequests  float64 `json:"mean_requests"`
	MaxRequests   int64   `json:"max_requests"`
	MeanWorkflows float64 `json:"mean_workflows"`
	MaxWorkflows  int64   `json:"max_workflows"`
}

type LittleLawCheck struct {
	ObservedMeanInFlight float64 `json:"observed_mean_in_flight"`
	ExpectedMeanInFlight float64 `json:"expected_mean_in_flight"`
	DifferencePercent    float64 `json:"difference_percent"`
}

type ServiceMetrics struct {
	Available         bool                       `json:"available"`
	UnavailableReason string                     `json:"unavailable_reason,omitempty"`
	Inbound           *RequestMetrics            `json:"inbound,omitempty"`
	Outbound          map[string]*RequestMetrics `json:"outbound,omitempty"`
	Runtime           *RuntimeMetrics            `json:"runtime,omitempty"`
	DBPool            *DBPoolMetrics             `json:"db_pool,omitempty"`
	Container         *ContainerMetrics          `json:"container,omitempty"`
}

type RequestMetrics struct {
	Requests    int64          `json:"requests"`
	Errors      int64          `json:"errors"`
	InFlight    int64          `json:"in_flight"`
	MaxInFlight int64          `json:"max_in_flight"`
	Latency     LatencySummary `json:"latency"`
}

type RuntimeMetrics struct {
	Goroutines     int64  `json:"goroutines"`
	HeapAllocBytes uint64 `json:"heap_alloc_bytes"`
	HeapInUseBytes uint64 `json:"heap_in_use_bytes"`
	GCCycles       uint32 `json:"gc_cycles"`
	GCPauseTotalNS uint64 `json:"gc_pause_total_ns"`
}

type DBPoolMetrics struct {
	OpenConnections int           `json:"open_connections"`
	InUse           int           `json:"in_use"`
	Idle            int           `json:"idle"`
	WaitCount       int64         `json:"wait_count"`
	WaitDuration    time.Duration `json:"wait_duration"`
}

type ContainerMetrics struct {
	Available         bool     `json:"available"`
	UnavailableReason string   `json:"unavailable_reason,omitempty"`
	Samples           int64    `json:"samples,omitempty"`
	CPUMeanPercent    *float64 `json:"cpu_mean_percent,omitempty"`
	CPUMaxPercent     *float64 `json:"cpu_max_percent,omitempty"`
	MemoryMeanBytes   *uint64  `json:"memory_mean_bytes,omitempty"`
	MemoryMaxBytes    *uint64  `json:"memory_max_bytes,omitempty"`
}

type DatabaseMetrics struct {
	Available           bool         `json:"available"`
	UnavailableReason   string       `json:"unavailable_reason,omitempty"`
	Samples             int64        `json:"samples"`
	Connections         GaugeSummary `json:"connections"`
	ActiveConnections   GaugeSummary `json:"active_connections"`
	IdleInTransaction   GaugeSummary `json:"idle_in_transaction"`
	LockWaitingSessions GaugeSummary `json:"lock_waiting_sessions"`
	UngrantedLocks      GaugeSummary `json:"ungranted_locks"`
	DeadlocksBefore     int64        `json:"deadlocks_before"`
	DeadlocksAfter      int64        `json:"deadlocks_after"`
	DatabaseBytesBefore int64        `json:"database_bytes_before"`
	DatabaseBytesAfter  int64        `json:"database_bytes_after"`
	// SessionsOpened counts new PostgreSQL connections during the run (includes the
	// runner's own few). A large number means the services are closing and reopening
	// connections, for example when MaxIdleConns is below MaxOpenConns.
	SessionsOpened    *int64            `json:"sessions_opened,omitempty"`
	PostgresContainer ContainerMetrics  `json:"postgres_container"`
	PGStatStatements  OptionalTelemetry `json:"pg_stat_statements"`
}

type GaugeSummary struct {
	Mean float64 `json:"mean"`
	Max  int64   `json:"max"`
}

type OptionalTelemetry struct {
	Available         bool           `json:"available"`
	UnavailableReason string         `json:"unavailable_reason,omitempty"`
	Values            map[string]any `json:"values,omitempty"`
}
