package messaging

import (
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	topologySetupAttempts = 5
	topologyRetryDelay    = time.Second
)

const (
	CommandsExchange   = "distri.commands"
	DeadLetterExchange = "distri.commands.dlx"
	DeadLetterQueue    = "distri.commands.dead"

	OrderCommandsQueue       = "distri.order.commands"
	PaymentCommandsQueue     = "distri.payment.commands"
	InventoryCommandsQueue   = "distri.inventory.commands"
	OrchestratorRepliesQueue = "distri.orchestrator.replies"
)

// retryTiers are the delay queues behind every work queue Q: Q.retry.<name>
// holds a message for ttl, then dead-letters it back to Q through the default
// exchange. Consumers pick the tier by attempt number and must use these names
// (see retry.go in each service).
var retryTiers = []struct {
	name string
	ttl  time.Duration
}{
	{name: "5s", ttl: 5 * time.Second},
	{name: "30s", ttl: 30 * time.Second},
	{name: "2m", ttl: 2 * time.Minute},
}

type queueBinding struct {
	queue      string
	routingKey string
}

var commandQueueBindings = []queueBinding{
	{queue: OrderCommandsQueue, routingKey: "order.#"},
	{queue: PaymentCommandsQueue, routingKey: "payment.#"},
	{queue: InventoryCommandsQueue, routingKey: "inventory.#"},
	{queue: OrchestratorRepliesQueue, routingKey: "reply.#"},
}

// DeclareTopology creates the durable command and dead-letter topology.
// RabbitMQ declarations are idempotent, so this is safe to call at startup.
func DeclareTopology(channel *amqp.Channel) error {
	if err := channel.ExchangeDeclare(
		CommandsExchange, "topic", true, false, false, false, nil,
	); err != nil {
		return fmt.Errorf("declare commands exchange: %w", err)
	}

	if err := channel.ExchangeDeclare(
		DeadLetterExchange, "topic", true, false, false, false, nil,
	); err != nil {
		return fmt.Errorf("declare dead-letter exchange: %w", err)
	}

	queueArguments := amqp.Table{
		"x-dead-letter-exchange": DeadLetterExchange,
	}
	for _, binding := range commandQueueBindings {
		if _, err := channel.QueueDeclare(
			binding.queue, true, false, false, false, queueArguments,
		); err != nil {
			return fmt.Errorf("declare queue %q: %w", binding.queue, err)
		}

		if err := channel.QueueBind(
			binding.queue,
			binding.routingKey,
			CommandsExchange,
			false,
			nil,
		); err != nil {
			return fmt.Errorf("bind queue %q: %w", binding.queue, err)
		}

		for _, tier := range retryTiers {
			retryQueue := binding.queue + ".retry." + tier.name
			if _, err := channel.QueueDeclare(
				retryQueue, true, false, false, false, amqp.Table{
					"x-message-ttl":             int32(tier.ttl.Milliseconds()),
					"x-dead-letter-exchange":    "",
					"x-dead-letter-routing-key": binding.queue,
				},
			); err != nil {
				return fmt.Errorf("declare retry queue %q: %w", retryQueue, err)
			}
		}
	}

	if _, err := channel.QueueDeclare(
		DeadLetterQueue, true, false, false, false, nil,
	); err != nil {
		return fmt.Errorf("declare dead-letter queue: %w", err)
	}

	if err := channel.QueueBind(
		DeadLetterQueue, "#", DeadLetterExchange, false, nil,
	); err != nil {
		return fmt.Errorf("bind dead-letter queue: %w", err)
	}

	return nil
}

// SetupTopology connects to RabbitMQ and declares the durable topology. The
// connection is only used for bootstrap; later publishers and consumers should
// own long-lived connections of their own.
func SetupTopology(url string) error {
	var lastErr error
	for attempt := 1; attempt <= topologySetupAttempts; attempt++ {
		if err := setupTopologyOnce(url); err == nil {
			return nil
		} else {
			lastErr = err
		}

		if attempt < topologySetupAttempts {
			time.Sleep(topologyRetryDelay)
		}
	}

	return fmt.Errorf(
		"set up RabbitMQ topology after %d attempts: %w",
		topologySetupAttempts,
		lastErr,
	)
}

// setupTopologyOnce declares the exchanges and queues once over a short-lived connection.
func setupTopologyOnce(url string) error {
	connection, err := amqp.Dial(url)
	if err != nil {
		return fmt.Errorf("connect to RabbitMQ: %w", err)
	}
	defer connection.Close()

	channel, err := connection.Channel()
	if err != nil {
		return fmt.Errorf("open RabbitMQ channel: %w", err)
	}
	defer channel.Close()

	return DeclareTopology(channel)
}
