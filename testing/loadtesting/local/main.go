package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// main runs the load test and exits non-zero if it fails.
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "load test failed:", err)
		os.Exit(1)
	}
}

// run loads the configuration and executes each run of the chosen case, or only re-renders the report when asked.
func run() error {
	if len(os.Args) > 1 && os.Args[1] == "render" {
		return renderOnly(env("RESULTS_DIR", "test-results/load-test"))
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	expectedMode := "success"
	if cfg.CaseName == "C11" {
		expectedMode = "decline"
	}
	if cfg.CaseName == "C12" {
		expectedMode = "random"
	}
	if cfg.GatewayMode != expectedMode {
		return fmt.Errorf("%s requires GATEWAY_MODE=%s, got %q", cfg.CaseName, expectedMode, cfg.GatewayMode)
	}
	if err := healthy(cfg.OrchestratorURL + "/health"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := healthy(cfg.OrderURL + "/health"); err != nil {
		return err
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect PostgreSQL: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping PostgreSQL: %w", err)
	}

	// A run that fails its completion or invariant checks is recorded in the report and
	// the remaining runs still execute: a sweep that stops at the first overloaded step
	// would hide what happens beyond the limit. Infrastructure errors still abort.
	var failedRuns []string
	for runNumber := 1; runNumber <= cfg.Runs; runNumber++ {
		experiment := cfg.experiment(runNumber)
		result, err := executeRun(ctx, pool, experiment, runNumber)
		if err != nil {
			return err
		}
		if err := writeAggregate(cfg.ResultsDir, result); err != nil {
			return err
		}
		fmt.Printf("%s: offered=%d accepted=%d completed=%d accepted/s=%.2f completed/s=%.2f e2e-p95=%.2fms\n",
			result.Label, result.Client.Offered, result.Client.Accepted, result.Client.Completed,
			result.Client.AcceptedRate, result.Client.CompletedRate, result.Client.EndToEndLatency.P95MS)
		if !runPassed(result, experiment) {
			failedRuns = append(failedRuns, result.Label)
		}
	}
	if len(failedRuns) > 0 {
		return fmt.Errorf("%d run(s) failed their completion or invariant checks (%s); see the report", len(failedRuns), strings.Join(failedRuns, ", "))
	}
	return nil
}

// executeRun resets the data, drives the load, collects metrics, checks invariants and returns the report.
func executeRun(ctx context.Context, pool *pgxpool.Pool, cfg config, runNumber int) (Report, error) {
	products, err := resetFixture(ctx, pool)
	if err != nil {
		return Report{}, err
	}
	hardware, err := readHardware()
	if err != nil {
		return Report{}, err
	}
	transport := &http.Transport{MaxIdleConns: cfg.MaxConns, MaxIdleConnsPerHost: cfg.MaxConns, MaxConnsPerHost: cfg.MaxConns}
	if cfg.Warmup > 0 {
		warmupClient := &loadClient{checkoutURL: cfg.OrchestratorURL, http: &http.Client{Transport: transport}, metrics: newCollector(), behavior: cfg.Behavior, workflowTimeout: cfg.WorkflowTimeout, statusPollEvery: cfg.StatusPollEvery}
		warmupSelector := newCartSelector(cfg.Workload, products)
		if cfg.CaseName == "C4" {
			runOpenLoop(ctx, warmupClient, warmupSelector, cfg.OfferedRate, cfg.Warmup)
		} else {
			runClosedLoop(ctx, warmupClient, warmupSelector, cfg.Clients, cfg.Warmup)
		}
		products, err = resetFixture(ctx, pool)
		if err != nil {
			return Report{}, fmt.Errorf("reset fixture after warm-up: %w", err)
		}
	}
	if err := configureWorkloadFixture(ctx, pool, cfg, products); err != nil {
		return Report{}, err
	}
	if err := resetServiceMetrics(ctx, cfg); err != nil {
		return Report{}, err
	}
	sampler, err := newDatabaseSampler(ctx, pool, cfg.SampleEvery)
	if err != nil {
		return Report{}, err
	}
	sampleCtx, cancelSamples := context.WithCancel(ctx)
	var sampleWG sync.WaitGroup
	containerSampler := newContainerSampler(cfg.SampleEvery)
	sampleWG.Add(2)
	go func() {
		defer sampleWG.Done()
		sampler.run(sampleCtx)
	}()
	go func() {
		defer sampleWG.Done()
		containerSampler.run(sampleCtx)
	}()

	cpuBefore, cpuBeforeOK := readCgroupCPUUsec()
	started := time.Now().UTC()
	metrics := newCollector()
	client := &loadClient{checkoutURL: cfg.OrchestratorURL, http: &http.Client{Transport: transport}, metrics: metrics, behavior: cfg.Behavior, workflowTimeout: cfg.WorkflowTimeout, statusPollEvery: cfg.StatusPollEvery}
	selector := newCartSelector(cfg.Workload, products)

	measurement := cfg.Duration
	switch cfg.CaseName {
	case "C1":
		for i := 0; i < cfg.ExactOrders; i++ {
			client.workflowCart(ctx, selector.next())
		}
		measurement = time.Since(started)
	case "C2", "C3", "C6", "C7", "C9", "C10", "C11", "C12", "C13", "C14":
		runClosedLoop(ctx, client, selector, cfg.Clients, cfg.Duration)
	case "C4":
		runOpenLoop(ctx, client, selector, cfg.OfferedRate, cfg.Duration)
	case "C5":
		if cfg.ExactOrders > 0 {
			runFixedWorkflows(ctx, client, selector, cfg.Clients, cfg.ExactOrders)
			measurement = time.Since(started)
		} else {
			runClosedLoop(ctx, client, selector, cfg.Clients, cfg.Duration)
		}
	}
	finished := time.Now().UTC()
	cancelSamples()
	sampleWG.Wait()
	containerMetrics := containerSampler.finish()
	for _, name := range requiredContainers {
		if !containerMetrics[name].Available {
			return Report{}, fmt.Errorf("%s container telemetry unavailable: %s", name, containerMetrics[name].UnavailableReason)
		}
	}

	database, err := sampler.finish(ctx)
	if err != nil {
		return Report{}, err
	}
	postgresContainer := containerMetrics["postgres"]
	database.PostgresContainer = postgresContainer
	services, err := fetchServiceMetrics(ctx, cfg, containerMetrics)
	if err != nil {
		return Report{}, err
	}
	// Keep Docker CPU/memory alongside the orchestrator's HTTP and DB-pool metrics.
	orchestratorContainer := containerMetrics["orchestrator"]
	orchestratorMetrics := services["orchestrator"]
	orchestratorMetrics.Container = &orchestratorContainer
	services["orchestrator"] = orchestratorMetrics
	clientMetrics := metrics.snapshot(measurement)
	if cpuAfter, ok := readCgroupCPUUsec(); ok && cpuBeforeOK && cpuAfter >= cpuBefore && finished.After(started) {
		percent := float64(cpuAfter-cpuBefore) / 1e6 / finished.Sub(started).Seconds() * 100
		clientMetrics.RunnerCPUPercent = &percent
	}
	if memory, ok := readCgroupMemoryBytes(); ok {
		clientMetrics.RunnerMemoryBytes = &memory
	}
	strictWorkflowCounts := cfg.CaseName != "C4" && !(cfg.CaseName == "C5" && cfg.ExactOrders > 0)
	invariants, err := checkInvariants(ctx, pool, clientMetrics, strictWorkflowCounts)
	if err != nil {
		return Report{}, err
	}
	workloadInvariants, err := checkWorkloadInvariants(ctx, pool, cfg, products, clientMetrics)
	if err != nil {
		return Report{}, err
	}
	invariants = append(invariants, workloadInvariants...)
	label := "c1-smoke"
	if cfg.CaseName == "C2" {
		label = fmt.Sprintf("c2-baseline-%d", runNumber)
	} else if cfg.CaseName == "C3" {
		label = fmt.Sprintf("c3-clients-%02d", cfg.Clients)
	} else if cfg.CaseName == "C4" {
		label = fmt.Sprintf("c4-rate-%03.0f", cfg.OfferedRate)
	} else if cfg.CaseName == "C5" {
		if cfg.ExactOrders > 0 {
			label = "c5b-hot-product-sellout"
		} else {
			label = "c5a-hot-product-throughput"
		}
	} else if cfg.CaseName == "C6" {
		label = "c6-skewed-80-20"
	} else if cfg.CaseName == "C7" {
		label = "c7-multi-item-opposite-order"
	} else if cfg.CaseName == "C9" {
		label = "c9-duplicate-checkout"
	} else if cfg.CaseName == "C10" {
		label = "c10-concurrent-pay"
	} else if cfg.CaseName == "C11" {
		label = "c11-decline-release"
	} else if cfg.CaseName == "C12" {
		label = "c12-random-outcomes"
	} else if cfg.CaseName == "C13" {
		label = fmt.Sprintf("c13-gateway-%04dms", cfg.GatewayLatency.Milliseconds())
	} else if cfg.CaseName == "C14" {
		label = fmt.Sprintf("c14-db-pool-%02d-idle%02d-gw%04dms", cfg.DBPoolSize, cfg.DBIdleSize, cfg.GatewayLatency.Milliseconds())
	}
	diagnostics := []string{
		"Every run resets orders, attempts, reservations, payments, and inventory counters before measurement.",
		fmt.Sprintf("Cart selection uses the %s workload distribution across the 50 seeded products.", cfg.Workload),
		"Warm-up traffic is excluded and its rows are removed before measurement.",
	}
	return Report{
		Suite:      "local-load-testing",
		RunID:      fmt.Sprintf("%s-%s", label, started.Format("20060102T150405.000000000Z")),
		Label:      label,
		StartedAt:  started,
		FinishedAt: finished,
		Config: RunConfig{
			Case:                 cfg.CaseName,
			Clients:              cfg.Clients,
			ExactOrders:          cfg.ExactOrders,
			ProductCount:         len(products),
			Workload:             cfg.Workload,
			HotStock:             cfg.HotStock,
			Warmup:               cfg.Warmup,
			Measurement:          measurement,
			DrainTimeout:         cfg.DrainTimeout,
			StatusPollInterval:   cfg.StatusPollEvery,
			SampleInterval:       cfg.SampleEvery,
			MaxInFlight:          cfg.Clients,
			OfferedRatePerSecond: cfg.OfferedRate,
			GatewayMode:          cfg.GatewayMode,
			GatewayLatency:       cfg.GatewayLatency,
			Behavior:             cfg.Behavior,
			DBPoolSize:           cfg.DBPoolSize,
			DBIdleSize:           cfg.DBIdleSize,
			Environment:          cfg.Environment,
			MaxConnections:       cfg.MaxConns,
			Hardware:             hardware,
		},
		Client:      clientMetrics,
		Services:    services,
		Database:    database,
		Diagnostics: diagnostics,
		Invariants:  invariants,
	}, nil
}

// runClosedLoop keeps a fixed number of clients busy for the duration, each starting a workflow as soon as its last one ends.
func runClosedLoop(ctx context.Context, client *loadClient, selector *cartSelector, clients int, duration time.Duration) {
	stopAt := time.Now().Add(duration)
	var workers sync.WaitGroup
	workers.Add(clients)
	for i := 0; i < clients; i++ {
		go func() {
			defer workers.Done()
			for time.Now().Before(stopAt) {
				client.workflowCart(ctx, selector.next())
			}
		}()
	}
	workers.Wait()
}

// runOpenLoop starts workflows at a fixed rate for the duration, whether or not earlier ones have finished.
func runOpenLoop(ctx context.Context, client *loadClient, selector *cartSelector, rate float64, duration time.Duration) {
	interval := time.Duration(float64(time.Second) / rate)
	started := time.Now()
	stopAt := started.Add(duration)
	next := started
	var workflows sync.WaitGroup
	for next.Before(stopAt) {
		if wait := time.Until(next); wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				workflows.Wait()
				return
			case <-timer.C:
			}
		}
		cart := selector.next()
		workflows.Add(1)
		go func() {
			defer workflows.Done()
			client.workflowCart(ctx, cart)
		}()
		next = next.Add(interval)
	}
	workflows.Wait()
}

