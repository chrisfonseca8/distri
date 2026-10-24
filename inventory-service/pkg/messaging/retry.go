package messaging

import (
	"context"
	"errors"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	DefaultMaxAttempts = 5

	attemptHeader   = "x-attempt"
	lastErrorHeader = "x-last-error"
	// originalKeyHeader carries the routing key the command was first published
	// with. Dead-lettering from a retry queue replaces the routing key with the
	// work queue's name, and handlers dispatch on the original key.
	originalKeyHeader   = "x-original-routing-key"
	maxLastErrorLength  = 200
	retryPublishTimeout = 10 * time.Second
)

// retryTierNames must match the retry queues the orchestrator declares:
// <queue>.retry.<name>. The first retry uses the first tier, the second the
// second, and every later retry the last one.
var retryTierNames = []string{"5s", "30s", "2m"}

// ConsumerOption customises a Consumer.
type ConsumerOption func(*Consumer)

// WithMaxAttempts sets how many times a message may be handled before a
// transient failure dead-letters it. Values below one keep the default.
func WithMaxAttempts(attempts int) ConsumerOption {
	return func(c *Consumer) {
		if attempts > 0 {
			c.maxAttempts = attempts
		}
	}
}

// RetryQueue returns the delay queue for the given retry number (1-based).
func RetryQueue(queue string, retry int) string {
	tier := retry - 1
	if tier < 0 {
		tier = 0
	}
	if tier >= len(retryTierNames) {
		tier = len(retryTierNames) - 1
	}
	return queue + ".retry." + retryTierNames[tier]
}

// withOriginalRoutingKey restores the routing key a retried message was first
// published with, so handlers see the same key on every attempt.
func withOriginalRoutingKey(delivery Delivery) Delivery {
	if original, ok := delivery.Headers[originalKeyHeader].(string); ok && original != "" {
		delivery.RoutingKey = original
	}
	return delivery
}

// deliveryAttempt is how many times the message was already republished.
func deliveryAttempt(delivery Delivery) int {
	switch value := delivery.Headers[attemptHeader].(type) {
	case int:
		return value
	case int8:
		return int(value)
	case int16:
		return int(value)
	case int32:
		return int(value)
	case int64:
		return int(value)
	case uint8:
		return int(value)
	case uint16:
		return int(value)
	case uint32:
		return int(value)
	case uint64:
		return int(value)
	default:
		return 0
	}
}

// retryPublisher republishes a failed delivery to a delay queue on a channel
// of its own: channels are not goroutine-safe, so it belongs to exactly one
// consumer goroutine. This is a transport-level resend of a message that was
// already committed upstream, not a new fact, so it bypasses the outbox.
type retryPublisher struct {
	channel *amqp.Channel
	returns <-chan amqp.Return
}

// newRetryPublisher opens a confirm-mode channel used to send messages to the retry queue.
func newRetryPublisher(connection *amqp.Connection) (*retryPublisher, error) {
	channel, err := connection.Channel()
	if err != nil {
		return nil, fmt.Errorf("open RabbitMQ retry channel: %w", err)
	}
	if err := channel.Confirm(false); err != nil {
		_ = channel.Close()
		return nil, fmt.Errorf("enable RabbitMQ retry confirms: %w", err)
	}
	return &retryPublisher{
		channel: channel,
		returns: channel.NotifyReturn(make(chan amqp.Return, 1)),
	}, nil
}

// close closes the retry channel.
func (p *retryPublisher) close() {
	_ = p.channel.Close()
}

// publish returns only after the broker confirmed the message, so the caller
// may then ack the original delivery.
func (p *retryPublisher) publish(
	ctx context.Context,
	delivery Delivery,
	queue string,
	attempt int,
	cause error,
) error {
	ctx, cancel := context.WithTimeout(ctx, retryPublishTimeout)
	defer cancel()

	headers := amqp.Table{}
	for key, value := range delivery.Headers {
		// The broker owns the dead-letter bookkeeping headers.
		switch key {
		case "x-death", "x-first-death-queue", "x-first-death-exchange",
			"x-first-death-reason", "x-last-death-queue", "x-last-death-exchange",
			"x-last-death-reason":
			continue
		}
		headers[key] = value
	}
	if _, kept := headers[originalKeyHeader].(string); !kept {
		headers[originalKeyHeader] = delivery.RoutingKey
	}
	headers[attemptHeader] = int32(attempt)
	headers[lastErrorHeader] = truncate(cause.Error(), maxLastErrorLength)

	confirmation, err := p.channel.PublishWithDeferredConfirmWithContext(
		ctx,
		"",
		RetryQueue(queue, attempt),
		true,
		false,
		amqp.Publishing{
			ContentType:  delivery.ContentType,
			DeliveryMode: amqp.Persistent,
			MessageId:    delivery.MessageId,
			Timestamp:    delivery.Timestamp,
			Headers:      headers,
			Body:         delivery.Body,
		},
	)
	if err != nil {
		return fmt.Errorf("publish RabbitMQ retry message: %w", err)
	}
	if confirmation == nil {
		return errors.New("RabbitMQ retry confirm was not registered")
	}
	acknowledged, err := confirmation.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("wait for RabbitMQ retry confirm: %w", err)
	}
	if !acknowledged {
		return errors.New("RabbitMQ negatively acknowledged retry message")
	}

	select {
	case returned := <-p.returns:
		return fmt.Errorf(
			"RabbitMQ retry message was not routed: queue %q: %s",
			returned.RoutingKey,
			returned.ReplyText,
		)
	default:
		return nil
	}
}

// truncate cuts value to at most limit bytes.
func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
