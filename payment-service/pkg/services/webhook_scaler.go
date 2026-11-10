package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chrisfonseca08/distri/payment-service/pkg/businessmetrics"
	"gorm.io/gorm"
)

// WebhookScaleConfig tunes the webhook worker autoscaler (see config.WebhookScaleConfig).
type WebhookScaleConfig struct {
	Min, Max        int
	Interval        time.Duration
	UpAge           time.Duration
	UpMinBacklog    int64
	UpSamples       int
	UpCooldown      time.Duration
	DownAge         time.Duration
	DownBacklog     int64
	DownSamples     int
	DownCooldown    time.Duration
	DownUtilization float64
}

// WebhookSample is one reading of the claimable inbox.
type WebhookSample struct {
	// Ready is the number of events a worker could claim right now.
	Ready int64
	// OldestReadyAge is how long the oldest of them has been claimable.
	OldestReadyAge time.Duration
}

// WebhookSampler reads the inbox. It must use the claim query's eligibility.
type WebhookSampler func(ctx context.Context) (WebhookSample, error)

// NewWebhookSampler counts the claimable events and the wait of the oldest, using
// exactly the claim query's eligibility (businessmetrics.WebhookClaimable).
func NewWebhookSampler(db *gorm.DB) WebhookSampler {
	return func(ctx context.Context) (WebhookSample, error) {
		var row struct {
			Ready            float64
			OldestAgeSeconds float64
		}
		if err := db.WithContext(ctx).Raw(businessmetrics.WebhookReadySampleSQL).Scan(&row).Error; err != nil {
			return WebhookSample{}, fmt.Errorf("sample webhook inbox: %w", err)
		}
		return WebhookSample{Ready: int64(row.Ready), OldestReadyAge: time.Duration(row.OldestAgeSeconds * float64(time.Second))}, nil
	}
}

// EffectiveWebhookMaxWorkers caps the worker count so the workers cannot take the
// whole database pool: `reserve` connections stay free for ingestion, consumers and
// the relay. A pool limit of 0 means database/sql has no limit, so nothing is clamped.
func EffectiveWebhookMaxWorkers(max, poolMaxOpen, reserve int) int {
	if poolMaxOpen <= 0 {
		return max
	}
	limit := poolMaxOpen - reserve
	if limit < 1 {
		limit = 1
	}
	if limit < max {
		return limit
	}
	return max
}

// ---- worker pool ----

type webhookWorker struct {
	processor *WebhookProcessor
	stop      chan struct{}
}

// WebhookWorkerPool runs a changing number of webhook processing loops over one
// shared inbox and one database pool. Each worker has its own processor (and so its
// own lease identity); rows are handed out by the claim query's SKIP LOCKED.
type WebhookWorkerPool struct {
	newProcessor func() (*WebhookProcessor, error)

	mu      sync.Mutex
	ctx     context.Context
	active  []*webhookWorker
	started bool
	wg      sync.WaitGroup

	busyNanos atomic.Int64
	handled   atomic.Int64

	// processFor returns the function a worker calls to claim and apply one batch.
	// It is processor.processOnce unless a test replaces it.
	processFor func(*WebhookProcessor) func(context.Context) (int, error)
}

// NewWebhookWorkerPool creates a pool whose workers each get a processor from newProcessor.
func NewWebhookWorkerPool(newProcessor func() (*WebhookProcessor, error)) *WebhookWorkerPool {
	return &WebhookWorkerPool{newProcessor: newProcessor}
}

// Start begins n workers. Cancelling ctx stops them all (as shutdown does).
func (p *WebhookWorkerPool) Start(ctx context.Context, n int) error {
	if n <= 0 {
		return errors.New("webhook worker pool needs at least one worker")
	}
	p.mu.Lock()
	if p.started {
		p.mu.Unlock()
		return errors.New("webhook worker pool already started")
	}
	p.ctx, p.started = ctx, true
	p.mu.Unlock()
	for i := 0; i < n; i++ {
		if err := p.Add(); err != nil {
			return err
		}
	}
	return nil
}

