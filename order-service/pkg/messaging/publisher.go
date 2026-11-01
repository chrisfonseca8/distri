package messaging

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
	"uuid"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/chrisfonseca08/distri/order-service/pkg/observability"
)

var ErrUnroutable = errors.New("RabbitMQ message was not routed")

type Publisher struct {
	mu         sync.Mutex
	url        string
	connection *amqp.Connection
	channel    *amqp.Channel
	returns    <-chan amqp.Return
}

// NewPublisher connects to RabbitMQ and returns a publisher.
func NewPublisher(url string) (*Publisher, error) {
	publisher := &Publisher{url: url}
	if err := publisher.connectLocked(); err != nil {
		return nil, err
	}
	return publisher, nil
}

// Publish sends payload to the given routing key on RabbitMQ.
func (p *Publisher) Publish(
	ctx context.Context,
	messageID uuid.UUID,
	routingKey string,
	payload []byte,
) (err error) {
	ctx = observability.WithMessageID(ctx, messageID.String())
	ctx, span := observability.StartSpan(ctx, "rabbitmq.publish "+routingKey, trace.SpanKindProducer,
		attribute.String("messaging.system", "rabbitmq"), attribute.String("messaging.destination.name", CommandsExchange),
		attribute.String("messaging.rabbitmq.routing_key", routingKey))
	result := "confirmed"
	defer func() {
		switch {
		case err == nil:
		case errors.Is(err, ErrUnroutable):
			result = "unroutable"
		case err.Error() == "RabbitMQ negatively acknowledged message":
			result = "nacked"
		default:
			result = "error"
		}
		observability.RabbitPublish.WithLabelValues(routingKey, result).Inc()
		observability.EndSpan(span, err)
	}()

	p.mu.Lock()
	defer p.mu.Unlock()

	if messageID == uuid.Nil() {
		return errors.New("RabbitMQ message ID is required")
	}
	if p.channel == nil || p.channel.IsClosed() {
		if err := p.connectLocked(); err != nil {
			return err
		}
	}

	confirmation, err := p.channel.PublishWithDeferredConfirmWithContext(
		ctx,
		CommandsExchange,
		routingKey,
		true,
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			MessageId:    messageID.String(),
			Headers:      observability.InjectAMQP(ctx, nil),
			Timestamp:    time.Now(),
			Body:         payload,
		},
	)
	if err != nil {
		p.disconnectLocked()
		return fmt.Errorf("publish RabbitMQ message: %w", err)
	}
	if confirmation == nil {
		p.disconnectLocked()
		return errors.New("RabbitMQ publisher confirm was not registered")
	}

	acknowledged, err := confirmation.WaitContext(ctx)
	if err != nil {
		p.disconnectLocked()
		return fmt.Errorf("wait for RabbitMQ publisher confirm: %w", err)
	}
	if !acknowledged {
		p.disconnectLocked()
		return errors.New("RabbitMQ negatively acknowledged message")
	}

	select {
	case returned, open := <-p.returns:
		if !open {
			p.disconnectLocked()
			return errors.New("RabbitMQ return listener closed")
		}
		return fmt.Errorf(
			"%w: routing key %q: %s",
			ErrUnroutable,
			returned.RoutingKey,
			returned.ReplyText,
		)
	default:
		return nil
	}
}

// Close closes the publisher's channel and connection.
func (p *Publisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.disconnectLocked()
}

// connectLocked opens a fresh connection and channel; the caller must hold the lock.
func (p *Publisher) connectLocked() error {
	_ = p.disconnectLocked()

	connection, err := amqp.Dial(p.url)
	if err != nil {
		return fmt.Errorf("connect RabbitMQ publisher: %w", err)
	}

	channel, err := connection.Channel()
	if err != nil {
		_ = connection.Close()
		return fmt.Errorf("open RabbitMQ publisher channel: %w", err)
	}
	if err := channel.Confirm(false); err != nil {
		_ = channel.Close()
		_ = connection.Close()
		return fmt.Errorf("enable RabbitMQ publisher confirms: %w", err)
	}

	returnedMessages := make(chan amqp.Return, 1)
	p.connection = connection
	p.channel = channel
	p.returns = channel.NotifyReturn(returnedMessages)
	return nil
}

// disconnectLocked closes the channel and connection; the caller must hold the lock.
func (p *Publisher) disconnectLocked() error {
	var closeErr error
	if p.channel != nil {
		if err := p.channel.Close(); err != nil && !errors.Is(err, amqp.ErrClosed) {
			closeErr = err
		}
	}
	if p.connection != nil {
		if err := p.connection.Close(); err != nil && !errors.Is(err, amqp.ErrClosed) && closeErr == nil {
			closeErr = err
		}
	}
	p.connection = nil
	p.channel = nil
	p.returns = nil
	return closeErr
}
