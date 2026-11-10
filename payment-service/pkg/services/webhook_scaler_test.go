package services

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
)

func testScaleConfig() WebhookScaleConfig {
	return WebhookScaleConfig{
		Min: 1, Max: 3, Interval: 5 * time.Second,
		UpAge: 2 * time.Second, UpMinBacklog: 10, UpSamples: 2, UpCooldown: 30 * time.Second,
		DownAge: 500 * time.Millisecond, DownBacklog: 5, DownSamples: 3, DownCooldown: 60 * time.Second,
		DownUtilization: 0.6,
	}
}

var t0 = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func at(seconds int) time.Time { return t0.Add(time.Duration(seconds) * time.Second) }

func sampleOf(ready int64, age time.Duration) *WebhookSample {
	return &WebhookSample{Ready: ready, OldestReadyAge: age}
}

func TestWebhookScaleUpNeedsAgeAndANonShrinkingBacklogForConsecutiveSamples(t *testing.T) {
	c := newWebhookScaleController(testScaleConfig(), 3)
	steps := []struct {
		sample *WebhookSample
		want   int
	}{
		{sampleOf(100, 5*time.Second), 0}, // first sample: no previous reading to compare with
		{sampleOf(120, 5*time.Second), 0}, // 1st pressured sample
		{sampleOf(150, 6*time.Second), 1}, // 2nd consecutive: add one worker
	}
	for i, step := range steps {
		got := c.observe(webhookScaleInput{Now: at(5 * i), Workers: 1, Sample: step.sample})
		if got.Delta != step.want {
			t.Fatalf("step %d: delta=%d (%s), want %d", i, got.Delta, got.Reason, step.want)
		}
	}
}

func TestWebhookScaleUpIgnoresYoungBacklogSmallBacklogAndADrainingBacklog(t *testing.T) {
	cases := map[string][]*WebhookSample{
		"old events but tiny backlog":  {sampleOf(3, 9*time.Second), sampleOf(3, 9*time.Second), sampleOf(3, 9*time.Second), sampleOf(3, 9*time.Second)},
		"big backlog but young events": {sampleOf(500, time.Second), sampleOf(500, time.Second), sampleOf(500, time.Second), sampleOf(500, time.Second)},
		"backlog draining by itself":   {sampleOf(2000, 9*time.Second), sampleOf(1500, 8*time.Second), sampleOf(1000, 7*time.Second), sampleOf(600, 6*time.Second)},
	}
	for name, samples := range cases {
		c := newWebhookScaleController(testScaleConfig(), 3)
		for i, sample := range samples {
			if got := c.observe(webhookScaleInput{Now: at(5 * i), Workers: 1, Sample: sample}); got.Delta != 0 {
				t.Fatalf("%s: scaled at sample %d (%s)", name, i, got.Reason)
			}
		}
	}
}

func TestWebhookScaleUpWaitsForTheCooldownBeforeAddingAgain(t *testing.T) {
	c := newWebhookScaleController(testScaleConfig(), 3)
	workers := 1
	added := []int{}
	for i := 0; i < 20; i++ { // a backlog that stays high and keeps growing for 100 s
		got := c.observe(webhookScaleInput{Now: at(5 * i), Workers: workers, Sample: sampleOf(int64(100+i*10), 8*time.Second)})
		if got.Delta == 1 {
			workers++
			added = append(added, 5*i)
		}
	}
	if len(added) < 2 {
		t.Fatalf("worker additions at %v s, want at least two over 100 s", added)
	}
	for i := 1; i < len(added); i++ {
		if gap := added[i] - added[i-1]; gap < 30 {
			t.Fatalf("workers added %d s apart (%v), cooldown is 30 s", gap, added)
		}
	}
}