// Add starts one more worker.
func (p *WebhookWorkerPool) Add() error {
	processor, err := p.newProcessor()
	if err != nil {
		return err
	}
	processor.onHandled = func(d time.Duration) {
		p.busyNanos.Add(int64(d))
		p.handled.Add(1)
	}
	worker := &webhookWorker{processor: processor, stop: make(chan struct{})}
	p.mu.Lock()
	if !p.started || p.ctx.Err() != nil {
		p.mu.Unlock()
		return errors.New("webhook worker pool is not running")
	}
	p.active = append(p.active, worker)
	count := len(p.active)
	ctx := p.ctx
	p.wg.Add(1)
	p.mu.Unlock()
	businessmetrics.WebhookWorkersActive.Set(float64(count))
	go func() {
		defer p.wg.Done()
		slog.Info("webhook worker started", "worker_id", processor.workerID)
		defer slog.Info("webhook worker stopped", "worker_id", processor.workerID)
		process := processor.processOnce
		if p.processFor != nil {
			process = p.processFor(processor)
		}
		runWebhookWorker(ctx, worker.stop, webhookIdleDelay, processor.config.Interval, process)
	}()
	return nil
}

// Retire asks the newest worker to stop. It is removed from the active count at
// once, finishes the batch it already claimed, and claims nothing more. It reports
// whether a worker was retired; the last worker is never retired.
func (p *WebhookWorkerPool) Retire() bool {
	p.mu.Lock()
	if len(p.active) <= 1 {
		p.mu.Unlock()
		return false
	}
	worker := p.active[len(p.active)-1]
	p.active = p.active[:len(p.active)-1]
	count := len(p.active)
	p.mu.Unlock()
	close(worker.stop)
	businessmetrics.WebhookWorkersActive.Set(float64(count))
	return true
}

// Count is the number of workers that are still claiming (retiring workers excluded).
func (p *WebhookWorkerPool) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.active)
}

// Load reports the cumulative time spent applying events and how many were applied,
// summed over all workers (including retired ones).
func (p *WebhookWorkerPool) Load() (busy time.Duration, handled int64) {
	return time.Duration(p.busyNanos.Load()), p.handled.Load()
}

// Wait blocks until every worker, retiring ones included, has stopped.
func (p *WebhookWorkerPool) Wait() { p.wg.Wait() }

// Run starts `initial` workers, runs the scaler if there is one, and returns when
// ctx is cancelled and every worker has stopped.
func (p *WebhookWorkerPool) Run(ctx context.Context, initial int, scaler *WebhookScaler) {
	if err := p.Start(ctx, initial); err != nil {
		slog.Error("start webhook workers", "error", err)
		p.Wait()
		return
	}
	if scaler != nil {
		scaler.Run(ctx)
	} else {
		<-ctx.Done()
	}
	p.Wait()
}

// ---- scaling decision (pure) ----

// webhookBacklogDrainTolerance: a backlog that shrank by more than this since the
// last sample is draining on its own, so it is not "persisting or growing".
const webhookBacklogDrainTolerance = 0.10

type webhookScaleInput struct {
	Now     time.Time
	Workers int
	// Sample is nil when the reading failed or is missing.
	Sample *WebhookSample
	// BusyPerSecond is the seconds of event-applying work per wall second over the
	// last interval, summed over all workers; BusyOK is false when unknown.
	BusyPerSecond float64
	BusyOK        bool
	// MaxWorkers is the current effective cap (configured max limited by the
	// database pool as it is now). Zero means "use the cap given at construction".
	MaxWorkers int
}

type webhookScaleDecision struct {
	Delta  int    // +1, -1 or 0
	Reason string // why, with the metric that decided
}

type webhookScaleController struct {
	cfg        WebhookScaleConfig
	maxWorkers int // effective cap: min(configured max, what the DB pool allows)

	lastUp, lastDown time.Time
	upStreak         int
	downStreak       int
	prevReady        int64
	havePrev         bool
}

func newWebhookScaleController(cfg WebhookScaleConfig, effectiveMax int) *webhookScaleController {
	if effectiveMax > cfg.Max || effectiveMax < 1 {
		effectiveMax = cfg.Max
	}
	return &webhookScaleController{cfg: cfg, maxWorkers: effectiveMax}
}

