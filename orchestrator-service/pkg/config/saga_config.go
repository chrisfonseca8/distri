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
	defaultStuckRefundAfter = 10 * time.Minute
	defaultUndoReviveAfter  = 10 * time.Minute
	defaultUndoMaxRevives   = 3
)

type SagaConfig struct {
	Deadline    time.Duration
	StepTimeout time.Duration

	SweepInterval    time.Duration
	SweepBatch       int
	StepMaxAttempts  int
	ResendBackoffCap time.Duration
	// StuckRefundAfter: a saga that stays STUCK on MARK_ORDER_PAID or CONFIRM_STOCK this
	// long (the customer has paid) is refunded and unwound instead of waiting forever for
	// a person. Zero disables the rule.
	StuckRefundAfter time.Duration
	// UndoReviveAfter / UndoMaxRevives: a saga STUCK on an undo step (cancel, release,
	// refund) is retried automatically after this long, at most UndoMaxRevives times, then
	// left for a person. Zero UndoReviveAfter disables the rule.
	UndoReviveAfter time.Duration
	UndoMaxRevives  int
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

	stuckRefundAfter, err := envDuration("SAGA_STUCK_REFUND_AFTER", defaultStuckRefundAfter)
	if err != nil {
		return SagaConfig{}, err
	}

	undoReviveAfter, err := envDuration("SAGA_UNDO_REVIVE_AFTER", defaultUndoReviveAfter)
	if err != nil {
		return SagaConfig{}, err
	}
	undoMaxRevives, err := envInt("SAGA_UNDO_MAX_REVIVES", defaultUndoMaxRevives)
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
		StuckRefundAfter: stuckRefundAfter,
		UndoReviveAfter:  undoReviveAfter,
		UndoMaxRevives:   undoMaxRevives,
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
	if c.StuckRefundAfter < 0 {
		return errors.New("SAGA_STUCK_REFUND_AFTER must not be negative")
	}
	if c.UndoReviveAfter < 0 || c.UndoMaxRevives < 0 {
		return errors.New("SAGA_UNDO_REVIVE_AFTER and SAGA_UNDO_MAX_REVIVES must not be negative")
	}
	if c.ResendBackoffCap < c.StepTimeout {
		return errors.New("SAGA_RESEND_BACKOFF_CAP must not be below SAGA_STEP_TIMEOUT")
	}
	return nil
}
