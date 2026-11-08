package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// writeAggregate adds a run to the saved results and rewrites the JSON and Markdown files.
func writeAggregate(resultsDir string, report Report) error {
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		return fmt.Errorf("create results directory: %w", err)
	}
	jsonPath := filepath.Join(resultsDir, "load-test.json")
	collection := ReportCollection{Suite: "local-load-testing"}
	if raw, err := os.ReadFile(jsonPath); err == nil {
		if err := json.Unmarshal(raw, &collection); err != nil {
			return fmt.Errorf("read existing load-test.json: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read existing load-test.json: %w", err)
	}

	replaced := false
	for i := range collection.Runs {
		if collection.Runs[i].Label == report.Label {
			report.PreviousRuns = append(collection.Runs[i].PreviousRuns, summarizeRun(collection.Runs[i]))
			collection.Runs[i] = report
			replaced = true
			break
		}
	}
	if !replaced {
		collection.Runs = append(collection.Runs, report)
	}
	sort.Slice(collection.Runs, func(i, j int) bool { return collection.Runs[i].Label < collection.Runs[j].Label })
	collection.Suite = "local-load-testing"
	collection.UpdatedAt = time.Now().UTC()
	collection.BaselineVariance = calculateBaselineVariance(collection.Runs)
	collection.C3Sweep, collection.C3KneeClients = calculateClientSweep(collection.Runs)
	collection.C4Sweep, collection.C4SaturationRate = calculateRateSweep(collection.Runs)
	collection.ContentionWave = calculateContentionWave(collection.Runs)
	collection.BehaviourWave = calculateScenarioPoints(collection.Runs, "C9", "C10", "C11", "C12")
	collection.C13LatencySweep = calculateScenarioPoints(collection.Runs, "C13")
	sort.Slice(collection.C13LatencySweep, func(i, j int) bool {
		a, b := collection.C13LatencySweep[i], collection.C13LatencySweep[j]
		if a.GatewayLatencyMS != b.GatewayLatencyMS {
			return a.GatewayLatencyMS < b.GatewayLatencyMS
		}
		return a.Clients < b.Clients
	})
	collection.C14PoolSweep = calculateScenarioPoints(collection.Runs, "C14")
	sort.Slice(collection.C14PoolSweep, func(i, j int) bool {
		a, b := collection.C14PoolSweep[i], collection.C14PoolSweep[j]
		if a.GatewayLatencyMS != b.GatewayLatencyMS {
			return a.GatewayLatencyMS < b.GatewayLatencyMS
		}
		if a.DBPoolSize != b.DBPoolSize {
			return a.DBPoolSize < b.DBPoolSize
		}
		if a.DBIdleSize != b.DBIdleSize {
			return a.DBIdleSize < b.DBIdleSize
		}
		return a.Clients < b.Clients
	})

	encoded, err := json.MarshalIndent(collection, "", "  ")
	if err != nil {
		return fmt.Errorf("encode JSON report: %w", err)
	}
	if err := atomicWrite(jsonPath, append(encoded, '\n')); err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(resultsDir, "load-test.md"), []byte(renderMarkdown(collection))); err != nil {
		return err
	}
	return nil
}

// atomicWrite writes a file through a temporary file so a reader never sees a half-written one.
func atomicWrite(path string, data []byte) error {
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("replace %s: %w", filepath.Base(path), err)
	}
	return nil
}