func (c *webhookScaleController) observe(in webhookScaleInput) webhookScaleDecision {
	if in.Sample == nil {
		// No evidence either way: keep the setting and require fresh, consecutive
		// samples before acting again.
		c.upStreak, c.downStreak, c.havePrev = 0, 0, false
		return webhookScaleDecision{Reason: "no sample: worker count left unchanged"}
	}
	sample := *in.Sample
	pressured := c.havePrev &&
		sample.Ready >= c.cfg.UpMinBacklog &&
		sample.OldestReadyAge >= c.cfg.UpAge &&
		float64(sample.Ready) >= float64(c.prevReady)*(1-webhookBacklogDrainTolerance)
	c.prevReady, c.havePrev = sample.Ready, true

	if pressured {
		c.upStreak++
		c.downStreak = 0
	} else {
		c.upStreak = 0
	}
	metric := fmt.Sprintf("ready=%d oldest_ready_age=%s", sample.Ready, sample.OldestReadyAge.Round(time.Millisecond))

	if c.upStreak >= c.cfg.UpSamples {
		maxWorkers := c.maxWorkers
		if in.MaxWorkers > 0 {
			maxWorkers = in.MaxWorkers
			if maxWorkers > c.cfg.Max {
				maxWorkers = c.cfg.Max
			}
		}
		switch {
		case in.Workers >= maxWorkers:
			return webhookScaleDecision{Reason: fmt.Sprintf("scale-up blocked: at cap %d (%s)", maxWorkers, metric)}
		case !c.lastUp.IsZero() && in.Now.Sub(c.lastUp) < c.cfg.UpCooldown:
			return webhookScaleDecision{Reason: fmt.Sprintf("scale-up waiting: cooldown (%s)", metric)}
		}
		c.lastUp = in.Now
		c.upStreak, c.downStreak = 0, 0
		return webhookScaleDecision{Delta: 1, Reason: fmt.Sprintf("scale up to %d: %s for %d samples, backlog not shrinking", in.Workers+1, metric, c.cfg.UpSamples)}
	}

	lowDemand := sample.Ready <= c.cfg.DownBacklog && sample.OldestReadyAge <= c.cfg.DownAge
	if !lowDemand || pressured || in.Workers <= c.cfg.Min {
		c.downStreak = 0
		return webhookScaleDecision{}
	}
	// An empty inbox is not enough: removing a worker must leave capacity for the
	// measured load. Busy seconds per second is the work arriving; one fewer worker
	// can do at most (workers-1) seconds of it per second.
	if !in.BusyOK {
		c.downStreak = 0
		return webhookScaleDecision{Reason: "scale-down held: worker load unknown"}
	}
	projected := in.BusyPerSecond / float64(in.Workers-1)
	if projected > c.cfg.DownUtilization {
		c.downStreak = 0
		return webhookScaleDecision{Reason: fmt.Sprintf("scale-down held: %d workers would run at %.2f (limit %.2f), busy=%.2f/s (%s)", in.Workers-1, projected, c.cfg.DownUtilization, in.BusyPerSecond, metric)}
	}
	c.downStreak++
	if c.downStreak < c.cfg.DownSamples {
		return webhookScaleDecision{}
	}
	last := c.lastUp
	if c.lastDown.After(last) {
		last = c.lastDown
	}
	if !last.IsZero() && in.Now.Sub(last) < c.cfg.DownCooldown {
		return webhookScaleDecision{Reason: fmt.Sprintf("scale-down waiting: cooldown (%s)", metric)}
	}
	c.lastDown = in.Now
	c.downStreak = 0
	return webhookScaleDecision{Delta: -1, Reason: fmt.Sprintf("scale down to %d: demand low for %d samples, projected load %.2f with %d workers, busy=%.2f/s (%s)", in.Workers-1, c.cfg.DownSamples, projected, in.Workers-1, in.BusyPerSecond, metric)}
}

// ---- scaler loop ----

// WebhookScaler samples the inbox every Interval and adjusts the pool one worker at
// a time. It reads only the inbox and the workers' own busy time; it knows nothing
// about the database pool or any other controller.
type WebhookScaler struct {
	pool    *WebhookWorkerPool
	sampler WebhookSampler
	cfg     WebhookScaleConfig
	ctl     *webhookScaleController
	now     func() time.Time

	// poolLimit, when set, reports the database pool's current connection limit so
	// the worker cap follows it as it grows or shrinks.
	poolLimit   func() int
	poolReserve int
	lastCap     int

	lastBusy time.Duration
	lastAt   time.Time
	haveLast bool
	lastNote string
}

