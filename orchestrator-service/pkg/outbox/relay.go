package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/orchestrator-service/database/models"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/telemetry"
)

const finishTimeout = 5 * time.Second

type relayStore interface {
	Claim(context.Context, string, int, time.Duration) ([]models.Outbox, error)
	MarkPublished(context.Context, uuid.UUID, string) (bool, error)
	Reschedule(context.Context, uuid.UUID, string, time.Duration, string) (bool, error)
}

type publisher interface {
	Publish(context.Context, uuid.UUID, string, []byte) error
}

type RelayConfig struct {
	BatchSize      int
	PollInterval   time.Duration
	Lease          time.Duration
	PublishTimeout time.Duration
	RetryBase      time.Duration
	RetryCap       time.Duration
	// AlertAttempts: a row that has failed to publish this many times is logged at ERROR
	// on every further failure, so a poison or long-blocked row is visible instead of
	// retrying silently forever. The row itself is never dropped. Zero disables the alert.
	AlertAttempts int
}

// DefaultRelayConfig returns the standard batch size, poll interval, lease and retry settings.
func DefaultRelayConfig() RelayConfig {
	return RelayConfig{
		BatchSize:      10,
		PollInterval:   500 * time.Millisecond,
		Lease:          2 * time.Minute,
		PublishTimeout: 10 * time.Second,
		RetryBase:      time.Second,
		RetryCap:       30 * time.Second,
		AlertAttempts:  20,
	}
}

// Validate rejects zero or negative relay settings.
func (config RelayConfig) Validate() error {
	if config.BatchSize <= 0 {
		return errors.New("outbox batch size must be greater than zero")
	}
	if config.PollInterval <= 0 {
		return errors.New("outbox poll interval must be greater than zero")
	}
	if config.PublishTimeout <= 0 {
		return errors.New("outbox publish timeout must be greater than zero")
	}
	if config.Lease <= time.Duration(config.BatchSize)*config.PublishTimeout {
		return errors.New("outbox lease must exceed the worst-case batch publish time")
	}
	if config.RetryBase <= 0 || config.RetryCap < config.RetryBase {
		return errors.New("outbox retry durations are invalid")
	}
	if config.AlertAttempts < 0 {
		return errors.New("outbox alert attempts must not be negative")
	}
	return nil
}

type Relay struct {
	store     relayStore
	publisher publisher
	config    RelayConfig
	workerID  string
	telemetry *telemetry.Recorder
}

type RelayOption func(*Relay)

// WithTelemetry records load-test timing around claims and confirmed publishes.
func WithTelemetry(recorder *telemetry.Recorder) RelayOption {
	return func(relay *Relay) { relay.telemetry = recorder }
}

// NewRelay creates a relay that publishes rows from store through messagePublisher.
func NewRelay(store relayStore, messagePublisher publisher, config RelayConfig, options ...RelayOption) (*Relay, error) {
	if store == nil {
		return nil, errors.New("outbox store is required")
	}
	if messagePublisher == nil {
		return nil, errors.New("outbox publisher is required")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}

	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("read hostname: %w", err)
	}
	relay := &Relay{
		store:     store,
		publisher: messagePublisher,
		config:    config,
		workerID:  fmt.Sprintf("%s-%d-%s", hostname, os.Getpid(), uuid.New().String()[:8]),
	}
	for _, option := range options {
		option(relay)
	}
	return relay, nil
}

// Run claims and publishes outbox rows in a loop until ctx is cancelled.
func (r *Relay) Run(ctx context.Context) {
	slog.Info("outbox relay started", "worker_id", r.workerID)
	defer slog.Info("outbox relay stopped", "worker_id", r.workerID)

	for ctx.Err() == nil {
		claimStartedAt := time.Now()
		rows, err := r.store.Claim(ctx, r.workerID, r.config.BatchSize, r.config.Lease)
		claimFinishedAt := time.Now()
		r.telemetry.Observe("outbox_claim_query", claimFinishedAt.Sub(claimStartedAt))
		if err != nil {
			r.telemetry.Increment("outbox_claim_failed")
			if ctx.Err() != nil {
				return
			}
			slog.Error("claim outbox messages", "error", err)
			if !wait(ctx, r.config.PollInterval) {
				return
			}
			continue
		}

		if len(rows) == 0 {
			if !wait(ctx, r.config.PollInterval) {
				return
			}
			continue
		}
		r.telemetry.Increment("outbox_claim_nonempty")

		for index, row := range rows {
			if ctx.Err() != nil {
				r.releaseClaimed(rows[index:])
				return
			}
			if !row.CreatedAt.IsZero() {
				r.telemetry.Observe("outbox_row_age_at_claim", claimFinishedAt.Sub(row.CreatedAt))
			}
			r.publishClaimed(ctx, row, claimFinishedAt)
		}
	}
}

