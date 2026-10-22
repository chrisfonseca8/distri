package messaging

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
	"uuid"

	amqp "github.com/rabbitmq/amqp091-go"
)

var ErrUnroutable = errors.New("RabbitMQ message was not routed")

type Publisher struct {
	mu         sync.Mutex
	url        string
	connection *amqp.Connection
	channel    *amqp.Channel
	returns    <-chan amqp.Return
}

func NewPublisher(url string) (*Publisher, error) {
	publisher := &Publisher{url: url}
	if err := publisher.connectLocked(); err != nil {
		return nil, err
	}
	return publisher, nil
}

func (p *Publisher) Publish(
	ctx context.Context,
	messageID uuid.UUID,
	routingKey string,
	payload []byte,
) error {
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

func (p *Publisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.disconnectLocked()
}

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