// calculateBaselineVariance measures how much the repeated baseline runs differ from each other.
func calculateBaselineVariance(reports []Report) *BaselineVariance {
	accepted, completed, checkoutP95, payP95, endToEndP95 := []float64{}, []float64{}, []float64{}, []float64{}, []float64{}
	for _, report := range reports {
		if !strings.HasPrefix(report.Label, "c2-baseline-") {
			continue
		}
		accepted = append(accepted, report.Client.AcceptedRate)
		completed = append(completed, report.Client.CompletedRate)
		checkoutP95 = append(checkoutP95, report.Client.CheckoutLatency.P95MS)
		payP95 = append(payP95, report.Client.PayLatency.P95MS)
		endToEndP95 = append(endToEndP95, report.Client.EndToEndLatency.P95MS)
	}
	if len(accepted) == 0 {
		return nil
	}
	return &BaselineVariance{
		Runs:          len(accepted),
		AcceptedRate:  summarizeVariance(accepted),
		CompletedRate: summarizeVariance(completed),
		CheckoutP95MS: summarizeVariance(checkoutP95),
		PayP95MS:      summarizeVariance(payP95),
		EndToEndP95MS: summarizeVariance(endToEndP95),
	}
}

// summarizeVariance returns the mean, minimum, maximum, standard deviation and variation of the values.
func summarizeVariance(values []float64) VarianceSummary {
	result := VarianceSummary{Min: values[0], Max: values[0]}
	for _, value := range values {
		result.Mean += value
		if value < result.Min {
			result.Min = value
		}
		if value > result.Max {
			result.Max = value
		}
	}
	result.Mean /= float64(len(values))
	for _, value := range values {
		difference := value - result.Mean
		result.StdDev += difference * difference
	}
	result.StdDev = math.Sqrt(result.StdDev / float64(len(values)))
	if result.Mean != 0 {
		result.CoefficientPct = result.StdDev / result.Mean * 100
	}
	return result
}

// renderMarkdown turns all saved runs into the Markdown results file.
func renderMarkdown(collection ReportCollection) string {
	var out strings.Builder
	out.WriteString("# Local load-test results\n\n")
	out.WriteString("C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.\n\n")
	fmt.Fprintf(&out, "Updated: `%s`\n\n", collection.UpdatedAt.Format(time.RFC3339))
	if collection.BaselineVariance != nil {
		v := collection.BaselineVariance
		fmt.Fprintf(&out, "## C2 run-to-run variance (%d runs recorded)\n\n", v.Runs)
		out.WriteString("| Metric | Mean | Min | Max | Std dev | CV |\n|---|---:|---:|---:|---:|---:|\n")
		writeVarianceRow(&out, "Accepted/s", v.AcceptedRate)
		writeVarianceRow(&out, "Completed/s", v.CompletedRate)
		writeVarianceRow(&out, "Checkout p95 ms", v.CheckoutP95MS)
		writeVarianceRow(&out, "Pay p95 ms", v.PayP95MS)
		writeVarianceRow(&out, "End-to-end p95 ms", v.EndToEndP95MS)
		out.WriteString("\n")
	}
	renderSweepSummary(&out, "C3 client sweep", collection.C3Sweep, collection.C3KneeClients, nil)
	renderSweepSummary(&out, "C4 rate sweep", collection.C4Sweep, nil, collection.C4SaturationRate)
	renderContentionSummary(&out, collection.ContentionWave)
	renderBehaviourSummary(&out, collection.BehaviourWave)
	renderGatewayLatencySummary(&out, collection.C13LatencySweep)
	renderPoolSizeSummary(&out, collection.C14PoolSweep)
	renderRepeatHistory(&out, collection.Runs)
	for _, report := range collection.Runs {
		renderRun(&out, report)
	}
	return out.String()
}

// calculateContentionWave collects the points for the hot-row, skewed and multi-item contention cases.
func calculateContentionWave(reports []Report) []ContentionPoint {
	points := make([]ContentionPoint, 0)
	for _, report := range reports {
		if report.Config.Case != "C5" && report.Config.Case != "C6" && report.Config.Case != "C7" {
			continue
		}
		orderWaitCount, orderWaitMS := int64(0), 0.0
		if service := report.Services["order"]; service.DBPool != nil {
			orderWaitCount = service.DBPool.WaitCount
			orderWaitMS = float64(service.DBPool.WaitDuration.Microseconds()) / 1000
		}
		points = append(points, ContentionPoint{
			Label: report.Label, Offered: report.Client.Offered, CompletedRate: report.Client.CompletedRate,
			Checkout409: report.Client.CheckoutStatusMix.Business409, CheckoutP99MS: report.Client.CheckoutLatency.P99MS,
			EndToEndP99MS: report.Client.EndToEndLatency.P99MS, LockWaitersMax: report.Database.LockWaitingSessions.Max,
			DeadlocksDelta:   report.Database.DeadlocksAfter - report.Database.DeadlocksBefore,
			OrderDBWaitCount: orderWaitCount, OrderDBWaitMS: orderWaitMS,
		})
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Label < points[j].Label })
	return points
}