// runFixedWorkflows runs an exact number of workflows spread across the clients.
func runFixedWorkflows(ctx context.Context, client *loadClient, selector *cartSelector, clients, total int) {
	var workers sync.WaitGroup
	var next atomic.Int64
	workers.Add(clients)
	for i := 0; i < clients; i++ {
		go func() {
			defer workers.Done()
			for {
				index := next.Add(1)
				if index > int64(total) {
					return
				}
				client.workflowCart(ctx, selector.next())
			}
		}()
	}
	workers.Wait()
}

// healthy returns an error if the service does not answer its health check.
func healthy(url string) error {
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("order service health check: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("order service health check returned HTTP %d", response.StatusCode)
	}
	return nil
}

// runPassed reports whether a run met its invariants and the checks specific to its case.
func runPassed(report Report, cfg config) bool {
	for _, invariant := range report.Invariants {
		if !invariant.Passed {
			return false
		}
	}
	if report.Database.DeadlocksAfter != report.Database.DeadlocksBefore {
		return false
	}
	if cfg.CaseName == "C5" && cfg.ExactOrders > 0 {
		expectedAccepted := cfg.HotStock
		expectedConflicts := int64(cfg.ExactOrders) - cfg.HotStock
		return report.Client.Offered == int64(cfg.ExactOrders) && report.Client.Accepted == expectedAccepted &&
			report.Client.Completed == expectedAccepted && report.Client.Paid == expectedAccepted &&
			report.Client.CheckoutStatusMix.Business409 == expectedConflicts &&
			report.Client.CheckoutStatusMix.Server5xx == 0 && report.Client.CheckoutStatusMix.TransportErrors == 0 &&
			report.Client.CheckoutStatusMix.DecodeErrors == 0 && report.Client.PayStatusMix.Server5xx == 0 &&
			report.Client.PayStatusMix.TransportErrors == 0 && report.Client.PayStatusMix.DecodeErrors == 0 &&
			report.Client.PayStatusMix.Business409 == 0
	}
	if cfg.CaseName == "C4" {
		return true
	}
	if cfg.CaseName == "C12" {
		return report.Client.Accepted == report.Client.Offered &&
			report.Client.Completed+report.Client.PendingAtDrain == report.Client.Offered &&
			report.Client.CheckoutStatusMix.Server5xx == 0 && report.Client.PayStatusMix.Server5xx == 0
	}
	if report.Client.Accepted != report.Client.Offered || report.Client.Completed != report.Client.Offered {
		return false
	}
	if cfg.CaseName == "C1" && report.Client.Offered != int64(cfg.ExactOrders) {
		return false
	}
	// A duplicate received while the saga is still running is an idempotent 202;
	// a later terminal replay is 200. Identity, not the timing-dependent 200/202
	// split, is the correctness condition.
	if cfg.CaseName == "C9" && (report.Client.Duplicate.InjectedWorkflows == 0 || report.Client.Duplicate.Mismatches != 0) {
		return false
	}
	if cfg.CaseName == "C10" && (report.Client.Duplicate.InjectedWorkflows != report.Client.Offered || report.Client.Duplicate.Mismatches != 0) {
		return false
	}
	if cfg.CaseName == "C11" && (report.Client.Cancelled != report.Client.Offered || report.Client.Paid != 0) {
		return false
	}
	return report.Client.CheckoutStatusMix.Server5xx == 0 && report.Client.CheckoutStatusMix.TransportErrors == 0 &&
		report.Client.CheckoutStatusMix.DecodeErrors == 0 && report.Client.CheckoutStatusMix.Business409 == 0 &&
		report.Client.PayStatusMix.Server5xx == 0 && report.Client.PayStatusMix.TransportErrors == 0 &&
		report.Client.PayStatusMix.DecodeErrors == 0 && report.Client.PayStatusMix.Business409 == 0
}