// releaseClaimed hands unpublished rows back so another pass can pick them up.
func (r *Relay) releaseClaimed(rows []models.Outbox) {
	finishContext, cancelFinish := context.WithTimeout(context.Background(), finishTimeout)
	defer cancelFinish()

	for _, row := range rows {
		updated, err := r.store.Reschedule(
			finishContext,
			row.ID,
			r.workerID,
			0,
			"relay stopped before publishing claimed message",
		)
		if err != nil {
			slog.Error("release claimed outbox message", "message_id", row.MessageID, "error", err)
			continue
		}
		if !updated {
			slog.Warn("outbox lease lost before shutdown release", "message_id", row.MessageID)
		}
	}
}

// publish sends one row and marks it published, or reschedules it with a delay if sending fails.
func (r *Relay) publish(ctx context.Context, row models.Outbox) {
	r.publishClaimed(ctx, row, time.Now())
}

func (r *Relay) publishClaimed(ctx context.Context, row models.Outbox, claimedAt time.Time) {
	publishStartedAt := time.Now()
	r.telemetry.Observe("outbox_claim_to_publish_started", publishStartedAt.Sub(claimedAt))
	publishContext, cancelPublish := context.WithTimeout(ctx, r.config.PublishTimeout)
	err := r.publisher.Publish(
		publishContext,
		row.MessageID,
		row.RoutingKey,
		[]byte(row.Payload),
	)
	cancelPublish()
	publishFinishedAt := time.Now()
	r.telemetry.Observe("outbox_publish_to_broker_confirmation", publishFinishedAt.Sub(publishStartedAt))

	finishContext, cancelFinish := context.WithTimeout(context.Background(), finishTimeout)
	defer cancelFinish()

	if err == nil {
		updated, updateErr := r.store.MarkPublished(
			finishContext,
			row.ID,
			r.workerID,
		)
		markedAt := time.Now()
		r.telemetry.Observe("outbox_confirmation_to_published_mark", markedAt.Sub(publishFinishedAt))
		r.telemetry.Observe("outbox_claim_to_published_mark", markedAt.Sub(claimedAt))
		if updateErr != nil {
			r.telemetry.Increment("outbox_mark_published_failed")
			slog.Error("mark outbox message published", "message_id", row.MessageID, "error", updateErr)
			return
		}
		if !updated {
			slog.Warn("outbox lease lost after publish", "message_id", row.MessageID)
		}
		r.telemetry.Increment("outbox_publish_confirmed")
		return
	}
	r.telemetry.Increment("outbox_publish_failed")

	delay := retryDelay(row.Attempts, r.config.RetryBase, r.config.RetryCap)
	updated, updateErr := r.store.Reschedule(
		finishContext,
		row.ID,
		r.workerID,
		delay,
		err.Error(),
	)
	if updateErr != nil {
		slog.Error("reschedule outbox message", "message_id", row.MessageID, "error", updateErr)
		return
	}
	if !updated {
		slog.Warn("outbox lease lost after publish failure", "message_id", row.MessageID)
		return
	}
	if r.config.AlertAttempts > 0 && row.Attempts >= r.config.AlertAttempts {
		slog.Error(
			"outbox message is not publishing: needs attention",
			"message_id", row.MessageID,
			"routing_key", row.RoutingKey,
			"attempts", row.Attempts,
			"error", err,
		)
	}
	slog.Warn(
		"outbox publish failed",
		"message_id", row.MessageID,
		"attempt", row.Attempts,
		"retry_in", delay,
		"error", err,
	)
}

// retryDelay returns the wait before the next attempt, growing from base up to cap.
func retryDelay(attempt int, base time.Duration, cap time.Duration) time.Duration {
	if attempt <= 1 {
		return base
	}
	delay := base
	for current := 1; current < attempt; current++ {
		if delay >= cap/2 {
			return cap
		}
		delay *= 2
	}
	if delay > cap {
		return cap
	}
	return delay
}

// wait sleeps for duration and returns false if ctx is cancelled first.
func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