// renderContentionSummary writes the contention table.
func renderContentionSummary(out *strings.Builder, points []ContentionPoint) {
	if len(points) == 0 {
		return
	}
	out.WriteString("## C5–C7 contention wave\n\n")
	out.WriteString("| Case | Offered | Completed/s | Checkout 409 | Checkout p99 ms | End-to-end p99 ms | Lock waiters max | Deadlocks | Order DB waits / duration ms |\n")
	out.WriteString("|---|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, point := range points {
		fmt.Fprintf(out, "| %s | %d | %.2f | %d | %.2f | %.2f | %d | %d | %d / %.2f |\n",
			point.Label, point.Offered, point.CompletedRate, point.Checkout409, point.CheckoutP99MS,
			point.EndToEndP99MS, point.LockWaitersMax, point.DeadlocksDelta, point.OrderDBWaitCount, point.OrderDBWaitMS)
	}
	out.WriteString("\n")
}

// calculateClientSweep collects the client-count sweep and finds the point where more clients stop helping.
func calculateClientSweep(reports []Report) ([]SweepPoint, *int) {
	points := sweepPoints(reports, "c3-")
	sort.Slice(points, func(i, j int) bool { return points[i].Clients < points[j].Clients })
	var knee *int
	for i := 1; i < len(points); i++ {
		if points[i-1].CompletedRate > 0 && (points[i].CompletedRate-points[i-1].CompletedRate)/points[i-1].CompletedRate < .10 {
			value := points[i-1].Clients
			knee = &value
			break
		}
	}
	return points, knee
}

// calculateRateSweep collects the offered-rate sweep and finds the rate where the system stops keeping up.
func calculateRateSweep(reports []Report) ([]SweepPoint, *float64) {
	points := sweepPoints(reports, "c4-")
	sort.Slice(points, func(i, j int) bool { return points[i].OfferedRate < points[j].OfferedRate })
	var saturation *float64
	for _, point := range points {
		if point.CompletedRate < point.OfferedRate*.95 || point.Server5xx > 0 || point.TransportErrors > 0 {
			value := point.OfferedRate
			saturation = &value
			break
		}
	}
	return points, saturation
}

// sweepPoints collects the measured points of the runs whose labels start with prefix.
func sweepPoints(reports []Report, prefix string) []SweepPoint {
	points := make([]SweepPoint, 0)
	for _, report := range reports {
		if !strings.HasPrefix(report.Label, prefix) {
			continue
		}
		points = append(points, SweepPoint{
			Label: report.Label, Clients: report.Config.Clients, OfferedRate: report.Client.OfferedRate,
			AcceptedRate: report.Client.AcceptedRate, CompletedRate: report.Client.CompletedRate,
			EndToEndP99MS: report.Client.EndToEndLatency.P99MS, MaxInFlight: report.Client.InFlight.MaxWorkflows,
			Server5xx:       report.Client.CheckoutStatusMix.Server5xx + report.Client.PayStatusMix.Server5xx,
			TransportErrors: report.Client.CheckoutStatusMix.TransportErrors + report.Client.PayStatusMix.TransportErrors,
		})
	}
	return points
}

