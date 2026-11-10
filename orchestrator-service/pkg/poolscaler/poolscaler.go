// Package poolscaler resizes the database/sql connection pool at runtime. It reads
// only the pool's own statistics and a database health guard; it knows nothing about
// RabbitMQ consumers or any other controller, and nothing else calls into it.
package poolscaler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/config"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/observability"
)

// Pool is the part of *sql.DB the scaler uses.
type Pool interface {
	Stats() sql.DBStats
	SetMaxOpenConns(n int)
	SetMaxIdleConns(n int)
}

// GuardResult says whether the database is healthy enough for more concurrent queries.
type GuardResult struct {
	// Pause blocks increases because more concurrent queries would likely make a
	// slowdown worse (latency, lock waits, connection errors, connection budget).
	Pause bool
	// Unknown means the guard could not be read; increases are also held.
	Unknown bool
	Reason  string
}

// Guard inspects the database. step is the size of the increase being considered.
type Guard interface {
	Check(ctx context.Context, step int) GuardResult
}

// sample is one reading of the pool statistics.
type sample struct {
	MaxOpen, InUse, Open int
	WaitCount            int64
	WaitDuration         time.Duration
}

func sampleOf(s sql.DBStats) sample {
	return sample{MaxOpen: s.MaxOpenConnections, InUse: s.InUse, Open: s.OpenConnections, WaitCount: s.WaitCount, WaitDuration: s.WaitDuration}
}

type decision struct {
	NewCap int    // 0 means leave the pool as it is
	Reason string // why, with the metric that decided
	// derived values for metrics and logs
	AvgWait      time.Duration
	BlockedDelta int64
}

type controller struct {
	cfg     config.PoolScalerConfig
	min     int
	ceiling int

	prev       sample
	havePrev   bool
	upStreak   int
	downStreak int
	lastUp     time.Time
	lastDown   time.Time
}

func newController(cfg config.PoolScalerConfig, start int) *controller {
	return &controller{cfg: cfg, min: start, ceiling: cfg.Ceiling()}
}