func TestWebhookScaleNeverExceedsTheCapOrThePoolAllowance(t *testing.T) {
	c := newWebhookScaleController(testScaleConfig(), 3)
	for i := 0; i < 30; i++ {
		if got := c.observe(webhookScaleInput{Now: at(60 * i), Workers: 3, Sample: sampleOf(int64(1000+i), 30*time.Second)}); got.Delta != 0 {
			t.Fatalf("scaled above the cap of 3 at sample %d", i)
		}
	}
	// The pool allowance lowers the cap below the configured maximum.
	cases := []struct{ max, pool, reserve, want int }{
		{10, 10, 4, 6}, {10, 0, 4, 10}, {3, 10, 4, 3}, {10, 3, 4, 1}, {10, 20, 4, 10},
	}
	for _, tc := range cases {
		if got := EffectiveWebhookMaxWorkers(tc.max, tc.pool, tc.reserve); got != tc.want {
			t.Fatalf("EffectiveWebhookMaxWorkers(%d,%d,%d) = %d, want %d", tc.max, tc.pool, tc.reserve, got, tc.want)
		}
	}
	lowered := newWebhookScaleController(testScaleConfig(), 2)
	if got := lowered.observe(webhookScaleInput{Now: at(0), Workers: 2, Sample: sampleOf(100, 9*time.Second)}); got.Delta != 0 {
		t.Fatal("scaled past the effective cap of 2")
	}
}

func TestWebhookScaleDownNeedsSustainedLowDemandAndCapacityForTheLoad(t *testing.T) {
	c := newWebhookScaleController(testScaleConfig(), 3)
	low := sampleOf(0, 0)
	var got webhookScaleDecision
	for i := 0; i < 3; i++ { // 3 workers, light measured load: 0.9 busy-s/s -> 0.45 each with two
		got = c.observe(webhookScaleInput{Now: at(100 + 5*i), Workers: 3, Sample: low, BusyPerSecond: 0.9, BusyOK: true})
		if i < 2 && got.Delta != 0 {
			t.Fatalf("scaled down after only %d low samples", i+1)
		}
	}
	if got.Delta != -1 {
		t.Fatalf("delta=%d (%s), want -1 after 3 low samples", got.Delta, got.Reason)
	}
}

func TestWebhookScaleDownIsRefusedWhenTheRemainingWorkersCannotCarryTheLoad(t *testing.T) {
	c := newWebhookScaleController(testScaleConfig(), 3)
	// The inbox is empty at every sample, but 2 workers are 0.69 busy each (1.38 s of
	// work per second): one worker cannot carry that, so the inbox being empty proves nothing.
	for i := 0; i < 10; i++ {
		got := c.observe(webhookScaleInput{Now: at(100 + 5*i), Workers: 2, Sample: sampleOf(0, 0), BusyPerSecond: 1.38, BusyOK: true})
		if got.Delta != 0 {
			t.Fatalf("scaled down at sample %d although one worker would run at 1.38", i)
		}
	}
	// Unknown load is not evidence either.
	c = newWebhookScaleController(testScaleConfig(), 3)
	for i := 0; i < 10; i++ {
		if got := c.observe(webhookScaleInput{Now: at(100 + 5*i), Workers: 2, Sample: sampleOf(0, 0)}); got.Delta != 0 {
			t.Fatal("scaled down with no load measurement")
		}
	}
}

func TestWebhookScaleDownRespectsTheMinimumAndTheCooldown(t *testing.T) {
	c := newWebhookScaleController(testScaleConfig(), 3)
	for i := 0; i < 20; i++ {
		if got := c.observe(webhookScaleInput{Now: at(5 * i), Workers: 1, Sample: sampleOf(0, 0), BusyPerSecond: 0.1, BusyOK: true}); got.Delta != 0 {
			t.Fatal("scaled below the minimum of 1")
		}
	}
	// A scale-up at t=100 s blocks a scale-down until 60 s later.
	c = newWebhookScaleController(testScaleConfig(), 3)
	c.lastUp = at(100)
	var firstDown int
	for i := 0; i < 30; i++ {
		now := 100 + 5*i
		if got := c.observe(webhookScaleInput{Now: at(now), Workers: 2, Sample: sampleOf(0, 0), BusyPerSecond: 0.2, BusyOK: true}); got.Delta == -1 {
			firstDown = now
			break
		}
	}
	if firstDown == 0 || firstDown-100 < 60 {
		t.Fatalf("scale-down at %d s, want at least 60 s after the scale-up at 100 s", firstDown)
	}
}

