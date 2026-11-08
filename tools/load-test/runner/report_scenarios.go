package main

import (
	"fmt"
	"sort"
	"strings"
)

// calculateScenarioPoints flattens the runs of the given cases (C9-C14) into
// summary rows. Rows are sorted by label so the output is stable.
func calculateScenarioPoints(reports []Report, cases ...string) []ScenarioPoint {
	wanted := make(map[string]bool, len(cases))
	for _, name := range cases {
		wanted[name] = true
	}
	points := make([]ScenarioPoint, 0)
	for _, report := range reports {
		if !wanted[report.Config.Case] {
			continue
		}
		client, db := report.Client, report.Database
		orderWaits, orderWaitMS := poolWait(report, "order")
		inventoryWaits, inventoryWaitMS := poolWait(report, "inventory")
		paymentWaits, paymentWaitMS := poolWait(report, "payment")
		paymentRequests, poolWaitMS := int64(0), orderWaitMS+inventoryWaitMS+paymentWaitMS
		if payment, found := report.Services["payment"]; found && payment.Inbound != nil {
			paymentRequests = payment.Inbound.Requests
		}
		perOrder := func(total float64) float64 {
			if client.Offered == 0 {
				return 0
			}
			return total / float64(client.Offered)
		}
		points = append(points, ScenarioPoint{
			Label: report.Label, Case: report.Config.Case, GatewayMode: report.Config.GatewayMode,
			GatewayLatencyMS: report.Config.GatewayLatency.Milliseconds(), DBPoolSize: report.Config.DBPoolSize,
			DBIdleSize: report.Config.DBIdleSize, Clients: report.Config.Clients, SessionsOpened: db.SessionsOpened,
			PoolWaitMSPerOrder: perOrder(poolWaitMS), PaymentReqPerOrder: perOrder(float64(paymentRequests)),
			Offered: client.Offered, Paid: client.Paid, Cancelled: client.Cancelled, Pending: client.PendingAtDrain,
			CompletedRate: client.CompletedRate, CheckoutP99MS: client.CheckoutLatency.P99MS,
			PayP50MS: client.PayLatency.P50MS, PayP99MS: client.PayLatency.P99MS, EndToEndP99MS: client.EndToEndLatency.P99MS,
			MaxInFlightRequests: client.InFlight.MaxRequests,
			Server5xx:           client.CheckoutStatusMix.Server5xx + client.PayStatusMix.Server5xx,
			TransportErrors:     client.CheckoutStatusMix.TransportErrors + client.PayStatusMix.TransportErrors,
			OrderPoolWaits:      orderWaits, OrderPoolWaitMS: orderWaitMS,
			InventoryPoolWaits: inventoryWaits, InventoryPoolWaitMS: inventoryWaitMS,
			PaymentPoolWaits: paymentWaits, PaymentPoolWaitMS: paymentWaitMS,
			PostgresConnectionsMax: db.Connections.Max, PostgresActiveMax: db.ActiveConnections.Max,
			IdleInTransactionMax: db.IdleInTransaction.Max, LockWaitersMax: db.LockWaitingSessions.Max,
			DeadlocksDelta: db.DeadlocksAfter - db.DeadlocksBefore,
		})
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Label < points[j].Label })
	return points
}

// poolWait returns how many times a service waited for a database connection and for how long.
func poolWait(report Report, service string) (int64, float64) {
	metrics, found := report.Services[service]
	if !found || metrics.DBPool == nil {
		return 0, 0
	}
	return metrics.DBPool.WaitCount, float64(metrics.DBPool.WaitDuration.Microseconds()) / 1000
}

// renderBehaviourSummary writes the table for the duplicate, double-pay, decline and random-outcome cases.
func renderBehaviourSummary(out *strings.Builder, points []ScenarioPoint) {
	if len(points) == 0 {
		return
	}
	out.WriteString("## C9–C12 behaviour wave\n\n")
	out.WriteString("| Case | Clients | Gateway | Offered | Paid / cancelled / pending | Completed/s | Checkout p99 ms | Pay p99 ms | End-to-end p99 ms | Order / inventory / payment pool waits | Pool wait ms / order | New PG sessions | Idle-in-tx max | 5xx | Deadlocks |\n")
	out.WriteString("|---|---:|---|---:|---|---:|---:|---:|---:|---|---:|---:|---:|---:|---:|\n")
	for _, p := range points {
		fmt.Fprintf(out, "| %s | %d | %s %dms | %d | %d / %d / %d | %.2f | %.2f | %.2f | %.2f | %s | %.1f | %s | %d | %d | %d |\n",
			p.Label, p.Clients, p.GatewayMode, p.GatewayLatencyMS, p.Offered, p.Paid, p.Cancelled, p.Pending, p.CompletedRate,
			p.CheckoutP99MS, p.PayP99MS, p.EndToEndP99MS, waitTriple(p), p.PoolWaitMSPerOrder, sessionsText(p.SessionsOpened), p.IdleInTransactionMax, p.Server5xx+p.TransportErrors, p.DeadlocksDelta)
	}
	out.WriteString("\n")
}

