package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultWebhookMaxBody  = 256 << 10
	defaultWebhookInterval = time.Second
	defaultWebhookBatch    = 20
	defaultWebhookWorkers  = 1

	// Autoscaling defaults (PAYMENT_WEBHOOK_SCALE_*). Autoscaling is off unless asked for.
	defaultScaleMinWorkers    = 1
	defaultScaleMaxWorkers    = 10
	defaultScaleInterval      = 5 * time.Second
	defaultScaleUpAge         = 2 * time.Second
	defaultScaleUpMinBacklog  = 10
	defaultScaleUpSamples     = 2
	defaultScaleUpCooldown    = 30 * time.Second
	defaultScaleDownAge       = 500 * time.Millisecond
	defaultScaleDownBacklog   = 5
	defaultScaleDownSamples   = 12
	defaultScaleDownCooldown  = 60 * time.Second
	defaultScaleDownUtil      = 0.6
	defaultScalePoolReserve   = 4
	defaultWebhookLease       = 30 * time.Second
	defaultWebhookMaxAttempts = 12
	defaultWebhookBackoffBase = time.Second
	defaultWebhookBackoffCap  = 2 * time.Minute
)

// WebhookScaleConfig tunes the optional webhook worker autoscaler. The scaler adds
// one worker when the oldest claimable event has waited longer than UpAge for
// UpSamples consecutive samples while the claimable backlog is not shrinking, and
// removes one when demand has been low for DownSamples samples and the remaining
// workers could carry the measured load.
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
	// PoolReserve is how many database connections the webhook workers must leave
	// to everything else in the process (ingestion, consumers, relay).
	PoolReserve int
}

// WebhookConfig configures webhook ingestion and the worker that applies events.
type WebhookConfig struct {
	// Secret is the dedicated webhook secret (RAZORPAY_WEBHOOK_SECRET), separate
	// from the API key secret. Empty disables the endpoint: polling alone then
	// completes payments.
	Secret       string
	MaxBodyBytes int64
	// Interval is how often the worker looks for pending events.
	Interval time.Duration
	Batch    int
	// Workers is the fixed number of goroutines running the inbox processing loop
	// in this process (PAYMENT_WEBHOOK_WORKERS). Each claims its own batches.
	Workers int
	// Autoscale lets the worker count follow the claimable backlog
	// (PAYMENT_WEBHOOK_AUTOSCALE=true). Workers is then the starting count.
	Autoscale bool
	Scale     WebhookScaleConfig
	Lease     time.Duration
	// MaxAttempts bounds how often an event whose payment is not known yet is
	// retried before it is parked for review.
	MaxAttempts int
	BackoffBase time.Duration
	BackoffCap  time.Duration
}

// Enabled reports whether a webhook secret is set, which turns webhook handling on.
func (c WebhookConfig) Enabled() bool { return c.Secret != "" }