// observe turns one sample into a decision. A nil sample (statistics unavailable)
// changes nothing and restarts the evidence.
func (c *controller) observe(now time.Time, s *sample, guard GuardResult) decision {
	if s == nil {
		c.upStreak, c.downStreak, c.havePrev = 0, 0, false
		return decision{Reason: "no pool statistics: pool left unchanged"}
	}
	cur := *s
	prev, hadPrev := c.prev, c.havePrev
	c.prev, c.havePrev = cur, true
	if !hadPrev {
		return decision{}
	}
	dCount := cur.WaitCount - prev.WaitCount
	dWait := cur.WaitDuration - prev.WaitDuration
	if dCount < 0 || dWait < 0 { // counters went backwards: not a usable interval
		c.upStreak, c.downStreak = 0, 0
		return decision{Reason: "pool wait counters went backwards: sample ignored"}
	}
	var avg time.Duration
	if dCount > 0 {
		avg = dWait / time.Duration(dCount)
	}
	out := decision{AvgWait: avg, BlockedDelta: dCount}

	capNow := cur.MaxOpen
	if capNow <= 0 {
		c.upStreak, c.downStreak = 0, 0
		out.Reason = "pool has no connection limit: nothing to scale"
		return out
	}
	near := float64(cur.InUse) >= c.cfg.UpNearFull*float64(capNow)
	highWait := dCount >= c.cfg.UpMinWaits && avg >= c.cfg.UpWait
	metric := fmt.Sprintf("avg_wait=%s blocked=%d in_use=%d/%d", avg.Round(100*time.Microsecond), dCount, cur.InUse, capNow)

	if highWait && near {
		c.upStreak++
		c.downStreak = 0
	} else {
		c.upStreak = 0
	}

	if c.upStreak >= c.cfg.UpSamples {
		switch {
		case capNow >= c.ceiling:
			out.Reason = fmt.Sprintf("scale-up blocked: at ceiling %d (%s)", c.ceiling, metric)
		case guard.Unknown:
			c.upStreak = 0
			out.Reason = fmt.Sprintf("scale-up held: database guard unavailable (%s) (%s)", guard.Reason, metric)
		case guard.Pause:
			c.upStreak = 0
			out.Reason = fmt.Sprintf("scale-up paused: %s (%s)", guard.Reason, metric)
		case !c.lastUp.IsZero() && now.Sub(c.lastUp) < c.cfg.UpCooldown:
			out.Reason = fmt.Sprintf("scale-up waiting: cooldown (%s)", metric)
		default:
			newCap := capNow + c.cfg.Step
			if newCap > c.ceiling {
				newCap = c.ceiling
			}
			c.lastUp = now
			c.upStreak, c.downStreak = 0, 0
			out.NewCap = newCap
			out.Reason = fmt.Sprintf("scale up %d -> %d: %s for %d samples", capNow, newCap, metric, c.cfg.UpSamples)
		}
		return out
	}

	low := dCount == 0 && float64(cur.InUse) <= c.cfg.DownUsage*float64(capNow)
	if !low || capNow <= c.min {
		c.downStreak = 0
		return out
	}
	c.downStreak++
	if c.downStreak < c.cfg.DownSamples {
		return out
	}
	last := c.lastUp
	if c.lastDown.After(last) {
		last = c.lastDown
	}
	if !last.IsZero() && now.Sub(last) < c.cfg.DownCooldown {
		out.Reason = fmt.Sprintf("scale-down waiting: cooldown (%s)", metric)
		return out
	}
	newCap := capNow - c.cfg.Step
	if newCap < c.min {
		newCap = c.min
	}
	c.lastDown = now
	c.downStreak = 0
	out.NewCap = newCap
	out.Reason = fmt.Sprintf("scale down %d -> %d: in_use=%d (<= %.0f%% of cap) with no blocked acquisitions for %d samples", capNow, newCap, cur.InUse, c.cfg.DownUsage*100, c.cfg.DownSamples)
	return out
}

// Scaler runs the controller on a timer against a real pool.
type Scaler struct {
	pool  Pool
	guard Guard
	cfg   config.PoolScalerConfig
	ctl   *controller
	now   func() time.Time

	lastNote string
}