// renderGatewayLatencySummary writes the table for the gateway latency sweep.
func renderGatewayLatencySummary(out *strings.Builder, points []ScenarioPoint) {
	if len(points) == 0 {
		return
	}
	out.WriteString("## C13 gateway latency sweep\n\n")
	out.WriteString("| Gateway latency ms | Clients | Completed/s | Pay p50 ms | Pay p99 ms | End-to-end p99 ms | Max in-flight requests | Payment requests / order | Pool wait ms / order | New PG sessions | Order / inventory / payment pool waits | Postgres active max | 5xx / transport |\n")
	out.WriteString("|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|---:|---:|\n")
	for _, p := range points {
		fmt.Fprintf(out, "| %d | %d | %.2f | %.2f | %.2f | %.2f | %d | %.1f | %.1f | %s | %s | %d | %d / %d |\n",
			p.GatewayLatencyMS, p.Clients, p.CompletedRate, p.PayP50MS, p.PayP99MS, p.EndToEndP99MS, p.MaxInFlightRequests,
			p.PaymentReqPerOrder, p.PoolWaitMSPerOrder, sessionsText(p.SessionsOpened), waitTriple(p), p.PostgresActiveMax, p.Server5xx, p.TransportErrors)
	}
	out.WriteString("\n")
}

// renderPoolSizeSummary writes the table for the database pool size sweep.
func renderPoolSizeSummary(out *strings.Builder, points []ScenarioPoint) {
	if len(points) == 0 {
		return
	}
	out.WriteString("## C14 pool-size sweep\n\n")
	out.WriteString("| Gateway latency ms | Clients | Max open | Max idle | Completed/s | Checkout p99 ms | End-to-end p99 ms | Pool wait ms / order | New PG sessions | Order / inventory / payment pool waits | Postgres connections max | Postgres active max | Idle-in-tx max | 5xx / transport |\n")
	out.WriteString("|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|---:|---:|---:|---:|\n")
	for _, p := range points {
		fmt.Fprintf(out, "| %d | %d | %d | %d | %.2f | %.2f | %.2f | %.1f | %s | %s | %d | %d | %d | %d / %d |\n",
			p.GatewayLatencyMS, p.Clients, p.DBPoolSize, p.DBIdleSize, p.CompletedRate, p.CheckoutP99MS, p.EndToEndP99MS,
			p.PoolWaitMSPerOrder, sessionsText(p.SessionsOpened), waitTriple(p),
			p.PostgresConnectionsMax, p.PostgresActiveMax, p.IdleInTransactionMax, p.Server5xx, p.TransportErrors)
	}
	out.WriteString("\n")
}

// waitTriple formats the order, inventory and payment pool waits as one cell.
func waitTriple(p ScenarioPoint) string {
	return fmt.Sprintf("%d (%.0f ms) / %d (%.0f ms) / %d (%.0f ms)",
		p.OrderPoolWaits, p.OrderPoolWaitMS, p.InventoryPoolWaits, p.InventoryPoolWaitMS, p.PaymentPoolWaits, p.PaymentPoolWaitMS)
}

// summarizeRun reduces a report to the figures kept in the repeat history.
func summarizeRun(report Report) RunSummary {
	return RunSummary{
		StartedAt: report.StartedAt, CompletedRate: report.Client.CompletedRate,
		EndToEndP95MS: report.Client.EndToEndLatency.P95MS, SessionsOpened: report.Database.SessionsOpened,
		GatewayMode: report.Config.GatewayMode, GatewayLatencyMS: report.Config.GatewayLatency.Milliseconds(),
		DBPoolSize: report.Config.DBPoolSize, DBIdleSize: report.Config.DBIdleSize,
	}
}

// RepeatPoint summarises every recorded run of one label (earlier runs plus the current one).
type RepeatPoint struct {
	Label  string
	Values []float64
	Stats  VarianceSummary
}

// calculateRepeatHistory returns labels that have been run more than once, with the
// completed-per-second values oldest first and their spread.
func calculateRepeatHistory(reports []Report) []RepeatPoint {
	points := make([]RepeatPoint, 0)
	for _, report := range reports {
		if len(report.PreviousRuns) == 0 {
			continue
		}
		values := make([]float64, 0, len(report.PreviousRuns)+1)
		for _, previous := range report.PreviousRuns {
			values = append(values, previous.CompletedRate)
		}
		values = append(values, report.Client.CompletedRate)
		points = append(points, RepeatPoint{Label: report.Label, Values: values, Stats: summarizeVariance(values)})
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Label < points[j].Label })
	return points
}

// renderRepeatHistory writes the table of earlier runs of the same case.
func renderRepeatHistory(out *strings.Builder, reports []Report) {
	points := calculateRepeatHistory(reports)
	if len(points) == 0 {
		return
	}
	out.WriteString("## Repeat history\n\n")
	out.WriteString("Completed orders/s of every recorded run of a label, oldest first. The last value is the run shown in the sections below; a large spread means single runs of that case cannot show small effects.\n\n")
	out.WriteString("| Label | Runs | Completed/s per run | Mean | Min | Max | CV |\n|---|---:|---|---:|---:|---:|---:|\n")
	for _, p := range points {
		parts := make([]string, len(p.Values))
		for i, value := range p.Values {
			parts[i] = fmt.Sprintf("%.1f", value)
		}
		fmt.Fprintf(out, "| %s | %d | %s | %.2f | %.2f | %.2f | %.1f%% |\n",
			p.Label, len(p.Values), strings.Join(parts, ", "), p.Stats.Mean, p.Stats.Min, p.Stats.Max, p.Stats.CoefficientPct)
	}
	out.WriteString("\n")
}

// sessionsText formats a session count, or n/a when it is unknown.
func sessionsText(value *int64) string {
	if value == nil {
		return "n/a"
	}
	return fmt.Sprintf("%d", *value)
}
