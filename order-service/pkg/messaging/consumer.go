package messaging

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	consumerPrefetch      = 10
	reconnectInitialDelay = time.Second
	reconnectMaximumDelay = 30 * time.Second
)

type Delivery = amqp.Delivery

type Handler func(context.Context, Delivery) error

type TransientError struct {
	Err error
}

// Error returns the message of the wrapped error.
func (e *TransientError) Error() string { return e.Err.Error() }

// Unwrap returns the wrapped error.
func (e *TransientError) Unwrap() error { return e.Err }

// Transient marks err as temporary, so the message is retried later.
func Transient(err error) error {
	if err == nil {
		return nil
	}
	return &TransientError{Err: err}
}

type PermanentError struct {
	Err error
}

// Error returns the message of the wrapped error.
func (e *PermanentError) Error() string { return e.Err.Error() }

// Unwrap returns the wrapped error.
func (e *PermanentError) Unwrap() error { return e.Err }

// Permanent marks err as unfixable by retrying, so the message goes to the dead-letter queue.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentError{Err: err}
}

type Consumer struct {
	url                   string
	queue                 string
	handler               Handler
	maxAttempts           int
	retry                 *retryPublisher
	reconnectInitialDelay time.Duration
	reconnectMaximumDelay time.Duration
}

// NewConsumer creates a consumer that passes each message from queue to handler.
func NewConsumer(url string, queue string, handler Handler, options ...ConsumerOption) (*Consumer, error) {
	if strings.TrimSpace(url) == "" {
		return nil, errors.New("RabbitMQ URL is required")
	}
	if strings.TrimSpace(queue) == "" {
		return nil, errors.New("RabbitMQ queue is required")
	}
	if handler == nil {
		return nil, errors.New("RabbitMQ delivery handler is required")
	}
	consumer := &Consumer{
		url:                   url,
		queue:                 queue,
		handler:               handler,
		maxAttempts:           DefaultMaxAttempts,
		reconnectInitialDelay: reconnectInitialDelay,
		reconnectMaximumDelay: reconnectMaximumDelay,
	}
	for _, option := range options {
		option(consumer)
	}
	return consumer, nil
}

// Run consumes messages until ctx is cancelled, reconnecting if the broker drops.
func (c *Consumer) Run(ctx context.Context) {
	c.RunUntil(ctx, nil)
}

// RunUntil is Run that also ends when stop is closed. A stop is observed only
// between deliveries, so the delivery being handled always finishes and acks.
func (c *Consumer) RunUntil(ctx context.Context, stop <-chan struct{}) {
	slog.Info("RabbitMQ consumer started", "queue", c.queue)
	defer slog.Info("RabbitMQ consumer stopped", "queue", c.queue)

	reconnectDelay := c.reconnectInitialDelay
	for ctx.Err() == nil && !isStopped(stop) {
		connected, err := c.consume(ctx, stop)
		if ctx.Err() != nil || isStopped(stop) {
			return
		}
		if err != nil {
			slog.Warn(
				"RabbitMQ consumer disconnected",
				"queue", c.queue,
				"retry_in", reconnectDelay,
				"error", err,
			)
		}
		if connected {
			reconnectDelay = c.reconnectInitialDelay
		}
		if !waitForConsumer(ctx, reconnectDelay) {
			return
		}
		if !connected && reconnectDelay < c.reconnectMaximumDelay {
			reconnectDelay *= 2
			if reconnectDelay > c.reconnectMaximumDelay {
				reconnectDelay = c.reconnectMaximumDelay
			}
		}
	}
}

// isStopped reports whether the stop channel has been closed.
func isStopped(stop <-chan struct{}) bool {
	select {
	case <-stop:
		return true
	default:
		return false
	}
}