// LoadWebhookConfig reads the webhook secret, body limit and processing settings from the environment.
func LoadWebhookConfig() (WebhookConfig, error) {
	if err := loadEnvFile(); err != nil {
		return WebhookConfig{}, fmt.Errorf("load .env: %w", err)
	}
	maxBody, err := envInt("WEBHOOK_MAX_BODY_BYTES", defaultWebhookMaxBody)
	if err != nil {
		return WebhookConfig{}, err
	}
	interval, err := envDuration("WEBHOOK_PROCESS_INTERVAL", defaultWebhookInterval)
	if err != nil {
		return WebhookConfig{}, err
	}
	batch, err := envInt("WEBHOOK_PROCESS_BATCH", defaultWebhookBatch)
	if err != nil {
		return WebhookConfig{}, err
	}
	workers, err := envInt("PAYMENT_WEBHOOK_WORKERS", defaultWebhookWorkers)
	if err != nil {
		return WebhookConfig{}, err
	}
	autoscale, err := envBool("PAYMENT_WEBHOOK_AUTOSCALE", false)
	if err != nil {
		return WebhookConfig{}, err
	}
	scale, err := loadWebhookScaleConfig()
	if err != nil {
		return WebhookConfig{}, err
	}
	lease, err := envDuration("WEBHOOK_LEASE", defaultWebhookLease)
	if err != nil {
		return WebhookConfig{}, err
	}
	attempts, err := envInt("WEBHOOK_MAX_ATTEMPTS", defaultWebhookMaxAttempts)
	if err != nil {
		return WebhookConfig{}, err
	}
	base, err := envDuration("WEBHOOK_BACKOFF_BASE", defaultWebhookBackoffBase)
	if err != nil {
		return WebhookConfig{}, err
	}
	limit, err := envDuration("WEBHOOK_BACKOFF_CAP", defaultWebhookBackoffCap)
	if err != nil {
		return WebhookConfig{}, err
	}
	result := WebhookConfig{
		Secret:       strings.TrimSpace(os.Getenv("RAZORPAY_WEBHOOK_SECRET")),
		MaxBodyBytes: int64(maxBody),
		Interval:     interval,
		Batch:        batch,
		Workers:      workers,
		Autoscale:    autoscale,
		Scale:        scale,
		Lease:        lease,
		MaxAttempts:  attempts,
		BackoffBase:  base,
		BackoffCap:   limit,
	}
	if err := result.Validate(); err != nil {
		return WebhookConfig{}, err
	}
	// Fail closed: with the Razorpay provider a missing secret must not quietly turn
	// webhooks off. Running on polling alone is allowed only when asked for by name.
	if result.Secret == "" && strings.EqualFold(strings.TrimSpace(os.Getenv("GATEWAY_PROVIDER")), "razorpay") &&
		!strings.EqualFold(strings.TrimSpace(os.Getenv("WEBHOOKS_DISABLED")), "true") {
		return WebhookConfig{}, errors.New("RAZORPAY_WEBHOOK_SECRET is required with GATEWAY_PROVIDER=razorpay; set WEBHOOKS_DISABLED=true to run on polling alone")
	}
	return result, nil
}

// Validate rejects zero or negative webhook settings.
func (c WebhookConfig) Validate() error {
	switch {
	case c.MaxBodyBytes <= 0:
		return errors.New("WEBHOOK_MAX_BODY_BYTES must be greater than zero")
	case c.Interval <= 0:
		return errors.New("WEBHOOK_PROCESS_INTERVAL must be greater than zero")
	case c.Batch <= 0:
		return errors.New("WEBHOOK_PROCESS_BATCH must be greater than zero")
	case c.Workers <= 0:
		return errors.New("PAYMENT_WEBHOOK_WORKERS must be greater than zero")
	case c.Lease <= 0:
		return errors.New("WEBHOOK_LEASE must be greater than zero")
	case c.MaxAttempts <= 0:
		return errors.New("WEBHOOK_MAX_ATTEMPTS must be greater than zero")
	case c.BackoffBase <= 0 || c.BackoffCap < c.BackoffBase:
		return errors.New("WEBHOOK_BACKOFF_BASE must be positive and not above WEBHOOK_BACKOFF_CAP")
	}
	if c.Autoscale {
		return c.validateAutoscale()
	}
	return nil
}