// NewWebhookScaler builds the scaler. effectiveMax is the cap after the database pool allowance.
func NewWebhookScaler(pool *WebhookWorkerPool, sampler WebhookSampler, cfg WebhookScaleConfig, effectiveMax int) (*WebhookScaler, error) {
	if pool == nil || sampler == nil {
		return nil, errors.New("webhook scaler needs a pool and a sampler")
	}
	if cfg.Interval <= 0 || cfg.Min < 1 || cfg.Max < cfg.Min || cfg.UpSamples < 1 || cfg.DownSamples < 1 ||
		cfg.DownUtilization <= 0 || cfg.DownUtilization >= 1 {
		return nil, errors.New("invalid webhook scaler configuration")
	}
	return &WebhookScaler{pool: pool, sampler: sampler, cfg: cfg, ctl: newWebhookScaleController(cfg, effectiveMax), now: time.Now}, nil
}

// WithPoolLimit makes the worker cap follow the database pool's current limit
// (read on every sample) instead of the value fixed at construction. reserve
// connections always stay free for the rest of the service.
func (s *WebhookScaler) WithPoolLimit(limit func() int, reserve int) *WebhookScaler {
	s.poolLimit, s.poolReserve = limit, reserve
	return s
}

// Run samples until ctx is cancelled.
func (s *WebhookScaler) Run(ctx context.Context) {
	slog.Info("webhook autoscaler started", "min", s.cfg.Min, "max", s.ctl.maxWorkers, "configured_max", s.cfg.Max,
		"interval", s.cfg.Interval, "up_age", s.cfg.UpAge, "up_samples", s.cfg.UpSamples, "up_cooldown", s.cfg.UpCooldown,
		"down_samples", s.cfg.DownSamples, "down_cooldown", s.cfg.DownCooldown, "down_utilization", s.cfg.DownUtilization)
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

func (s *WebhookScaler) tick(ctx context.Context) {
	now := s.now()
	in := webhookScaleInput{Now: now, Workers: s.pool.Count()}
	if s.poolLimit != nil {
		in.MaxWorkers = EffectiveWebhookMaxWorkers(s.cfg.Max, s.poolLimit(), s.poolReserve)
		if in.MaxWorkers != s.lastCap {
			if s.lastCap != 0 {
				slog.Info("webhook worker cap changed with the database pool", "from", s.lastCap, "to", in.MaxWorkers,
					"pool_limit", s.poolLimit(), "reserve", s.poolReserve)
			}
			s.lastCap = in.MaxWorkers
		}
	}

	busy, _ := s.pool.Load()
	if s.haveLast {
		if wall := now.Sub(s.lastAt); wall > 0 {
			in.BusyPerSecond, in.BusyOK = (busy-s.lastBusy).Seconds()/wall.Seconds(), true
			businessmetrics.WebhookBusyRatio.Set(in.BusyPerSecond)
		}
	}
	s.lastBusy, s.lastAt, s.haveLast = busy, now, true

	sampleCtx, cancel := context.WithTimeout(ctx, s.cfg.Interval)
	sample, err := s.sampler(sampleCtx)
	cancel()
	if err != nil {
		businessmetrics.WebhookScaleSampleErrors.Inc()
		slog.Warn("webhook autoscaler sample failed; worker count unchanged", "workers", in.Workers, "error", err)
	} else {
		in.Sample = &sample
	}

	decision := s.ctl.observe(in)
	switch {
	case decision.Delta > 0:
		if err := s.pool.Add(); err != nil {
			slog.Error("webhook autoscaler could not add a worker", "error", err)
			return
		}
		businessmetrics.WebhookScaleEvents.WithLabelValues("up").Inc()
		slog.Info("webhook workers scaled", "direction", "up", "workers", s.pool.Count(), "decision", decision.Reason)
	case decision.Delta < 0:
		if s.pool.Retire() {
			businessmetrics.WebhookScaleEvents.WithLabelValues("down").Inc()
			slog.Info("webhook workers scaled", "direction", "down", "workers", s.pool.Count(), "decision", decision.Reason)
		}
	case decision.Reason != "" && decision.Reason != s.lastNote:
		slog.Info("webhook autoscaler holding", "workers", in.Workers, "decision", decision.Reason)
	}
	s.lastNote = decision.Reason
}