// New builds a scaler whose floor is the pool's current limit. It refuses a pool
// with no limit, and starts without headroom if the budget leaves none.
func New(pool Pool, guard Guard, cfg config.PoolScalerConfig) (*Scaler, error) {
	if pool == nil || guard == nil {
		return nil, errors.New("pool scaler needs a pool and a guard")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	start := pool.Stats().MaxOpenConnections
	if start <= 0 {
		return nil, errors.New("pool autoscaling needs DB_MAX_OPEN_CONNS to be set above 0")
	}
	return &Scaler{pool: pool, guard: guard, cfg: cfg, ctl: newController(cfg, start), now: time.Now}, nil
}

// Ceiling is the effective maximum for this process.
func (s *Scaler) Ceiling() int { return s.ctl.ceiling }

// Run samples until ctx is cancelled.
func (s *Scaler) Run(ctx context.Context) {
	slog.Info("database pool autoscaler started", "start", s.ctl.min, "ceiling", s.ctl.ceiling, "hard_max", config.HardMaxPoolConnections,
		"configured_max", s.cfg.Max, "budget", s.cfg.Budget, "budget_reserved", s.cfg.BudgetReserved, "replicas", s.cfg.Replicas,
		"step", s.cfg.Step, "interval", s.cfg.Interval, "up_wait", s.cfg.UpWait, "up_samples", s.cfg.UpSamples, "up_cooldown", s.cfg.UpCooldown,
		"down_samples", s.cfg.DownSamples, "down_cooldown", s.cfg.DownCooldown)
	if s.ctl.ceiling <= s.ctl.min {
		slog.Warn("database pool autoscaler has no headroom above the starting pool; it can only hold", "start", s.ctl.min, "ceiling", s.ctl.ceiling)
	}
	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Scaler) tick(ctx context.Context) {
	now := s.now()
	stats := sampleOf(s.pool.Stats())
	checkCtx, cancel := context.WithTimeout(ctx, s.cfg.Interval)
	guard := s.guard.Check(checkCtx, s.cfg.Step)
	cancel()
	d := s.ctl.observe(now, &stats, guard)

	metricCap.Set(float64(stats.MaxOpen))
	metricInUse.Set(float64(stats.InUse))
	metricCeiling.Set(float64(s.ctl.ceiling))
	metricAcquireWait.Set(d.AvgWait.Seconds())
	metricBlockedPerSecond.Set(float64(d.BlockedDelta) / s.cfg.Interval.Seconds())
	paused := 0.0
	if guard.Pause || guard.Unknown {
		paused = 1
	}
	metricGuardHolding.Set(paused)

	switch {
	case d.NewCap > 0:
		s.pool.SetMaxOpenConns(d.NewCap)
		// Keep idle connections in step with the cap: an idle limit below the cap
		// would close and reopen connections on every burst.
		s.pool.SetMaxIdleConns(d.NewCap)
		direction := "up"
		if d.NewCap < stats.MaxOpen {
			direction = "down"
		}
		metricScaleEvents.WithLabelValues(direction).Inc()
		metricCap.Set(float64(d.NewCap))
		slog.Info("database pool resized", "direction", direction, "from", stats.MaxOpen, "to", d.NewCap, "decision", d.Reason)
	case d.Reason != "" && d.Reason != s.lastNote:
		slog.Info("database pool autoscaler holding", "cap", stats.MaxOpen, "decision", d.Reason)
	}
	s.lastNote = d.Reason
}

// ---- metrics ----

var (
	metricCap = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "orchestrator_db_pool_cap", Help: "Current database/sql MaxOpenConns of this process."})
	metricInUse = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "orchestrator_db_pool_in_use", Help: "Connections in use at the last autoscaler sample."})
	metricCeiling = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "orchestrator_db_pool_ceiling", Help: "Largest pool the autoscaler may set (hard limit, configured maximum and connection budget share)."})
	metricAcquireWait = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "orchestrator_db_pool_acquire_wait_seconds", Help: "Average wait per blocked connection acquisition over the last sample interval, from DB.Stats WaitDuration/WaitCount deltas. SQL execution time is not included."})
	metricBlockedPerSecond = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "orchestrator_db_pool_blocked_acquisitions_per_second", Help: "Connection acquisitions that had to wait, per second, over the last sample interval."})
	metricGuardHolding = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "orchestrator_db_pool_scale_up_held", Help: "1 while the database guard (latency, locks, connection errors, budget, or an unreadable probe) blocks pool increases."})
	metricScaleEvents = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "orchestrator_db_pool_scale_events_total", Help: "Pool resizes by direction."}, []string{"direction"})
	metricProbeLatency = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "orchestrator_db_probe_latency_seconds", Help: "Round trip of SELECT 1 on the autoscaler's own connection (no pool wait)."})
	metricProbeBaseline = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "orchestrator_db_probe_baseline_seconds", Help: "Healthy-probe moving average the latency guard compares against."})
	metricUngrantedLocks = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "orchestrator_db_ungranted_locks", Help: "Lock requests waiting in pg_locks at the last guard check."})
	metricLiveConnections = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "orchestrator_db_live_connections", Help: "Connections to this database from all clients at the last guard check."})
	metricSampleErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "orchestrator_db_pool_guard_errors_total", Help: "Guard checks that could not be read; pool increases were held."})
)

// RegisterMetrics exposes the autoscaler's metrics. Call it once, only when the
// autoscaler is enabled.
func RegisterMetrics() {
	observability.Registry.MustRegister(metricCap, metricInUse, metricCeiling, metricAcquireWait, metricBlockedPerSecond,
		metricGuardHolding, metricScaleEvents, metricProbeLatency, metricProbeBaseline, metricUngrantedLocks,
		metricLiveConnections, metricSampleErrors)
}