// renderSweepSummary writes a sweep table with its knee or saturation point.
func renderSweepSummary(out *strings.Builder, title string, points []SweepPoint, knee *int, saturation *float64) {
	if len(points) == 0 {
		return
	}
	fmt.Fprintf(out, "## %s\n\n", title)
	if strings.HasPrefix(title, "C4") {
		out.WriteString("| Offered/s | Accepted/s | Completed/s | End-to-end p99 ms | Max in-flight | 5xx | Transport errors |\n")
		out.WriteString("|---:|---:|---:|---:|---:|---:|---:|\n")
		for _, point := range points {
			fmt.Fprintf(out, "| %.2f | %.2f | %.2f | %.2f | %d | %d | %d |\n", point.OfferedRate, point.AcceptedRate, point.CompletedRate, point.EndToEndP99MS, point.MaxInFlight, point.Server5xx, point.TransportErrors)
		}
	} else {
		out.WriteString("| Clients | Offered/s | Accepted/s | Completed/s | End-to-end p99 ms | Max in-flight | 5xx | Transport errors |\n")
		out.WriteString("|---:|---:|---:|---:|---:|---:|---:|---:|\n")
		for _, point := range points {
			fmt.Fprintf(out, "| %d | %.2f | %.2f | %.2f | %.2f | %d | %d | %d |\n", point.Clients, point.OfferedRate, point.AcceptedRate, point.CompletedRate, point.EndToEndP99MS, point.MaxInFlight, point.Server5xx, point.TransportErrors)
		}
	}
	if knee != nil {
		fmt.Fprintf(out, "\nAutomatic knee candidate: **%d clients** (first following step with less than 10%% additional completed throughput).\n\n", *knee)
	} else if saturation != nil {
		fmt.Fprintf(out, "\nAutomatic saturation candidate: **%.0f offered workflows/s** (completed rate below 95%% of offered rate, or errors appeared).\n\n", *saturation)
	} else {
		out.WriteString("\nNo knee/saturation candidate was detected in the recorded steps.\n\n")
	}
}

// writeVarianceRow writes one row of the baseline variance table.
func writeVarianceRow(out *strings.Builder, label string, value VarianceSummary) {
	fmt.Fprintf(out, "| %s | %.2f | %.2f | %.2f | %.2f | %.2f%% |\n", label, value.Mean, value.Min, value.Max, value.StdDev, value.CoefficientPct)
}

