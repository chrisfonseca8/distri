package poolscaler

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/config"
)

func testCfg() config.PoolScalerConfig {
	return config.PoolScalerConfig{
		Enabled: true, Max: 50, Step: 5, Interval: 5 * time.Second,
		UpWait: 5 * time.Millisecond, UpMinWaits: 10, UpNearFull: 0.8, UpSamples: 2, UpCooldown: 30 * time.Second,
		DownUsage: 0.3, DownSamples: 3, DownCooldown: 60 * time.Second,
		Budget: 100, BudgetReserved: 20, Replicas: 2, // this process's share: (100-20)/2 = 40
		GuardLatencyFactor: 3, GuardLatencyMax: 100 * time.Millisecond, GuardLockWaiters: 5, GuardConnErrors: 3,
	}
}

var base = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func tm(seconds int) time.Time { return base.Add(time.Duration(seconds) * time.Second) }

// feed builds cumulative statistics the way sql.DB reports them.
type feed struct {
	count int64
	wait  time.Duration
}

func (f *feed) next(maxOpen, inUse int, blocked int64, avgWait time.Duration) *sample {
	f.count += blocked
	f.wait += time.Duration(blocked) * avgWait
	return &sample{MaxOpen: maxOpen, InUse: inUse, Open: maxOpen, WaitCount: f.count, WaitDuration: f.wait}
}

func ok() GuardResult { return GuardResult{} }

func TestScaleUpUsesDeltasOfWaitCountAndDurationInStepsOfFive(t *testing.T) {
	c := newController(testCfg(), 10)
	// A long history of cheap waits: the lifetime average is tiny, the recent one is not.
	f := &feed{count: 5_000_000, wait: 5_000_000 * time.Microsecond}
	if d := c.observe(tm(0), f.next(10, 10, 0, 0), ok()); d.NewCap != 0 {
		t.Fatal("scaled on the first sample")
	}
	if d := c.observe(tm(5), f.next(10, 10, 200, 8*time.Millisecond), ok()); d.NewCap != 0 || d.AvgWait != 8*time.Millisecond || d.BlockedDelta != 200 {
		t.Fatalf("first high-wait sample: %+v (avg wait must be the interval's 8ms, not the lifetime ~1us)", d)
	}
	d := c.observe(tm(10), f.next(10, 10, 200, 8*time.Millisecond), ok())
	if d.NewCap != 15 {
		t.Fatalf("new cap %d (%s), want 15 after two consecutive high-wait samples", d.NewCap, d.Reason)
	}
}

func TestScaleUpNeedsHighAverageWaitEnoughBlockedAcquisitionsAndANearFullPool(t *testing.T) {
	cases := map[string]struct {
		inUse   int
		blocked int64
		avg     time.Duration
	}{
		"low average wait":       {10, 500, 500 * time.Microsecond},
		"too few blocked":        {10, 3, 50 * time.Millisecond},
		"pool far from full":     {4, 500, 20 * time.Millisecond},
		"no waits, pool is full": {10, 0, 0},
	}
	for name, tc := range cases {
		c := newController(testCfg(), 10)
		f := &feed{}
		c.observe(tm(0), f.next(10, tc.inUse, 0, 0), ok())
		for i := 1; i <= 6; i++ {
			if d := c.observe(tm(5*i), f.next(10, tc.inUse, tc.blocked, tc.avg), ok()); d.NewCap != 0 {
				t.Fatalf("%s: scaled up (%s)", name, d.Reason)
			}
		}
	}
}

func TestScaleUpWaitsForTheCooldown(t *testing.T) {
	c := newController(testCfg(), 10)
	f := &feed{}
	cap := 10
	var ups []int
	c.observe(tm(0), f.next(cap, cap, 0, 0), ok())
	for i := 1; i <= 24; i++ { // saturated for two minutes
		if d := c.observe(tm(5*i), f.next(cap, cap, 300, 10*time.Millisecond), ok()); d.NewCap > 0 {
			cap = d.NewCap
			ups = append(ups, 5*i)
		}
	}
	if len(ups) < 2 {
		t.Fatalf("scale-ups at %v s, want at least two in two minutes", ups)
	}
	for i := 1; i < len(ups); i++ {
		if ups[i]-ups[i-1] < 30 {
			t.Fatalf("scale-ups %v s apart, cooldown is 30 s", ups)
		}
	}
}

