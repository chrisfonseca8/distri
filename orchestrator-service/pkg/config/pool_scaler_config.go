package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// HardMaxPoolConnections is the largest pool the autoscaler may ever set per
// process, whatever the environment says.
const HardMaxPoolConnections = 50

const (
	defaultPoolMax            = HardMaxPoolConnections
	defaultPoolStep           = 5
	defaultPoolInterval       = 5 * time.Second
	defaultPoolUpWait         = 5 * time.Millisecond
	defaultPoolUpMinWaits     = 10
	defaultPoolUpNearFull     = 0.8
	defaultPoolUpSamples      = 2
	defaultPoolUpCooldown     = 30 * time.Second
	defaultPoolDownUsage      = 0.3
	defaultPoolDownSamples    = 24
	defaultPoolDownCooldown   = 60 * time.Second
	defaultPoolReplicas       = 1
	defaultGuardLatencyFactor = 3.0
	defaultGuardLatencyMax    = 100 * time.Millisecond
	defaultGuardLockWaiters   = 5
	defaultGuardConnErrors    = 3
)

// PoolScalerConfig tunes the optional database/sql pool autoscaler
// (ORCHESTRATOR_DB_POOL_*). The pool starts at DB_MAX_OPEN_CONNS and never goes below it.
type PoolScalerConfig struct {
	Enabled bool
	// Max is the per-process ceiling asked for; the effective ceiling is also
	// bounded by HardMaxPoolConnections and by the database connection budget.
	Max      int
	Step     int
	Interval time.Duration

	// Scale up when, for UpSamples consecutive samples, the average wait per blocked
	// acquisition is at least UpWait with at least UpMinWaits blocked acquisitions in
	// the interval, and connections in use are at least UpNearFull of the cap.
	UpWait     time.Duration
	UpMinWaits int64
	UpNearFull float64
	UpSamples  int
	UpCooldown time.Duration

	// Scale down when connections in use stay at or below DownUsage of the cap with
	// no blocked acquisitions for DownSamples consecutive samples.
	DownUsage    float64
	DownSamples  int
	DownCooldown time.Duration

	// Budget is the number of connections the whole database may hold for this
	// service's clients (all replicas plus other clients). BudgetReserved is what
	// other clients are known to use; Replicas is how many processes share the rest.
	Budget         int
	BudgetReserved int
	Replicas       int

	// Increases are paused while the database looks stressed.
	GuardLatencyFactor float64
	GuardLatencyMax    time.Duration
	GuardLockWaiters   int64
	GuardConnErrors    int64
}

// Ceiling is the largest pool this process may use: the hard limit, the configured
// maximum, and this process's share of the database connection budget.
func (c PoolScalerConfig) Ceiling() int {
	ceiling := HardMaxPoolConnections
	if c.Max > 0 && c.Max < ceiling {
		ceiling = c.Max
	}
	if c.Budget > 0 {
		replicas := c.Replicas
		if replicas < 1 {
			replicas = 1
		}
		share := (c.Budget - c.BudgetReserved) / replicas
		if share < ceiling {
			ceiling = share
		}
	}
	if ceiling < 0 {
		ceiling = 0
	}
	return ceiling
}

// LoadPoolScalerConfig reads the autoscaler settings from the environment. With
// autoscaling off (the default) only the on/off switch is read.
func LoadPoolScalerConfig() (PoolScalerConfig, error) {
	if err := loadEnvFile(); err != nil {
		return PoolScalerConfig{}, fmt.Errorf("load .env: %w", err)
	}
	enabled, err := envBool("ORCHESTRATOR_DB_POOL_AUTOSCALE", false)
	if err != nil {
		return PoolScalerConfig{}, err
	}
	c := PoolScalerConfig{Enabled: enabled}
	if !enabled {
		return c, nil
	}
	var intErr error
	readInt := func(name string, def int) int {
		v, err := envInt(name, def)
		if err != nil && intErr == nil {
			intErr = err
		}
		return v
	}
	readDur := func(name string, def time.Duration) time.Duration {
		v, err := envDuration(name, def)
		if err != nil && intErr == nil {
			intErr = err
		}
		return v
	}
	readFloat := func(name string, def float64) float64 {
		v, err := envFloat(name, def)
		if err != nil && intErr == nil {
			intErr = err
		}
		return v
	}
	c.Max = readInt("ORCHESTRATOR_DB_POOL_MAX", defaultPoolMax)
	c.Step = readInt("ORCHESTRATOR_DB_POOL_STEP", defaultPoolStep)
	c.Interval = readDur("ORCHESTRATOR_DB_POOL_INTERVAL", defaultPoolInterval)
	c.UpWait = readDur("ORCHESTRATOR_DB_POOL_UP_WAIT", defaultPoolUpWait)
	c.UpMinWaits = int64(readInt("ORCHESTRATOR_DB_POOL_UP_MIN_WAITS", defaultPoolUpMinWaits))
	c.UpNearFull = readFloat("ORCHESTRATOR_DB_POOL_UP_NEAR_FULL", defaultPoolUpNearFull)
	c.UpSamples = readInt("ORCHESTRATOR_DB_POOL_UP_SAMPLES", defaultPoolUpSamples)
	c.UpCooldown = readDur("ORCHESTRATOR_DB_POOL_UP_COOLDOWN", defaultPoolUpCooldown)
	c.DownUsage = readFloat("ORCHESTRATOR_DB_POOL_DOWN_USAGE", defaultPoolDownUsage)
	c.DownSamples = readInt("ORCHESTRATOR_DB_POOL_DOWN_SAMPLES", defaultPoolDownSamples)
	c.DownCooldown = readDur("ORCHESTRATOR_DB_POOL_DOWN_COOLDOWN", defaultPoolDownCooldown)
	c.Budget = readInt("ORCHESTRATOR_DB_CONNECTION_BUDGET", 0)
	c.BudgetReserved = readInt("ORCHESTRATOR_DB_CONNECTION_BUDGET_RESERVED", 0)
	c.Replicas = readInt("ORCHESTRATOR_REPLICAS", defaultPoolReplicas)
	c.GuardLatencyFactor = readFloat("ORCHESTRATOR_DB_POOL_GUARD_LATENCY_FACTOR", defaultGuardLatencyFactor)
	c.GuardLatencyMax = readDur("ORCHESTRATOR_DB_POOL_GUARD_LATENCY_MAX", defaultGuardLatencyMax)
	c.GuardLockWaiters = int64(readInt("ORCHESTRATOR_DB_POOL_GUARD_LOCK_WAITERS", defaultGuardLockWaiters))
	c.GuardConnErrors = int64(readInt("ORCHESTRATOR_DB_POOL_GUARD_CONN_ERRORS", defaultGuardConnErrors))
	if intErr != nil {
		return PoolScalerConfig{}, intErr
	}
	return c, c.Validate()
}