// validateAutoscale rejects scaling settings that could not work. It runs only when
// autoscaling is on, so an unused, malformed value never blocks a fixed-size start.
func (c WebhookConfig) validateAutoscale() error {
	s := c.Scale
	switch {
	case s.Min < 1:
		return errors.New("PAYMENT_WEBHOOK_WORKERS_MIN must be at least 1")
	case s.Max < s.Min:
		return errors.New("PAYMENT_WEBHOOK_WORKERS_MAX must not be below PAYMENT_WEBHOOK_WORKERS_MIN")
	case c.Workers < s.Min || c.Workers > s.Max:
		return errors.New("PAYMENT_WEBHOOK_WORKERS (the starting count) must be between PAYMENT_WEBHOOK_WORKERS_MIN and PAYMENT_WEBHOOK_WORKERS_MAX")
	case s.Interval <= 0:
		return errors.New("PAYMENT_WEBHOOK_SCALE_INTERVAL must be greater than zero")
	case s.UpAge <= 0 || s.DownAge < 0 || s.DownAge >= s.UpAge:
		return errors.New("PAYMENT_WEBHOOK_SCALE_UP_AGE must be positive and above PAYMENT_WEBHOOK_SCALE_DOWN_AGE")
	case s.UpMinBacklog < 1 || s.DownBacklog < 0 || s.DownBacklog >= s.UpMinBacklog:
		return errors.New("PAYMENT_WEBHOOK_SCALE_UP_MIN_BACKLOG must be at least 1 and above PAYMENT_WEBHOOK_SCALE_DOWN_BACKLOG")
	case s.UpSamples < 1 || s.DownSamples < 1:
		return errors.New("PAYMENT_WEBHOOK_SCALE_UP_SAMPLES and PAYMENT_WEBHOOK_SCALE_DOWN_SAMPLES must be at least 1")
	case s.UpCooldown < 0 || s.DownCooldown < 0:
		return errors.New("PAYMENT_WEBHOOK_SCALE_UP_COOLDOWN and PAYMENT_WEBHOOK_SCALE_DOWN_COOLDOWN must not be negative")
	case s.DownUtilization <= 0 || s.DownUtilization >= 1:
		return errors.New("PAYMENT_WEBHOOK_SCALE_DOWN_UTILIZATION must be between 0 and 1 (exclusive)")
	case s.PoolReserve < 0:
		return errors.New("PAYMENT_WEBHOOK_POOL_RESERVE must not be negative")
	}
	return nil
}

func loadWebhookScaleConfig() (WebhookScaleConfig, error) {
	var s WebhookScaleConfig
	var err error
	ints := []struct {
		name string
		def  int
		into *int
	}{
		{"PAYMENT_WEBHOOK_WORKERS_MIN", defaultScaleMinWorkers, &s.Min},
		{"PAYMENT_WEBHOOK_WORKERS_MAX", defaultScaleMaxWorkers, &s.Max},
		{"PAYMENT_WEBHOOK_SCALE_UP_SAMPLES", defaultScaleUpSamples, &s.UpSamples},
		{"PAYMENT_WEBHOOK_SCALE_DOWN_SAMPLES", defaultScaleDownSamples, &s.DownSamples},
		{"PAYMENT_WEBHOOK_POOL_RESERVE", defaultScalePoolReserve, &s.PoolReserve},
	}
	for _, in := range ints {
		if *in.into, err = envInt(in.name, in.def); err != nil {
			return s, err
		}
	}
	durations := []struct {
		name string
		def  time.Duration
		into *time.Duration
	}{
		{"PAYMENT_WEBHOOK_SCALE_INTERVAL", defaultScaleInterval, &s.Interval},
		{"PAYMENT_WEBHOOK_SCALE_UP_AGE", defaultScaleUpAge, &s.UpAge},
		{"PAYMENT_WEBHOOK_SCALE_UP_COOLDOWN", defaultScaleUpCooldown, &s.UpCooldown},
		{"PAYMENT_WEBHOOK_SCALE_DOWN_AGE", defaultScaleDownAge, &s.DownAge},
		{"PAYMENT_WEBHOOK_SCALE_DOWN_COOLDOWN", defaultScaleDownCooldown, &s.DownCooldown},
	}
	for _, in := range durations {
		if *in.into, err = envDuration(in.name, in.def); err != nil {
			return s, err
		}
	}
	backlog, err := envInt("PAYMENT_WEBHOOK_SCALE_UP_MIN_BACKLOG", defaultScaleUpMinBacklog)
	if err != nil {
		return s, err
	}
	s.UpMinBacklog = int64(backlog)
	down, err := envInt("PAYMENT_WEBHOOK_SCALE_DOWN_BACKLOG", defaultScaleDownBacklog)
	if err != nil {
		return s, err
	}
	s.DownBacklog = int64(down)
	if s.DownUtilization, err = envFloat("PAYMENT_WEBHOOK_SCALE_DOWN_UTILIZATION", defaultScaleDownUtil); err != nil {
		return s, err
	}
	return s, nil
}

// envBool reads a boolean environment variable, or returns fallback when it is unset.
func envBool(name string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false: %w", name, err)
	}
	return parsed, nil
}
