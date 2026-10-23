package config

import (
	"errors"
	"fmt"
	"time"
)

const (
	defaultSagaDeadline = 15 * time.Minute
	defaultStepTimeout  = 30 * time.Second
)

type SagaConfig struct {
	Deadline    time.Duration
	StepTimeout time.Duration
}

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

	result := SagaConfig{Deadline: deadline, StepTimeout: stepTimeout}
	if err := result.Validate(); err != nil {
		return SagaConfig{}, err
	}
	return result, nil
}

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
	return nil
}
