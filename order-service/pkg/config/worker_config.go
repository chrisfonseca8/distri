package config

import (
	"errors"
	"fmt"
	"time"
)

const (
	defaultWorkerConcurrency = 4
	defaultJobLease          = 30 * time.Second
	defaultJobHandlerTimeout = 20 * time.Second
	defaultJobPollInterval   = 200 * time.Millisecond
	defaultJobNotReadyDelay  = time.Second
	defaultJobRetryDelay     = 5 * time.Second
)

type WorkerConfig struct {
	Concurrency    int
	Lease          time.Duration
	HandlerTimeout time.Duration
	PollInterval   time.Duration
	NotReadyDelay  time.Duration
	RetryDelay     time.Duration
}

func LoadWorkerConfig() (WorkerConfig, error) {
	if err := loadEnvFile(); err != nil {
		return WorkerConfig{}, fmt.Errorf("load .env: %w", err)
	}

	concurrency, err := envInt("WORKER_CONCURRENCY", defaultWorkerConcurrency)
	if err != nil {
		return WorkerConfig{}, err
	}
	lease, err := envDuration("JOB_LEASE", defaultJobLease)
	if err != nil {
		return WorkerConfig{}, err
	}
	handlerTimeout, err := envDuration("JOB_HANDLER_TIMEOUT", defaultJobHandlerTimeout)
	if err != nil {
		return WorkerConfig{}, err
	}
	pollInterval, err := envDuration("JOB_POLL_INTERVAL", defaultJobPollInterval)
	if err != nil {
		return WorkerConfig{}, err
	}
	notReadyDelay, err := envDuration("JOB_NOT_READY_DELAY", defaultJobNotReadyDelay)
	if err != nil {
		return WorkerConfig{}, err
	}
	retryDelay, err := envDuration("JOB_RETRY_DELAY", defaultJobRetryDelay)
	if err != nil {
		return WorkerConfig{}, err
	}

	workerConfig := WorkerConfig{
		Concurrency:    concurrency,
		Lease:          lease,
		HandlerTimeout: handlerTimeout,
		PollInterval:   pollInterval,
		NotReadyDelay:  notReadyDelay,
		RetryDelay:     retryDelay,
	}
	if err := workerConfig.Validate(); err != nil {
		return WorkerConfig{}, err
	}
	return workerConfig, nil
}

func (c WorkerConfig) Validate() error {
	if c.Concurrency <= 0 {
		return errors.New("WORKER_CONCURRENCY must be greater than zero")
	}
	for name, value := range map[string]time.Duration{
		"JOB_LEASE":           c.Lease,
		"JOB_HANDLER_TIMEOUT": c.HandlerTimeout,
		"JOB_POLL_INTERVAL":   c.PollInterval,
		"JOB_NOT_READY_DELAY": c.NotReadyDelay,
		"JOB_RETRY_DELAY":     c.RetryDelay,
	} {
		if value <= 0 {
			return fmt.Errorf("%s must be greater than zero", name)
		}
	}
	if c.HandlerTimeout >= c.Lease {
		return errors.New("JOB_HANDLER_TIMEOUT must be less than JOB_LEASE")
	}
	return nil
}