func TestWebhookScaleMissingSampleLeavesTheCountAndRestartsTheEvidence(t *testing.T) {
	c := newWebhookScaleController(testScaleConfig(), 3)
	c.observe(webhookScaleInput{Now: at(0), Workers: 1, Sample: sampleOf(100, 5*time.Second)})
	c.observe(webhookScaleInput{Now: at(5), Workers: 1, Sample: sampleOf(120, 5*time.Second)}) // streak 1
	got := c.observe(webhookScaleInput{Now: at(10), Workers: 1, Sample: nil})
	if got.Delta != 0 {
		t.Fatalf("a missing sample changed the worker count: %+v", got)
	}
	// The next pressured sample is only the first of a new run: no scale-up yet.
	if got := c.observe(webhookScaleInput{Now: at(15), Workers: 1, Sample: sampleOf(150, 6*time.Second)}); got.Delta != 0 {
		t.Fatalf("scaled up on stale evidence after a missing sample: %+v", got)
	}
}

// ---- worker pool ----

func fakePool(t *testing.T, process func(worker int) func(context.Context) (int, error)) *WebhookWorkerPool {
	t.Helper()
	cfg := WebhookProcessorConfig{
		Interval: 20 * time.Millisecond, Batch: 10, Lease: time.Second, MaxAttempts: 3,
		BackoffBase: time.Second, BackoffCap: time.Minute, PaymentWindow: time.Minute, GatewayName: "razorpay",
	}
	// Workers are numbered when the pool creates them (1, 2, ...), which is the order
	// Add is called in and so the order Retire removes them in (newest first). Numbering
	// by which goroutine first runs would be a race.
	var mu sync.Mutex
	ids := map[*WebhookProcessor]int{}
	pool := NewWebhookWorkerPool(func() (*WebhookProcessor, error) {
		processor, err := NewWebhookProcessor(&gorm.DB{}, cfg)
		if err != nil {
			return nil, err
		}
		mu.Lock()
		ids[processor] = len(ids) + 1
		mu.Unlock()
		return processor, nil
	})
	pool.processFor = func(p *WebhookProcessor) func(context.Context) (int, error) {
		mu.Lock()
		id := ids[p]
		mu.Unlock()
		return process(id)
	}
	return pool
}

func TestWebhookPoolAddsAndRetiresWorkersAndKeepsAtLeastOne(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	pool := fakePool(t, func(int) func(context.Context) (int, error) {
		return func(context.Context) (int, error) { return 0, nil }
	})
	if err := pool.Start(ctx, 2); err != nil || pool.Count() != 2 {
		t.Fatalf("start: count=%d err=%v", pool.Count(), err)
	}
	if err := pool.Add(); err != nil || pool.Count() != 3 {
		t.Fatalf("add: count=%d err=%v", pool.Count(), err)
	}
	if !pool.Retire() || !pool.Retire() || pool.Count() != 1 {
		t.Fatalf("retire: count=%d, want 1", pool.Count())
	}
	if pool.Retire() || pool.Count() != 1 {
		t.Fatal("the last worker must never be retired")
	}
	cancel()
	pool.Wait()
	if err := pool.Add(); err == nil {
		t.Fatal("a stopped pool accepted a new worker")
	}
}