// renderRun writes the full section for one run.
func renderRun(out *strings.Builder, report Report) {
	client := report.Client
	fmt.Fprintf(out, "## %s\n\n", report.Label)
	fmt.Fprintf(out, "**What it does:** %s\n\n", runDescription(report.Config))
	fmt.Fprintf(out, "**Result:** %s — %d offered, %d accepted, %d completed (%d paid, %d cancelled, %d pending at drain).\n\n", passLabel(report), client.Offered, client.Accepted, client.Completed, client.Paid, client.Cancelled, client.PendingAtDrain)
	fmt.Fprintf(out, "**Environment:** %d logical CPUs, %d bytes memory (the host running the runner); gateway `%s` with %s simulator latency; warm-up %s; measurement %s.\n\n",
		report.Config.Hardware.LogicalCPUs, report.Config.Hardware.MemoryBytes, report.Config.GatewayMode,
		report.Config.GatewayLatency, report.Config.Warmup, report.Config.Measurement)
	if report.Config.Environment != "" {
		fmt.Fprintf(out, "**Deployment:** %s\n\n", report.Config.Environment)
	}
	fmt.Fprintf(out, "**Completion observation:** `%s`", report.Config.CompletionMode)
	if report.Config.CompletionMode == "poll" {
		fmt.Fprintf(out, " every %s", report.Config.StatusPollInterval)
	}
	out.WriteString(".\n\n")
	out.WriteString("### Client metrics\n\n")
	out.WriteString("| Metric | Value |\n|---|---:|\n")
	fmt.Fprintf(out, "| Offered rate | %.2f/s |\n| Accepted rate | %.2f/s |\n| Completed rate | %.2f/s |\n", client.OfferedRate, client.AcceptedRate, client.CompletedRate)
	fmt.Fprintf(out, "| Mean/max in-flight requests | %.2f / %d |\n| Mean/max in-flight workflows | %.2f / %d |\n", client.InFlight.MeanRequests, client.InFlight.MaxRequests, client.InFlight.MeanWorkflows, client.InFlight.MaxWorkflows)
	fmt.Fprintf(out, "| Little's law observed / expected | %.2f / %.2f (%.2f%% difference) |\n", client.LittleLaw.ObservedMeanInFlight, client.LittleLaw.ExpectedMeanInFlight, client.LittleLaw.DifferencePercent)
	if client.RunnerCPUPercent != nil {
		fmt.Fprintf(out, "| Load generator CPU (percent of one core, measured window) | %.1f%% |\n", *client.RunnerCPUPercent)
	}
	if client.RunnerMemoryBytes != nil {
		fmt.Fprintf(out, "| Load generator memory at end | %.1f MB |\n", float64(*client.RunnerMemoryBytes)/1e6)
	}
	if report.Config.CompletionMode == "sse" {
		observation := client.CompletionObservation
		fmt.Fprintf(out, "| SSE attempts / terminal events / polling fallbacks | %d / %d / %d |\n", observation.SSEAttempts, observation.SSETerminalEvents, observation.PollingFallbacks)
	}
	out.WriteString("\n")
	out.WriteString("| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |\n|---|---:|---:|---:|---:|---:|\n")
	writeLatencyRow(out, "Checkout", client.CheckoutLatency)
	writeLatencyRow(out, "Pay", client.PayLatency)
	writeLatencyRow(out, "End to end", client.EndToEndLatency)
	writeLatencyRow(out, "Server saga completion", client.ServerCompletionLatency)
	out.WriteString("\n")
	fmt.Fprintf(out, "Checkout status mix: `%s`  \nPay status mix: `%s`\n\n", statusText(client.CheckoutStatusMix), statusText(client.PayStatusMix))
	if client.Duplicate.InjectedWorkflows > 0 {
		fmt.Fprintf(out, "Duplicate injection: %d workflows, %d extra requests, %d HTTP 200 replays, %d mismatches.\n\n", client.Duplicate.InjectedWorkflows, client.Duplicate.ExtraRequests, client.Duplicate.Replay200, client.Duplicate.Mismatches)
	}

	out.WriteString("### Service metrics\n\n")
	out.WriteString("| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |\n")
	out.WriteString("|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|\n")
	serviceNames := []string{"orchestrator", "order", "inventory", "payment"}
	for _, name := range serviceNames {
		service := report.Services[name]
		if !service.Available || service.Inbound == nil || service.Runtime == nil || service.DBPool == nil || service.Container == nil {
			fmt.Fprintf(out, "| %s | unavailable | | | | | | | | | | |\n", name)
			continue
		}
		container := service.Container
		cpu, memory := "unavailable", "unavailable"
		if container.Available && container.CPUMeanPercent != nil && container.CPUMaxPercent != nil && container.MemoryMeanBytes != nil && container.MemoryMaxBytes != nil {
			cpu = fmt.Sprintf("%.2f%% / %.2f%%", *container.CPUMeanPercent, *container.CPUMaxPercent)
			memory = fmt.Sprintf("%d / %d", *container.MemoryMeanBytes, *container.MemoryMaxBytes)
		}
		fmt.Fprintf(out, "| %s | %d | %d | %d | %.2f | %d | %d | %d/%d/%d | %d | %s | %s | %s |\n",
			name, service.Inbound.Requests, service.Inbound.Errors, service.Inbound.MaxInFlight, service.Inbound.Latency.P95MS,
			service.Runtime.Goroutines, service.Runtime.HeapAllocBytes, service.DBPool.OpenConnections, service.DBPool.InUse,
			service.DBPool.Idle, service.DBPool.WaitCount, service.DBPool.WaitDuration, cpu, memory)
	}
	out.WriteString("\n")

	db := report.Database
	out.WriteString("### PostgreSQL metrics\n\n")
	fmt.Fprintf(out, "Samples: %d. Connections mean/max: %.2f/%d. Active mean/max: %.2f/%d. Idle-in-transaction max: %d. Lock-waiting sessions max: %d. Ungranted locks max: %d. Deadlocks before/after: %d/%d. Database bytes before/after: %d/%d. New PostgreSQL sessions opened during the run: %s.\n\n",
		db.Samples, db.Connections.Mean, db.Connections.Max, db.ActiveConnections.Mean, db.ActiveConnections.Max,
		db.IdleInTransaction.Max, db.LockWaitingSessions.Max, db.UngrantedLocks.Max, db.DeadlocksBefore, db.DeadlocksAfter, db.DatabaseBytesBefore, db.DatabaseBytesAfter, sessionsText(db.SessionsOpened))
	if !db.PGStatStatements.Available {
		fmt.Fprintf(out, "`pg_stat_statements`: unavailable — %s.\n\n", db.PGStatStatements.UnavailableReason)
	}
	if db.PostgresContainer.Available && db.PostgresContainer.CPUMeanPercent != nil && db.PostgresContainer.CPUMaxPercent != nil && db.PostgresContainer.MemoryMeanBytes != nil && db.PostgresContainer.MemoryMaxBytes != nil {
		fmt.Fprintf(out, "PostgreSQL container (%d samples): CPU mean/max %.2f%%/%.2f%%; working-set memory mean/max %d/%d bytes.\n\n",
			db.PostgresContainer.Samples, *db.PostgresContainer.CPUMeanPercent, *db.PostgresContainer.CPUMaxPercent,
			*db.PostgresContainer.MemoryMeanBytes, *db.PostgresContainer.MemoryMaxBytes)
	}

	out.WriteString("### Invariants\n\n")
	for _, invariant := range report.Invariants {
		state := "PASS"
		if !invariant.Passed {
			state = "FAIL"
		}
		fmt.Fprintf(out, "- %s — %s (violations: %d)\n", state, invariant.Name, invariant.Violations)
	}
	limitations := make([]string, 0)
	for _, name := range serviceNames {
		service := report.Services[name]
		if !service.Available {
			limitations = append(limitations, fmt.Sprintf("%s service: %s", name, service.UnavailableReason))
		} else if service.Container != nil && !service.Container.Available {
			limitations = append(limitations, fmt.Sprintf("%s container: %s", name, service.Container.UnavailableReason))
		}
	}
	if !db.PostgresContainer.Available {
		limitations = append(limitations, "PostgreSQL container: "+db.PostgresContainer.UnavailableReason)
	}
	if len(limitations) > 0 {
		out.WriteString("\n### Telemetry limitations\n\n")
		for _, limitation := range limitations {
			fmt.Fprintf(out, "- %s.\n", limitation)
		}
	}
	out.WriteString("\n")
}

