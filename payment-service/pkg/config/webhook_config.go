package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	defaultWebhookMaxBody     = 256 << 10
	defaultWebhookInterval    = time.Second
	defaultWebhookBatch       = 20
	defaultWebhookLease       = 30 * time.Second
	defaultWebhookMaxAttempts = 12
	defaultWebhookBackoffBase = time.Second
	defaultWebhookBackoffCap  = 2 * time.Minute
)

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
	Lease    time.Duration
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
		Lease:        lease,
		MaxAttempts:  attempts,
		BackoffBase:  base,
		BackoffCap:   limit,
	}
	return result, result.Validate()
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
	case c.Lease <= 0:
		return errors.New("WEBHOOK_LEASE must be greater than zero")
	case c.MaxAttempts <= 0:
		return errors.New("WEBHOOK_MAX_ATTEMPTS must be greater than zero")
	case c.BackoffBase <= 0 || c.BackoffCap < c.BackoffBase:
		return errors.New("WEBHOOK_BACKOFF_BASE must be positive and not above WEBHOOK_BACKOFF_CAP")
	}
	return nil
}