// Validate rejects settings that could not work or that would exceed the budget.
func (c PoolScalerConfig) Validate() error {
	switch {
	case c.Max < 1 || c.Max > HardMaxPoolConnections:
		return fmt.Errorf("ORCHESTRATOR_DB_POOL_MAX must be between 1 and %d", HardMaxPoolConnections)
	case c.Step < 1:
		return errors.New("ORCHESTRATOR_DB_POOL_STEP must be at least 1")
	case c.Interval <= 0:
		return errors.New("ORCHESTRATOR_DB_POOL_INTERVAL must be greater than zero")
	case c.UpWait <= 0 || c.UpMinWaits < 1:
		return errors.New("ORCHESTRATOR_DB_POOL_UP_WAIT and ORCHESTRATOR_DB_POOL_UP_MIN_WAITS must be positive")
	case c.UpNearFull <= 0 || c.UpNearFull > 1:
		return errors.New("ORCHESTRATOR_DB_POOL_UP_NEAR_FULL must be above 0 and at most 1")
	case c.UpSamples < 1 || c.DownSamples < 1:
		return errors.New("ORCHESTRATOR_DB_POOL_UP_SAMPLES and ORCHESTRATOR_DB_POOL_DOWN_SAMPLES must be at least 1")
	case c.UpCooldown < 0 || c.DownCooldown < 0:
		return errors.New("ORCHESTRATOR_DB_POOL_UP_COOLDOWN and ORCHESTRATOR_DB_POOL_DOWN_COOLDOWN must not be negative")
	case c.DownUsage <= 0 || c.DownUsage >= c.UpNearFull:
		return errors.New("ORCHESTRATOR_DB_POOL_DOWN_USAGE must be above 0 and below ORCHESTRATOR_DB_POOL_UP_NEAR_FULL")
	case c.Budget < 1:
		return errors.New("ORCHESTRATOR_DB_CONNECTION_BUDGET is required with ORCHESTRATOR_DB_POOL_AUTOSCALE=true: the number of connections the database may give this service across all replicas and clients")
	case c.BudgetReserved < 0 || c.BudgetReserved >= c.Budget:
		return errors.New("ORCHESTRATOR_DB_CONNECTION_BUDGET_RESERVED must be at least 0 and below ORCHESTRATOR_DB_CONNECTION_BUDGET")
	case c.Replicas < 1:
		return errors.New("ORCHESTRATOR_REPLICAS must be at least 1")
	case c.GuardLatencyFactor <= 1 || c.GuardLatencyMax <= 0:
		return errors.New("ORCHESTRATOR_DB_POOL_GUARD_LATENCY_FACTOR must be above 1 and ORCHESTRATOR_DB_POOL_GUARD_LATENCY_MAX positive")
	case c.GuardLockWaiters < 1 || c.GuardConnErrors < 1:
		return errors.New("ORCHESTRATOR_DB_POOL_GUARD_LOCK_WAITERS and ORCHESTRATOR_DB_POOL_GUARD_CONN_ERRORS must be at least 1")
	}
	return nil
}

func envBool(name string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false: %w", name, err)
	}
	return parsed, nil
}

func envFloat(name string, fallback float64) (float64, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number: %w", name, err)
	}
	return parsed, nil
}
