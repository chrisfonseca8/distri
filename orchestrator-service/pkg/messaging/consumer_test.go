package messaging

import (
	"context"
	"errors"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
)

type fakeAcknowledger struct {
	acked        int
	nacked       int
	nackRequeued bool
	ackErr       error
	nackErr      error
}

func (a *fakeAcknowledger) Ack(uint64, bool) error {
	a.acked++
	return a.ackErr
}

func (a *fakeAcknowledger) Nack(_ uint64, _ bool, requeue bool) error {
	a.nacked++
	a.nackRequeued = requeue
	return a.nackErr
}

func (*fakeAcknowledger) Reject(uint64, bool) error { return nil }

func TestConsumerAcknowledgesSuccessfulDelivery(t *testing.T) {
	acknowledger := &fakeAcknowledger{}
	consumer := &Consumer{handler: func(context.Context, Delivery) error { return nil }}
	delivery := amqp.Delivery{Acknowledger: acknowledger, DeliveryTag: 1}

	if err := consumer.handleDelivery(context.Background(), delivery); err != nil {
		t.Fatalf("handle delivery: %v", err)
	}
	if acknowledger.acked != 1 || acknowledger.nacked != 0 {
		t.Fatalf("acked=%d nacked=%d", acknowledger.acked, acknowledger.nacked)
	}
}

func TestConsumerDeadLettersPermanentFailure(t *testing.T) {
	acknowledger := &fakeAcknowledger{}
	consumer := &Consumer{
		queue: "test",
		handler: func(context.Context, Delivery) error {
			return Permanent(errors.New("invalid payload"))
		},
	}
	delivery := amqp.Delivery{Acknowledger: acknowledger, DeliveryTag: 1}

	if err := consumer.handleDelivery(context.Background(), delivery); err != nil {
		t.Fatalf("handle delivery: %v", err)
	}
	if acknowledger.acked != 0 || acknowledger.nacked != 1 || acknowledger.nackRequeued {
		t.Fatalf(
			"acked=%d nacked=%d requeued=%t",
			acknowledger.acked,
			acknowledger.nacked,
			acknowledger.nackRequeued,
		)
	}
}

func TestConsumerRequeuesTransientFailureWhenRetryRepublishFails(t *testing.T) {
	acknowledger := &fakeAcknowledger{}
	consumer := &Consumer{
		queue: "test",
		handler: func(context.Context, Delivery) error {
			return Transient(errors.New("database unavailable"))
		},
	}
	delivery := amqp.Delivery{Acknowledger: acknowledger, DeliveryTag: 1}

	// No retry publisher is connected, so the republish fails: the original is
	// requeued and the error ends the connection.
	if err := consumer.handleDelivery(context.Background(), delivery); err == nil {
		t.Fatal("handle delivery hid the retry republish failure")
	}
	if acknowledger.acked != 0 || acknowledger.nacked != 1 || !acknowledger.nackRequeued {
		t.Fatalf(
			"acked=%d nacked=%d requeued=%t",
			acknowledger.acked,
			acknowledger.nacked,
			acknowledger.nackRequeued,
		)
	}
}

func TestNewConsumerValidatesRequiredValues(t *testing.T) {
	handler := func(context.Context, Delivery) error { return nil }
	if _, err := NewConsumer("", "queue", handler); err == nil {
		t.Fatal("NewConsumer accepted an empty URL")
	}
	if _, err := NewConsumer("amqp://example", "", handler); err == nil {
		t.Fatal("NewConsumer accepted an empty queue")
	}
	if _, err := NewConsumer("amqp://example", "queue", nil); err == nil {
		t.Fatal("NewConsumer accepted a nil handler")
	}
}