// runDescription describes in a sentence what a run does.
func runDescription(config RunConfig) string {
	switch config.Case {
	case "C1":
		return fmt.Sprintf("one client performs %d checkout → pay → terminal-order workflows sequentially, then verifies all invariants", config.ExactOrders)
	case "C4":
		return fmt.Sprintf("an open-loop scheduler offers %.0f workflows/s for %s across all %d products without waiting for earlier workflows", config.OfferedRatePerSecond, config.Measurement, config.ProductCount)
	case "C5":
		if config.ExactOrders > 0 {
			return fmt.Sprintf("%d clients submit %d attempts against one product with %d units, requiring exactly stock-many successes and 409 for the remainder", config.Clients, config.ExactOrders, config.HotStock)
		}
		return fmt.Sprintf("%d closed-loop clients buy one high-stock product for %s to measure single-row contention", config.Clients, config.Measurement)
	case "C6":
		return fmt.Sprintf("%d closed-loop clients send 80%% of carts to five hot products and 20%% to the other 45 for %s", config.Clients, config.Measurement)
	case "C7":
		return fmt.Sprintf("%d closed-loop clients buy randomized two- and three-item carts from five hot products for %s", config.Clients, config.Measurement)
	case "C9":
		return fmt.Sprintf("%d clients inject same-key duplicate checkouts into 20%% of workflows for %s, alternating immediate and concurrent replay", config.Clients, config.Measurement)
	case "C10":
		return fmt.Sprintf("%d clients issue two simultaneous pay calls for every order for %s", config.Clients, config.Measurement)
	case "C11":
		return fmt.Sprintf("%d clients exercise the all-decline cancellation and inventory-release path for %s", config.Clients, config.Measurement)
	case "C12":
		return fmt.Sprintf("%d clients exercise random success, decline, and timeout outcomes for %s", config.Clients, config.Measurement)
	case "C13":
		return fmt.Sprintf("%d clients measure the system with %s simulated gateway latency for %s", config.Clients, config.GatewayLatency, config.Measurement)
	case "C14":
		return fmt.Sprintf("%d clients measure all service DB pools configured to %d connections for %s", config.Clients, config.DBPoolSize, config.Measurement)
	default:
		return fmt.Sprintf("%d closed-loop clients continuously perform checkout → pay → terminal-order workflows for %s across all %d products", config.Clients, config.Measurement, config.ProductCount)
	}
}

