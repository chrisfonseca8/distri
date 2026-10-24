package config

import (
	"errors"
	"fmt"
	"time"
)

const defaultPaymentResolveInterval = 30 * time.Second

type ResolverConfig struct {
	Interval time.Duration
}

// LoadResolverConfig reads the dead-job resolver interval from the environment.
func LoadResolverConfig() (ResolverConfig, error) {
	if err := loadEnvFile(); err != nil {
		return ResolverConfig{}, fmt.Errorf("load .env: %w", err)
	}
	interval, err := envDuration("PAYMENT_RESOLVE_INTERVAL", defaultPaymentResolveInterval)
	if err != nil {
		return ResolverConfig{}, err
	}
	if interval <= 0 {
		return ResolverConfig{}, errors.New("PAYMENT_RESOLVE_INTERVAL must be greater than zero")
	}
	return ResolverConfig{Interval: interval}, nil
}
