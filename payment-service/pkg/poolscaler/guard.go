package poolscaler

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/chrisfonseca08/distri/payment-service/pkg/config"
)

// DBGuard decides whether the database can take more concurrent queries. It uses
// its own single connection, so what it measures is the database's round trip and
// lock state, never the wait for a pool connection.
type DBGuard struct {
	probe      *sql.DB
	cfg        config.PoolScalerConfig
	connErrors func() (float64, error)

	baseline    time.Duration
	warmup      []time.Duration
	prevErrors  float64
	haveErrors  bool
	probeBudget time.Duration
}

// NewDBGuard builds the guard. probe should be a separate *sql.DB limited to one
// connection; connErrors returns the running count of connection-type errors seen
// by the service (see ConnectionErrorCounter).
func NewDBGuard(probe *sql.DB, cfg config.PoolScalerConfig, connErrors func() (float64, error)) *DBGuard {
	return &DBGuard{probe: probe, cfg: cfg, connErrors: connErrors, probeBudget: 5 * time.Second}
}

const latencyWarmupSamples = 3

// Check runs the probes. Any failure to read is reported as Unknown, which holds increases.
func (g *DBGuard) Check(ctx context.Context, step int) GuardResult {
	ctx, cancel := context.WithTimeout(ctx, g.probeBudget)
	defer cancel()

	started := time.Now()
	if _, err := g.probe.ExecContext(ctx, "SELECT 1"); err != nil {
		metricSampleErrors.Inc()
		return GuardResult{Unknown: true, Reason: "latency probe failed: " + err.Error()}
	}
	latency := time.Since(started)
	metricProbeLatency.Set(latency.Seconds())

	var locks, live int64
	if err := g.probe.QueryRowContext(ctx, "SELECT count(*) FROM pg_locks WHERE NOT granted").Scan(&locks); err != nil {
		metricSampleErrors.Inc()
		return GuardResult{Unknown: true, Reason: "lock probe failed: " + err.Error()}
	}
	metricUngrantedLocks.Set(float64(locks))
	if err := g.probe.QueryRowContext(ctx, "SELECT count(*) FROM pg_stat_activity WHERE datname = current_database()").Scan(&live); err != nil {
		metricSampleErrors.Inc()
		return GuardResult{Unknown: true, Reason: "connection count probe failed: " + err.Error()}
	}
	metricLiveConnections.Set(float64(live))

	var newErrors float64
	if g.connErrors != nil {
		total, err := g.connErrors()
		if err != nil {
			metricSampleErrors.Inc()
			return GuardResult{Unknown: true, Reason: "connection error counter unreadable: " + err.Error()}
		}
		if g.haveErrors && total >= g.prevErrors {
			newErrors = total - g.prevErrors
		}
		g.prevErrors, g.haveErrors = total, true
	}

	switch {
	case newErrors >= float64(g.cfg.GuardConnErrors):
		return GuardResult{Pause: true, Reason: fmt.Sprintf("%.0f connection errors since the last sample (limit %d)", newErrors, g.cfg.GuardConnErrors)}
	case locks >= g.cfg.GuardLockWaiters:
		return GuardResult{Pause: true, Reason: fmt.Sprintf("%d lock requests waiting (limit %d)", locks, g.cfg.GuardLockWaiters)}
	case live+int64(step) > int64(g.cfg.Budget):
		return GuardResult{Pause: true, Reason: fmt.Sprintf("%d live connections + step %d would exceed the connection budget %d", live, step, g.cfg.Budget)}
	}
	if verdict, reason := g.latencyVerdict(latency); verdict {
		return GuardResult{Pause: true, Reason: reason}
	}
	return GuardResult{}
}

// latencyVerdict compares a probe with the moving average of earlier healthy probes.
// Only healthy probes move the baseline, so a slow database cannot teach the guard
// that slow is normal.
func (g *DBGuard) latencyVerdict(latency time.Duration) (pause bool, reason string) {
	if g.baseline == 0 {
		g.warmup = append(g.warmup, latency)
		if len(g.warmup) >= latencyWarmupSamples {
			low := g.warmup[0]
			for _, v := range g.warmup {
				if v < low {
					low = v
				}
			}
			g.baseline = low
			metricProbeBaseline.Set(g.baseline.Seconds())
		}
		return false, ""
	}
	limit := time.Duration(float64(g.baseline) * g.cfg.GuardLatencyFactor)
	if latency > limit || latency > g.cfg.GuardLatencyMax {
		return true, fmt.Sprintf("database latency %s vs baseline %s (limit %s, max %s)",
			latency.Round(100*time.Microsecond), g.baseline.Round(100*time.Microsecond), limit.Round(100*time.Microsecond), g.cfg.GuardLatencyMax)
	}
	g.baseline = time.Duration(0.9*float64(g.baseline) + 0.1*float64(latency))
	metricProbeBaseline.Set(g.baseline.Seconds())
	return false, ""
}

// connectionErrorKinds are the db_errors_total kinds that mean the database or the
// connection, not the statement, was the problem.
func isConnectionErrorKind(kind string) bool {
	switch kind {
	case "connection", "timeout":
		return true
	}
	return strings.HasPrefix(kind, "pg_08") || strings.HasPrefix(kind, "pg_53") || strings.HasPrefix(kind, "pg_57p")
}

// ConnectionErrorCounter reads the running total of connection-type failures from
// the service's own db_errors_total metric.
func ConnectionErrorCounter(g prometheus.Gatherer) func() (float64, error) {
	return func() (float64, error) {
		families, err := g.Gather()
		if err != nil {
			return 0, err
		}
		var total float64
		for _, family := range families {
			if family.GetName() != "db_errors_total" {
				continue
			}
			for _, metric := range family.GetMetric() {
				if kind := labelValue(metric, "kind"); isConnectionErrorKind(kind) {
					total += metric.GetCounter().GetValue()
				}
			}
		}
		return total, nil
	}
}

func labelValue(m *dto.Metric, name string) string {
	for _, label := range m.GetLabel() {
		if label.GetName() == name {
			return label.GetValue()
		}
	}
	return ""
}
