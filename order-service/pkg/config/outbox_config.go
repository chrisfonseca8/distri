package config

import (
	"errors"
	"time"
)

const defaultOutboxConcurrency = 4
const defaultOutboxPollInterval = 100 * time.Millisecond

// LoadOutboxConcurrency returns the number of independent outbox relay workers.
func LoadOutboxConcurrency() (int, error) {
	concurrency, err := envInt("OUTBOX_CONCURRENCY", defaultOutboxConcurrency)
	if err != nil {
		return 0, err
	}
	if concurrency <= 0 {
		return 0, errors.New("OUTBOX_CONCURRENCY must be greater than zero")
	}
	return concurrency, nil
}

// LoadOutboxPollInterval returns how long an idle relay waits before claiming again.
func LoadOutboxPollInterval() (time.Duration, error) {
	interval, err := envDuration("OUTBOX_POLL_INTERVAL", defaultOutboxPollInterval)
	if err != nil {
		return 0, err
	}
	if interval <= 0 {
		return 0, errors.New("OUTBOX_POLL_INTERVAL must be greater than zero")
	}
	return interval, nil
}
