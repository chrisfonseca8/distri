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
	transientRequeueDelay = 500 * time.Millisecond
	reconnectInitialDelay = time.Second
	reconnectMaximumDelay = 30 * time.Second
)

type Delivery = amqp.Delivery

type Handler func(context.Context, Delivery) error

type TransientError struct {
	Err error
}

func (e *TransientError) Error() string { return e.Err.Error() }
func (e *TransientError) Unwrap() error { return e.Err }

func Transient(err error) error {
	if err == nil {
		return nil
	}
	return &TransientError{Err: err}
}

type PermanentError struct {
	Err error
}

func (e *PermanentError) Error() string { return e.Err.Error() }
func (e *PermanentError) Unwrap() error { return e.Err }

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
	transientRequeueDelay time.Duration
	reconnectInitialDelay time.Duration
	reconnectMaximumDelay time.Duration
}

func NewConsumer(url string, queue string, handler Handler) (*Consumer, error) {
	if strings.TrimSpace(url) == "" {
		return nil, errors.New("RabbitMQ URL is required")
	}
	if strings.TrimSpace(queue) == "" {
		return nil, errors.New("RabbitMQ queue is required")
	}
	if handler == nil {
		return nil, errors.New("RabbitMQ delivery handler is required")
	}
	return &Consumer{
		url:                   url,
		queue:                 queue,
		handler:               handler,
		transientRequeueDelay: transientRequeueDelay,
		reconnectInitialDelay: reconnectInitialDelay,
		reconnectMaximumDelay: reconnectMaximumDelay,
	}, nil
}

func (c *Consumer) Run(ctx context.Context) {
	slog.Info("RabbitMQ consumer started", "queue", c.queue)
	defer slog.Info("RabbitMQ consumer stopped", "queue", c.queue)

	reconnectDelay := c.reconnectInitialDelay
	for ctx.Err() == nil {
		connected, err := c.consume(ctx)
		if ctx.Err() != nil {
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

func (c *Consumer) consume(ctx context.Context) (bool, error) {
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

func (c *Consumer) handleDelivery(ctx context.Context, delivery Delivery) error {
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

	if !waitForConsumer(ctx, c.transientRequeueDelay) {
		if nackErr := delivery.Nack(false, true); nackErr != nil {
			return fmt.Errorf("requeue RabbitMQ delivery during shutdown: %w", nackErr)
		}
		return nil
	}
	slog.Warn(
		"transient RabbitMQ delivery failure",
		"queue", c.queue,
		"message_id", delivery.MessageId,
		"error", err,
	)
	// TODO(OR8): replace unbounded immediate requeues with retry queues and an
	// attempt limit before dead-lettering.
	if nackErr := delivery.Nack(false, true); nackErr != nil {
		return fmt.Errorf("nack transient RabbitMQ delivery: %w", nackErr)
	}
	return nil
}

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