func TestCeilingIsTheSmallestOfHardMaxConfiguredMaxAndTheBudgetShare(t *testing.T) {
	cases := []struct {
		name string
		edit func(*config.PoolScalerConfig)
		want int
	}{
		{"budget share of two replicas", func(c *config.PoolScalerConfig) {}, 40},
		{"one replica, ample budget", func(c *config.PoolScalerConfig) { c.Replicas, c.Budget, c.BudgetReserved = 1, 1000, 0 }, 50},
		{"configured max below everything", func(c *config.PoolScalerConfig) { c.Max = 25 }, 25},
		{"tight budget", func(c *config.PoolScalerConfig) { c.Budget, c.BudgetReserved, c.Replicas = 30, 10, 1 }, 20},
		{"hard max wins over a huge configured max", func(c *config.PoolScalerConfig) { c.Max, c.Budget, c.Replicas = 500, 5000, 1 }, 50},
	}
	for _, tc := range cases {
		cfg := testCfg()
		tc.edit(&cfg)
		if got := cfg.Ceiling(); got != tc.want {
			t.Fatalf("%s: ceiling %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestScaleUpNeverExceedsTheCeilingAndTheLastStepIsClamped(t *testing.T) {
	c := newController(testCfg(), 10) // ceiling 40
	f := &feed{}
	cap := 38
	c.observe(tm(0), f.next(cap, cap, 0, 0), ok())
	c.observe(tm(5), f.next(cap, cap, 300, 10*time.Millisecond), ok())
	d := c.observe(tm(10), f.next(cap, cap, 300, 10*time.Millisecond), ok())
	if d.NewCap != 40 {
		t.Fatalf("new cap %d, want the step clamped to the ceiling 40", d.NewCap)
	}
	cap = 40
	for i := 3; i < 40; i++ {
		if d := c.observe(tm(5*i+100), f.next(cap, cap, 300, 10*time.Millisecond), ok()); d.NewCap != 0 {
			t.Fatalf("scaled above the ceiling: %d", d.NewCap)
		}
	}
	hard := testCfg()
	hard.Max, hard.Budget, hard.Replicas, hard.BudgetReserved = 50, 10_000, 1, 0
	c = newController(hard, 10)
	cap = 48
	c.observe(tm(0), f.next(cap, cap, 0, 0), ok())
	c.observe(tm(5), f.next(cap, cap, 300, 10*time.Millisecond), ok())
	if d := c.observe(tm(10), f.next(cap, cap, 300, 10*time.Millisecond), ok()); d.NewCap != 50 {
		t.Fatalf("new cap %d, want 50, the hard ceiling", d.NewCap)
	}
}

func TestIncreasesArePausedWhileTheDatabaseGuardSaysSo(t *testing.T) {
	for name, guard := range map[string]GuardResult{
		"database slow":    {Pause: true, Reason: "database latency 80ms vs baseline 2ms"},
		"lock contention":  {Pause: true, Reason: "9 lock requests waiting"},
		"connection errs":  {Pause: true, Reason: "5 connection errors since the last sample"},
		"guard unreadable": {Unknown: true, Reason: "latency probe failed"},
	} {
		c := newController(testCfg(), 10)
		f := &feed{}
		c.observe(tm(0), f.next(10, 10, 0, 0), ok())
		for i := 1; i <= 8; i++ {
			if d := c.observe(tm(5*i), f.next(10, 10, 300, 10*time.Millisecond), guard); d.NewCap != 0 {
				t.Fatalf("%s: scaled up while the guard was holding (%s)", name, d.Reason)
			}
		}
		// Released: pressure must be seen again for UpSamples fresh samples first.
		if d := c.observe(tm(60), f.next(10, 10, 300, 10*time.Millisecond), ok()); d.NewCap != 0 {
			t.Fatalf("%s: scaled on the first sample after the guard released", name)
		}
		if d := c.observe(tm(65), f.next(10, 10, 300, 10*time.Millisecond), ok()); d.NewCap != 15 {
			t.Fatalf("%s: new cap %d (%s), want 15 once the guard is clear", name, d.NewCap, d.Reason)
		}
	}
}

func TestScaleDownIsGradualAfterSustainedLowUsageAndNeverBelowTheStart(t *testing.T) {
	c := newController(testCfg(), 10)
	f := &feed{}
	cap := 20
	var sizes []int
	c.observe(tm(0), f.next(cap, 3, 0, 0), ok())
	for i := 1; i <= 60; i++ {
		d := c.observe(tm(100+5*i), f.next(cap, 3, 0, 0), ok())
		if d.NewCap > 0 {
			cap = d.NewCap
			sizes = append(sizes, cap)
		}
	}
	if len(sizes) != 2 || sizes[0] != 15 || sizes[1] != 10 {
		t.Fatalf("pool sizes after scale-downs %v, want 15 then 10 (steps of 5, floor at the start)", sizes)
	}
}

func TestScaleDownNeedsNoBlockedAcquisitionsAndRespectsTheCooldown(t *testing.T) {
	c := newController(testCfg(), 10)
	f := &feed{}
	c.observe(tm(0), f.next(20, 3, 0, 0), ok())
	for i := 1; i <= 30; i++ { // low in-use but a trickle of blocked acquisitions
		if d := c.observe(tm(5*i), f.next(20, 3, 1, time.Millisecond), ok()); d.NewCap != 0 {
			t.Fatal("scaled down although acquisitions were still blocking")
		}
	}
	c = newController(testCfg(), 10)
	c.lastUp = tm(100)
	f = &feed{}
	c.observe(tm(100), f.next(20, 3, 0, 0), ok())
	first := 0
	for i := 1; i <= 30; i++ {
		if d := c.observe(tm(100+5*i), f.next(20, 3, 0, 0), ok()); d.NewCap > 0 {
			first = 5 * i
			break
		}
	}
	if first < 60 {
		t.Fatalf("scale-down %d s after a scale-up, want at least 60 s", first)
	}
}

func TestMissingStatisticsLeaveThePoolAndRestartTheEvidence(t *testing.T) {
	c := newController(testCfg(), 10)
	f := &feed{}
	c.observe(tm(0), f.next(10, 10, 0, 0), ok())
	c.observe(tm(5), f.next(10, 10, 300, 10*time.Millisecond), ok()) // streak 1
	if d := c.observe(tm(10), nil, ok()); d.NewCap != 0 {
		t.Fatal("a missing sample resized the pool")
	}
	if d := c.observe(tm(15), f.next(10, 10, 300, 10*time.Millisecond), ok()); d.NewCap != 0 {
		t.Fatal("scaled on stale evidence after a missing sample")
	}
	// Counters that go backwards (a restart of the counter) are not an interval either.
	c = newController(testCfg(), 10)
	f = &feed{}
	c.observe(tm(0), f.next(10, 10, 500, 10*time.Millisecond), ok())
	if d := c.observe(tm(5), &sample{MaxOpen: 10, InUse: 10, WaitCount: 1, WaitDuration: time.Millisecond}, ok()); d.NewCap != 0 {
		t.Fatal("scaled on a backwards counter")
	}
}

// ---- the loop against a fake pool ----

type fakePool struct {
	stats      sql.DBStats
	open, idle []int
}

func (p *fakePool) Stats() sql.DBStats    { return p.stats }
func (p *fakePool) SetMaxOpenConns(n int) { p.open = append(p.open, n); p.stats.MaxOpenConnections = n }
func (p *fakePool) SetMaxIdleConns(n int) { p.idle = append(p.idle, n) }

type fakeGuard struct{ result GuardResult }

func (g *fakeGuard) Check(context.Context, int) GuardResult { return g.result }

func TestScalerTickResizesOpenAndIdleTogetherAndLogsTheTrigger(t *testing.T) {
	pool := &fakePool{stats: sql.DBStats{MaxOpenConnections: 10, InUse: 10}}
	guard := &fakeGuard{}
	scaler, err := New(pool, guard, testCfg())
	if err != nil {
		t.Fatal(err)
	}
	clock := base
	scaler.now = func() time.Time { return clock }
	for i := 0; i < 4; i++ {
		pool.stats.WaitCount += 300
		pool.stats.WaitDuration += 300 * 10 * time.Millisecond
		scaler.tick(context.Background())
		clock = clock.Add(5 * time.Second)
	}
	if len(pool.open) != 1 || pool.open[0] != 15 || len(pool.idle) != 1 || pool.idle[0] != 15 {
		t.Fatalf("SetMaxOpenConns %v / SetMaxIdleConns %v, want one resize to 15 for both", pool.open, pool.idle)
	}
	if scaler.Ceiling() != 40 {
		t.Fatalf("ceiling %d, want 40", scaler.Ceiling())
	}
}

func TestScalerHoldsWhenTheGuardCannotBeRead(t *testing.T) {
	pool := &fakePool{stats: sql.DBStats{MaxOpenConnections: 10, InUse: 10}}
	scaler, err := New(pool, &fakeGuard{result: GuardResult{Unknown: true, Reason: "probe timed out"}}, testCfg())
	if err != nil {
		t.Fatal(err)
	}
	clock := base
	scaler.now = func() time.Time { return clock }
	for i := 0; i < 30; i++ {
		pool.stats.WaitCount += 300
		pool.stats.WaitDuration += 3 * time.Second
		scaler.tick(context.Background())
		clock = clock.Add(5 * time.Second)
	}
	if len(pool.open) != 0 {
		t.Fatalf("pool resized %v while the guard was unreadable", pool.open)
	}
}

func TestNewRejectsAPoolWithoutALimitAndABadConfiguration(t *testing.T) {
	if _, err := New(&fakePool{}, &fakeGuard{}, testCfg()); err == nil || !strings.Contains(err.Error(), "DB_MAX_OPEN_CONNS") {
		t.Fatalf("pool with no limit: err=%v, want a DB_MAX_OPEN_CONNS error", err)
	}
	bad := testCfg()
	bad.Budget = 0
	if _, err := New(&fakePool{stats: sql.DBStats{MaxOpenConnections: 10}}, &fakeGuard{}, bad); err == nil {
		t.Fatal("a missing connection budget was accepted")
	}
	if _, err := New(nil, &fakeGuard{}, testCfg()); err == nil {
		t.Fatal("a missing pool was accepted")
	}
}
