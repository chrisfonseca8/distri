package config

import (
	"errors"
	"fmt"
	"time"
)

// CompensationConfig tunes the refund worker. Every value is a policy choice, not a
// provider guarantee.
type CompensationConfig struct {
	Interval              time.Duration
	Batch                 int
	Lease                 time.Duration
	PendingPoll           time.Duration
	AbsenceWindow         time.Duration
	IdempotentRetryWindow time.Duration
	MaxIdempotentRetries  int
	UnknownMaxAge         time.Duration
	MaxFailedRetries      int
	RetryDelay            time.Duration
	// ReviewRetryAfter and MaxReviewRetries: a refund that went to REVIEW only because the
	// gateway kept rejecting it (nothing was created) is tried again, a bounded number of
	// times, instead of waiting forever for a person. Zero disables.
	ReviewRetryAfter time.Duration
	MaxReviewRetries int
}

// LoadCompensationConfig reads the refund worker settings from the environment.
func LoadCompensationConfig() (CompensationConfig, error) {
	if err := loadEnvFile(); err != nil {
		return CompensationConfig{}, fmt.Errorf("load .env: %w", err)
	}
	var (
		c   CompensationConfig
		err error
	)
	durations := []struct {
		name     string
		fallback time.Duration
		target   *time.Duration
	}{
		{"COMPENSATION_INTERVAL", 2 * time.Second, &c.Interval},
		{"COMPENSATION_LEASE", time.Minute, &c.Lease},
		{"COMPENSATION_PENDING_POLL", 5 * time.Second, &c.PendingPoll},
		{"COMPENSATION_ABSENCE_WINDOW", 2 * time.Minute, &c.AbsenceWindow},
		{"COMPENSATION_IDEMPOTENT_RETRY_WINDOW", 10 * time.Minute, &c.IdempotentRetryWindow},
		{"COMPENSATION_UNKNOWN_MAX_AGE", 30 * time.Minute, &c.UnknownMaxAge},
		{"COMPENSATION_RETRY_DELAY", 30 * time.Second, &c.RetryDelay},
		{"COMPENSATION_REVIEW_RETRY_AFTER", 30 * time.Minute, &c.ReviewRetryAfter},
	}
	for _, d := range durations {
		if *d.target, err = envDuration(d.name, d.fallback); err != nil {
			return CompensationConfig{}, err
		}
	}
	if c.Batch, err = envInt("COMPENSATION_BATCH", 10); err != nil {
		return CompensationConfig{}, err
	}
	if c.MaxIdempotentRetries, err = envInt("COMPENSATION_MAX_IDEMPOTENT_RETRIES", 2); err != nil {
		return CompensationConfig{}, err
	}
	if c.MaxFailedRetries, err = envInt("COMPENSATION_MAX_FAILED_RETRIES", 2); err != nil {
		return CompensationConfig{}, err
	}
	if c.MaxReviewRetries, err = envInt("COMPENSATION_MAX_REVIEW_RETRIES", 3); err != nil {
		return CompensationConfig{}, err
	}
	return c, c.Validate()
}

// Validate rejects zero or negative intervals and windows that are out of order.
func (c CompensationConfig) Validate() error {
	switch {
	case c.Interval <= 0 || c.Batch <= 0 || c.Lease <= 0 || c.PendingPoll <= 0 || c.RetryDelay <= 0:
		return errors.New("COMPENSATION_* intervals, batch, lease and delays must be greater than zero")
	case c.AbsenceWindow < 0 || c.IdempotentRetryWindow < c.AbsenceWindow || c.UnknownMaxAge < c.IdempotentRetryWindow:
		return errors.New("COMPENSATION windows must satisfy absence <= idempotent retry <= unknown max age")
	case c.MaxIdempotentRetries < 0 || c.MaxFailedRetries < 0 || c.MaxReviewRetries < 0 || c.ReviewRetryAfter < 0:
		return errors.New("COMPENSATION retry counts and delays must not be negative")
	}
	return nil
}
