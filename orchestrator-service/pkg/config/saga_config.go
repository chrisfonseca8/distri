package config

import (
	"errors"
	"fmt"
	"time"
)

const (
	defaultSagaDeadline = 15 * time.Minute
	defaultStepTimeout  = 30 * time.Second

	defaultSweepInterval    = 5 * time.Second
	defaultSweepBatch       = 50
	defaultStepMaxAttempts  = 5
	defaultResendBackoffCap = 5 * time.Minute
)

type SagaConfig struct {
	Deadline    time.Duration
	StepTimeout time.Duration

	SweepInterval    time.Duration
	SweepBatch       int
	StepMaxAttempts  int
	ResendBackoffCap time.Duration
}

// LoadSagaConfig reads the saga deadline, step timeout and retry settings from the environment.
func LoadSagaConfig() (SagaConfig, error) {
	if err := loadEnvFile(); err != nil {
		return SagaConfig{}, fmt.Errorf("load .env: %w", err)
	}

	deadline, err := envDuration("SAGA_DEADLINE", defaultSagaDeadline)
	if err != nil {
		return SagaConfig{}, err
	}
	stepTimeout, err := envDuration("SAGA_STEP_TIMEOUT", defaultStepTimeout)
	if err != nil {
		return SagaConfig{}, err
	}

	sweepInterval, err := envDuration("SAGA_SWEEP_INTERVAL", defaultSweepInterval)
	if err != nil {
		return SagaConfig{}, err
	}
	sweepBatch, err := envInt("SAGA_SWEEP_BATCH", defaultSweepBatch)
	if err != nil {
		return SagaConfig{}, err
	}
	stepMaxAttempts, err := envInt("SAGA_STEP_MAX_ATTEMPTS", defaultStepMaxAttempts)
	if err != nil {
		return SagaConfig{}, err
	}
	resendBackoffCap, err := envDuration("SAGA_RESEND_BACKOFF_CAP", defaultResendBackoffCap)
	if err != nil {
		return SagaConfig{}, err
	}

	result := SagaConfig{
		Deadline:         deadline,
		StepTimeout:      stepTimeout,
		SweepInterval:    sweepInterval,
		SweepBatch:       sweepBatch,
		StepMaxAttempts:  stepMaxAttempts,
		ResendBackoffCap: resendBackoffCap,
	}
	if err := result.Validate(); err != nil {
		return SagaConfig{}, err
	}
	return result, nil
}

// Validate rejects zero or negative saga settings.
func (c SagaConfig) Validate() error {
	if c.Deadline <= 0 {
		return errors.New("SAGA_DEADLINE must be greater than zero")
	}
	if c.StepTimeout <= 0 {
		return errors.New("SAGA_STEP_TIMEOUT must be greater than zero")
	}
	if c.StepTimeout >= c.Deadline {
		return errors.New("SAGA_STEP_TIMEOUT must be less than SAGA_DEADLINE")
	}
	if c.SweepInterval <= 0 {
		return errors.New("SAGA_SWEEP_INTERVAL must be greater than zero")
	}
	if c.SweepBatch <= 0 {
		return errors.New("SAGA_SWEEP_BATCH must be greater than zero")
	}
	if c.StepMaxAttempts <= 0 {
		return errors.New("SAGA_STEP_MAX_ATTEMPTS must be greater than zero")
	}
	if c.ResendBackoffCap < c.StepTimeout {
		return errors.New("SAGA_RESEND_BACKOFF_CAP must not be below SAGA_STEP_TIMEOUT")
	}
	return nil
}
