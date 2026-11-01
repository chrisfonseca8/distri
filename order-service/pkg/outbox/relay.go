package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/order-service/database/models"
	"github.com/chrisfonseca08/distri/order-service/pkg/messaging"
	"github.com/chrisfonseca08/distri/order-service/pkg/observability"
	"go.opentelemetry.io/otel/trace"
)

const finishTimeout = 5 * time.Second

type relayStore interface {
	Claim(context.Context, string, int, time.Duration) ([]models.Outbox, error)
	MarkPublished(context.Context, uuid.UUID, string) (bool, error)
	Reschedule(context.Context, uuid.UUID, string, time.Duration, string) (bool, error)
	RecordUnpublishable(context.Context, uuid.UUID, string, int, time.Duration, string) (bool, bool, error)
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
	// AlertAttempts is the number of permanently unroutable failures before a row is
	// marked dead. Transient broker failures never count. Zero disables dead-lettering.
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
}

// NewRelay creates a relay that publishes rows from store through messagePublisher.
func NewRelay(store relayStore, messagePublisher publisher, config RelayConfig) (*Relay, error) {
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
	return &Relay{
		store:     store,
		publisher: messagePublisher,
		config:    config,
		workerID:  fmt.Sprintf("%s-%d-%s", hostname, os.Getpid(), uuid.New().String()[:8]),
	}, nil
}

// Run claims and publishes outbox rows in a loop until ctx is cancelled.
func (r *Relay) Run(ctx context.Context) {
	slog.Info("outbox relay started", "worker_id", r.workerID)
	defer slog.Info("outbox relay stopped", "worker_id", r.workerID)

	for ctx.Err() == nil {
		rows, err := r.store.Claim(ctx, r.workerID, r.config.BatchSize, r.config.Lease)
		if err != nil {
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

		for index, row := range rows {
			if ctx.Err() != nil {
				r.releaseClaimed(rows[index:])
				return
			}
			r.publish(ctx, row)
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
	// Resume the correlation and trace stored with the row, then join the trace.
	var stored observability.CorrelationJSON
	_ = json.Unmarshal(row.Correlation, &stored)
	ctx = observability.ContextFromCorrelation(ctx, stored)
	ctx = observability.WithMessageID(ctx, row.MessageID.String())
	ctx, span := observability.StartSpan(ctx, "outbox.publish "+row.RoutingKey, trace.SpanKindInternal)
	var publishErr error
	defer func() { observability.EndSpan(span, publishErr) }()

	publishContext, cancelPublish := context.WithTimeout(ctx, r.config.PublishTimeout)
	err := r.publisher.Publish(
		publishContext,
		row.MessageID,
		row.RoutingKey,
		[]byte(row.Payload),
	)
	cancelPublish()
	publishErr = err

	finishContext, cancelFinish := context.WithTimeout(context.Background(), finishTimeout)
	defer cancelFinish()

	if err == nil {
		updated, updateErr := r.store.MarkPublished(
			finishContext,
			row.ID,
			r.workerID,
		)
		if updateErr != nil {
			slog.Error("mark outbox message published", "message_id", row.MessageID, "error", updateErr)
			return
		}
		if !updated {
			observability.OutboxPublish.WithLabelValues("lease_lost").Inc()
			slog.WarnContext(ctx, "outbox lease lost after publish", "message_id", row.MessageID)
			return
		}
		observability.OutboxPublish.WithLabelValues("published").Inc()
		observability.OutboxLatency.WithLabelValues(row.RoutingKey).Observe(time.Since(row.CreatedAt).Seconds())
		return
	}

	delay := retryDelay(row.Attempts, r.config.RetryBase, r.config.RetryCap)
	if errors.Is(err, messaging.ErrUnroutable) && r.config.AlertAttempts > 0 {
		updated, dead, updateErr := r.store.RecordUnpublishable(finishContext, row.ID, r.workerID, r.config.AlertAttempts, delay, err.Error())
		if updateErr != nil {
			slog.Error("record unpublishable outbox message", "message_id", row.MessageID, "error", updateErr)
			return
		}
		if !updated {
			slog.Warn("outbox lease lost after unpublishable failure", "message_id", row.MessageID)
			return
		}
		if dead {
			observability.OutboxPublish.WithLabelValues("dead").Inc()
			slog.ErrorContext(ctx, "outbox message marked dead", "event", observability.EventOutboxMessageMarkedDead, "message_id", row.MessageID, "routing_key", row.RoutingKey, "unpublishable_attempts", row.UnpublishableAttempts+1, "error", err)
			return
		}
		observability.OutboxPublish.WithLabelValues("permanent_failure").Inc()
		slog.WarnContext(ctx, "outbox message was unroutable", "event", observability.EventOutboxPublishFailed, "failure", "permanent", "message_id", row.MessageID, "attempt", row.UnpublishableAttempts+1, "retry_in", delay)
		return
	}
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
	observability.OutboxPublish.WithLabelValues("transient_failure").Inc()
	slog.WarnContext(
		ctx,
		"outbox publish failed",
		"event", observability.EventOutboxPublishFailed,
		"failure", "transient",
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