// passLabel returns PASS or FAIL depending on the run's invariants.
func passLabel(report Report) string {
	for _, invariant := range report.Invariants {
		if !invariant.Passed {
			return "FAIL"
		}
	}
	if report.Config.Case == "C5" && report.Config.ExactOrders > 0 {
		expected := report.Config.HotStock
		if report.Client.Accepted != expected || report.Client.Completed != expected || report.Client.CheckoutStatusMix.Business409 != int64(report.Config.ExactOrders)-expected {
			return "FAIL"
		}
	} else if report.Config.Case == "C12" {
		if report.Client.Accepted != report.Client.Offered || report.Client.Completed+report.Client.PendingAtDrain != report.Client.Offered {
			return "FAIL"
		}
	} else if report.Config.Case != "C4" && (report.Client.Accepted != report.Client.Offered || report.Client.Completed != report.Client.Offered) {
		return "FAIL"
	}
	return "PASS"
}

// writeLatencyRow writes one row of a latency table.
func writeLatencyRow(out *strings.Builder, label string, latency LatencySummary) {
	fmt.Fprintf(out, "| %s | %d | %.2f | %.2f | %.2f | %.2f |\n", label, latency.Samples, latency.P50MS, latency.P95MS, latency.P99MS, latency.MaxMS)
}

// statusText formats a set of HTTP status counts as text.
func statusText(mix StatusMix) string {
	keys := make([]string, 0, len(mix.ByHTTPStatus))
	for status := range mix.ByHTTPStatus {
		keys = append(keys, status)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys)+3)
	for _, status := range keys {
		parts = append(parts, fmt.Sprintf("HTTP %s=%d", status, mix.ByHTTPStatus[status]))
	}
	parts = append(parts, fmt.Sprintf("transport=%d", mix.TransportErrors), fmt.Sprintf("decode=%d", mix.DecodeErrors), fmt.Sprintf("5xx=%d", mix.Server5xx))
	return strings.Join(parts, ", ")
}

// renderOnly rebuilds load-test.md from the existing load-test.json without
// running a test, for example after editing the stored analysis.
func renderOnly(resultsDir string) error {
	raw, err := os.ReadFile(filepath.Join(resultsDir, "load-test.json"))
	if err != nil {
		return fmt.Errorf("read load-test.json: %w", err)
	}
	var collection ReportCollection
	if err := json.Unmarshal(raw, &collection); err != nil {
		return fmt.Errorf("decode load-test.json: %w", err)
	}
	return atomicWrite(filepath.Join(resultsDir, "load-test.md"), []byte(renderMarkdown(collection)))
}
