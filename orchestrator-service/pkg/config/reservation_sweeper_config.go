package config

import (
	"errors"
	"fmt"
	"time"
)

const (
	defaultReservationSweepTTL       = 10 * time.Minute
	defaultReservationSweepInterval  = time.Minute
	defaultReservationSweepBatchSize = 100
)

type ReservationSweeperConfig struct {
	ReservationTTL time.Duration
	Interval       time.Duration
	BatchSize      int
}

func LoadReservationSweeperConfig() (ReservationSweeperConfig, error) {
	if err := loadEnvFile(); err != nil {
		return ReservationSweeperConfig{}, fmt.Errorf("load .env: %w", err)
	}

	ttl, err := envDuration("RESERVATION_SWEEP_TTL", defaultReservationSweepTTL)
	if err != nil {
		return ReservationSweeperConfig{}, err
	}
	interval, err := envDuration("RESERVATION_SWEEP_INTERVAL", defaultReservationSweepInterval)
	if err != nil {
		return ReservationSweeperConfig{}, err
	}
	batchSize, err := envInt("RESERVATION_SWEEP_BATCH_SIZE", defaultReservationSweepBatchSize)
	if err != nil {
		return ReservationSweeperConfig{}, err
	}

	result := ReservationSweeperConfig{ReservationTTL: ttl, Interval: interval, BatchSize: batchSize}
	if err := result.Validate(); err != nil {
		return ReservationSweeperConfig{}, err
	}
	return result, nil
}

func (c ReservationSweeperConfig) Validate() error {
	if c.ReservationTTL <= 0 {
		return errors.New("RESERVATION_SWEEP_TTL must be greater than zero")
	}
	if c.Interval <= 0 {
		return errors.New("RESERVATION_SWEEP_INTERVAL must be greater than zero")
	}
	if c.BatchSize <= 0 || c.BatchSize > 1000 {
		return errors.New("RESERVATION_SWEEP_BATCH_SIZE must be between 1 and 1000")
	}
	return nil
}
