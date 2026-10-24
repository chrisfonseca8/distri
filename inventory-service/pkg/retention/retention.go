// Package retention deletes rows that exist only to make messaging safe and have outlived
// every window in which they can matter.
//
// Published outbox rows, processed-message markers, finished jobs and processed webhook
// events are bookkeeping. They are kept long enough to cover every redelivery, resend and
// replay window (days, against minutes of retry), then purged in small batches so the
// tables stop growing without bound. Business records (orders, reservations, payment
// attempts, compensation records) are NOT touched: how long those must be kept is a
// business and audit decision, not an operational one.
package retention

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"gorm.io/gorm"
)

// Config sets how often the purge runs and how old each kind of row must be.
type Config struct {
	Interval time.Duration
	Batch    int
	// Messages applies to outbox, processed_messages, finished jobs and processed events.
	Messages time.Duration
	// Parked applies to webhook events that were parked and never resolved.
	Parked time.Duration
	// Checkouts applies to completed idempotency records and finished sagas.
	Checkouts time.Duration
}

// LoadConfig reads the retention settings from the environment.
func LoadConfig() (Config, error) {
	config := Config{
		Interval:  time.Hour,
		Batch:     1000,
		Messages:  14 * 24 * time.Hour,
		Parked:    30 * 24 * time.Hour,
		Checkouts: 30 * 24 * time.Hour,
	}
	for name, target := range map[string]*time.Duration{
		"RETENTION_INTERVAL":  &config.Interval,
		"RETENTION_MESSAGES":  &config.Messages,
		"RETENTION_PARKED":    &config.Parked,
		"RETENTION_CHECKOUTS": &config.Checkouts,
	} {
		if value := os.Getenv(name); value != "" {
			parsed, err := time.ParseDuration(value)
			if err != nil {
				return Config{}, fmt.Errorf("%s must be a duration: %w", name, err)
			}
			*target = parsed
		}
	}
	if value := os.Getenv("RETENTION_BATCH"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return Config{}, fmt.Errorf("RETENTION_BATCH must be a number: %w", err)
		}
		config.Batch = parsed
	}
	if config.Interval <= 0 || config.Batch <= 0 || config.Messages <= 0 || config.Parked <= 0 || config.Checkouts <= 0 {
		return Config{}, fmt.Errorf("retention interval, batch and ages must be positive")
	}
	return config, nil
}

// Rule deletes one kind of expired row. SQL takes two parameters: the cutoff time and
// the batch size, and must delete at most that many rows.
type Rule struct {
	Name string
	Age  func(Config) time.Duration
	SQL  string
}

// Purger runs a service's rules on an interval.
type Purger struct {
	db     *gorm.DB
	config Config
	rules  []Rule
	now    func() time.Time
}

// New creates a purger for the rules of one service.
func New(db *gorm.DB, config Config, rules []Rule) *Purger {
	return &Purger{db: db, config: config, rules: rules, now: time.Now}
}

// Run purges on every interval until ctx is cancelled.
func (p *Purger) Run(ctx context.Context) {
	slog.Info("retention purge started", "interval", p.config.Interval, "messages", p.config.Messages)
	defer slog.Info("retention purge stopped")
	ticker := time.NewTicker(p.config.Interval)
	defer ticker.Stop()
	for {
		if deleted := p.RunOnce(ctx); len(deleted) > 0 {
			slog.Info("retention purge deleted rows", "rows", deleted)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunOnce applies every rule until it has nothing left to delete (batch by batch, so no
// statement is large) and reports how many rows each rule removed.
func (p *Purger) RunOnce(ctx context.Context) map[string]int64 {
	deleted := map[string]int64{}
	for _, rule := range p.rules {
		cutoff := p.now().Add(-rule.Age(p.config))
		for ctx.Err() == nil {
			result := p.db.WithContext(ctx).Exec(rule.SQL, cutoff, p.config.Batch)
			if result.Error != nil {
				slog.Error("retention purge failed", "rule", rule.Name, "error", result.Error)
				break
			}
			deleted[rule.Name] += result.RowsAffected
			if result.RowsAffected < int64(p.config.Batch) {
				break
			}
		}
	}
	for name, count := range deleted {
		if count == 0 {
			delete(deleted, name)
		}
	}
	return deleted
}