// consume runs one broker connection and reports whether it connected, so Run knows whether to reset its backoff.
func (c *Consumer) consume(ctx context.Context, stop <-chan struct{}) (bool, error) {
	connection, err := amqp.Dial(c.url)
	if err != nil {
		return false, fmt.Errorf("connect RabbitMQ consumer: %w", err)
	}
	defer connection.Close()

	channel, err := connection.Channel()
	if err != nil {
		return false, fmt.Errorf("open RabbitMQ consumer channel: %w", err)
	}
	defer channel.Close()

	retry, err := newRetryPublisher(connection)
	if err != nil {
		return false, err
	}
	defer retry.close()
	c.retry = retry
	defer func() { c.retry = nil }()

	if err := channel.Qos(consumerPrefetch, 0, false); err != nil {
		return false, fmt.Errorf("configure RabbitMQ consumer qos: %w", err)
	}

	deliveries, err := channel.Consume(
		c.queue,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return false, fmt.Errorf("consume RabbitMQ queue %q: %w", c.queue, err)
	}

	for {
		select {
		case <-ctx.Done():
			return true, nil
		case <-stop:
			return true, nil
		case delivery, open := <-deliveries:
			if !open {
				return true, errors.New("RabbitMQ delivery channel closed")
			}
			if err := c.handleDelivery(ctx, delivery); err != nil {
				return true, err
			}
		}
	}
}

// handleDelivery runs the handler and acks, retries or dead-letters the message based on the result.
func (c *Consumer) handleDelivery(ctx context.Context, delivery Delivery) error {
	delivery = withOriginalRoutingKey(delivery)
	err := c.handler(ctx, delivery)
	if err == nil {
		if ackErr := delivery.Ack(false); ackErr != nil {
			return fmt.Errorf("ack RabbitMQ delivery: %w", ackErr)
		}
		return nil
	}

	var permanentError *PermanentError
	if errors.As(err, &permanentError) {
		slog.Warn(
			"permanent RabbitMQ delivery failure",
			"queue", c.queue,
			"message_id", delivery.MessageId,
			"error", err,
		)
		if nackErr := delivery.Nack(false, false); nackErr != nil {
			return fmt.Errorf("nack permanent RabbitMQ delivery: %w", nackErr)
		}
		return nil
	}

	attempt := deliveryAttempt(delivery) + 1
	slog.Warn(
		"transient RabbitMQ delivery failure",
		"queue", c.queue,
		"message_id", delivery.MessageId,
		"attempt", attempt,
		"max_attempts", c.attemptLimit(),
		"error", err,
	)
	if attempt >= c.attemptLimit() {
		slog.Error(
			"RabbitMQ delivery exhausted its attempts; dead-lettering",
			"queue", c.queue,
			"message_id", delivery.MessageId,
			"attempts", attempt,
		)
		if nackErr := delivery.Nack(false, false); nackErr != nil {
			return fmt.Errorf("dead-letter exhausted RabbitMQ delivery: %w", nackErr)
		}
		return nil
	}

	// Ack the original only after the broker confirmed the delayed copy. If
	// the republish fails the original is requeued (at-least-once) and the
	// error ends this connection so the reconnect backoff paces the retries.
	if retryErr := c.republish(ctx, delivery, attempt, err); retryErr != nil {
		if nackErr := delivery.Nack(false, true); nackErr != nil {
			return fmt.Errorf("requeue RabbitMQ delivery: %w", nackErr)
		}
		return retryErr
	}
	if ackErr := delivery.Ack(false); ackErr != nil {
		return fmt.Errorf("ack retried RabbitMQ delivery: %w", ackErr)
	}
	return nil
}

// attemptLimit returns how many times a message may be tried before it is dead-lettered.
func (c *Consumer) attemptLimit() int {
	if c.maxAttempts <= 0 {
		return DefaultMaxAttempts
	}
	return c.maxAttempts
}

// republish sends the message to the retry queue with a higher attempt count.
func (c *Consumer) republish(ctx context.Context, delivery Delivery, attempt int, cause error) error {
	if c.retry == nil {
		return errors.New("RabbitMQ retry publisher is not connected")
	}
	return c.retry.publish(ctx, delivery, c.queue, attempt, cause)
}

// waitForConsumer sleeps for delay and returns false if ctx is cancelled first.
func waitForConsumer(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
