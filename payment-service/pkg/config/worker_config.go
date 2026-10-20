package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	defaultWorkerConcurrency = 4
	defaultJobLease          = 30 * time.Second
	defaultJobHandlerTimeout = 20 * time.Second
	defaultJobPollInterval   = 200 * time.Millisecond
	defaultJobNotReadyDelay  = time.Second
	defaultJobRetryBase      = 2 * time.Second
	defaultJobRetryCap       = 60 * time.Second
	defaultJobRetryJitter    = 0.25
)

type WorkerConfig struct {
	Concurrency    int
	Lease          time.Duration
	HandlerTimeout time.Duration
	PollInterval   time.Duration
	NotReadyDelay  time.Duration
	RetryBase      time.Duration
	RetryCap       time.Duration
	RetryJitter    float64
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
	retryBase, err := envDuration("JOB_RETRY_BASE", defaultJobRetryBase)
	if err != nil {
		return WorkerConfig{}, err
	}
	retryCap, err := envDuration("JOB_RETRY_CAP", defaultJobRetryCap)
	if err != nil {
		return WorkerConfig{}, err
	}
	retryJitter, err := envFloat("JOB_RETRY_JITTER", defaultJobRetryJitter)
	if err != nil {
		return WorkerConfig{}, err
	}

	workerConfig := WorkerConfig{
		Concurrency:    concurrency,
		Lease:          lease,
		HandlerTimeout: handlerTimeout,
		PollInterval:   pollInterval,
		NotReadyDelay:  notReadyDelay,
		RetryBase:      retryBase,
		RetryCap:       retryCap,
		RetryJitter:    retryJitter,
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
		"JOB_RETRY_BASE":      c.RetryBase,
		"JOB_RETRY_CAP":       c.RetryCap,
	} {
		if value <= 0 {
			return fmt.Errorf("%s must be greater than zero", name)
		}
	}
	if c.HandlerTimeout >= c.Lease {
		return errors.New("JOB_HANDLER_TIMEOUT must be less than JOB_LEASE")
	}
	if c.RetryBase > c.RetryCap {
		return errors.New("JOB_RETRY_BASE must not exceed JOB_RETRY_CAP")
	}
	if c.RetryJitter < 0 || c.RetryJitter >= 1 {
		return errors.New("JOB_RETRY_JITTER must be at least 0 and less than 1")
	}
	return nil
}

func envFloat(name string, fallback float64) (float64, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number: %w", name, err)
	}
	return parsed, nil
}