func TestRetiringWebhookWorkerFinishesItsBatchAndClaimsNothingMore(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var claimsByWorker sync.Map // worker id -> *atomic.Int32
	process := func(worker int) func(context.Context) (int, error) {
		counter, _ := claimsByWorker.LoadOrStore(worker, new(atomic.Int32))
		return func(context.Context) (int, error) {
			n := counter.(*atomic.Int32).Add(1)
			if worker == 2 && n == 1 {
				close(started)
				<-release // worker 2 is in the middle of applying its batch
				return 7, nil
			}
			return 0, nil
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool := fakePool(t, process)
	if err := pool.Start(ctx, 2); err != nil {
		t.Fatal(err)
	}
	<-started
	if !pool.Retire() { // retires worker 2, the newest
		t.Fatal("retire refused")
	}
	if pool.Count() != 1 {
		t.Fatalf("count=%d right after retire, want 1 (the retiring worker is not claiming)", pool.Count())
	}
	done := make(chan struct{})
	go func() { pool.Wait(); close(done) }()
	select {
	case <-done:
		t.Fatal("the retiring worker stopped before its batch was finished")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	time.Sleep(100 * time.Millisecond)
	counter, _ := claimsByWorker.Load(2)
	if got := counter.(*atomic.Int32).Load(); got != 1 {
		t.Fatalf("retired worker claimed %d times, want exactly 1 (no claim after retiring)", got)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pool did not stop after cancellation")
	}
}

func TestWebhookScalerTickAddsAWorkerAfterConsecutivePressureThenHoldsDuringCooldown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool := fakePool(t, func(int) func(context.Context) (int, error) {
		return func(context.Context) (int, error) { return 0, nil }
	})
	if err := pool.Start(ctx, 1); err != nil {
		t.Fatal(err)
	}
	var ready atomic.Int64
	ready.Store(100)
	sampler := func(context.Context) (WebhookSample, error) {
		return WebhookSample{Ready: ready.Add(10), OldestReadyAge: 9 * time.Second}, nil
	}
	scaler, err := NewWebhookScaler(pool, sampler, testScaleConfig(), 3)
	if err != nil {
		t.Fatal(err)
	}
	clock := t0
	scaler.now = func() time.Time { return clock }
	counts := []int{}
	for i := 0; i < 6; i++ {
		scaler.tick(ctx)
		counts = append(counts, pool.Count())
		clock = clock.Add(5 * time.Second)
	}
	if counts[0] != 1 || counts[1] != 1 || counts[2] != 2 {
		t.Fatalf("worker counts per tick %v, want the first addition on the 3rd tick", counts)
	}
	if counts[5] != 2 {
		t.Fatalf("worker counts per tick %v, want no second addition inside the 30 s cooldown", counts)
	}
}

func TestWebhookScalerTickKeepsTheCountWhenTheSampleFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool := fakePool(t, func(int) func(context.Context) (int, error) {
		return func(context.Context) (int, error) { return 0, nil }
	})
	if err := pool.Start(ctx, 2); err != nil {
		t.Fatal(err)
	}
	sampler := func(context.Context) (WebhookSample, error) {
		return WebhookSample{}, errors.New("database unavailable")
	}
	scaler, err := NewWebhookScaler(pool, sampler, testScaleConfig(), 3)
	if err != nil {
		t.Fatal(err)
	}
	clock := t0
	scaler.now = func() time.Time { return clock }
	for i := 0; i < 20; i++ {
		scaler.tick(ctx)
		clock = clock.Add(5 * time.Second)
	}
	if pool.Count() != 2 {
		t.Fatalf("worker count %d after 20 failed samples, want it left at 2", pool.Count())
	}
}

func TestNewWebhookScalerRejectsABadConfiguration(t *testing.T) {
	pool := fakePool(t, nil)
	sampler := func(context.Context) (WebhookSample, error) { return WebhookSample{}, nil }
	bad := testScaleConfig()
	bad.DownUtilization = 1.5
	if _, err := NewWebhookScaler(pool, sampler, bad, 3); err == nil {
		t.Fatal("utilization above 1 was accepted")
	}
	if _, err := NewWebhookScaler(nil, sampler, testScaleConfig(), 3); err == nil {
		t.Fatal("a missing pool was accepted")
	}
}

// The cap follows the pool: a cap of 6 on a pool of 10 rises to 10 when the pool grows to 14.
func TestWebhookScaleCapFollowsInputMaxWorkers(t *testing.T) {
	cfg := testScaleConfig()
	cfg.Max = 10
	c := newWebhookScaleController(cfg, 6)
	at := func(s int) time.Time {
		return time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC).Add(time.Duration(s) * time.Second)
	}
	hot := func(i int) *WebhookSample {
		return &WebhookSample{Ready: int64(100 + i*10), OldestReadyAge: 8 * time.Second}
	}
	var blocked, grew bool
	for i := 0; i < 4; i++ {
		got := c.observe(webhookScaleInput{Now: at(5 * i), Workers: 6, Sample: hot(i), MaxWorkers: 6})
		blocked = blocked || (got.Delta == 0 && strings.Contains(got.Reason, "at cap 6"))
	}
	for i := 4; i < 8; i++ {
		got := c.observe(webhookScaleInput{Now: at(5 * i), Workers: 6, Sample: hot(i), MaxWorkers: 9})
		grew = grew || got.Delta == 1
	}
	if !blocked || !grew {
		t.Fatalf("blocked=%v grew=%v: cap should block at 6, then allow growth when the pool allows 9", blocked, grew)
	}
}
