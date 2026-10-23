package config

import (
	"strings"
	"testing"
)

func TestLoadRabbitMQConfigDefaultsConsumerConcurrency(t *testing.T) {
	t.Setenv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
	t.Setenv("ORDER_CONSUMER_CONCURRENCY", "")

	rabbitMQConfig, err := LoadRabbitMQConfig()
	if err != nil {
		t.Fatalf("load RabbitMQ config: %v", err)
	}
	if rabbitMQConfig.ConsumerConcurrency != 1 {
		t.Fatalf("consumer concurrency = %d, want 1", rabbitMQConfig.ConsumerConcurrency)
	}
}

func TestLoadRabbitMQConfigReadsConsumerConcurrency(t *testing.T) {
	t.Setenv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
	t.Setenv("ORDER_CONSUMER_CONCURRENCY", "3")

	rabbitMQConfig, err := LoadRabbitMQConfig()
	if err != nil {
		t.Fatalf("load RabbitMQ config: %v", err)
	}
	if rabbitMQConfig.ConsumerConcurrency != 3 {
		t.Fatalf("consumer concurrency = %d, want 3", rabbitMQConfig.ConsumerConcurrency)
	}
}

func TestLoadRabbitMQConfigRejectsInvalidConsumerConcurrency(t *testing.T) {
	for _, value := range []string{"0", "-1", "not-a-number"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
			t.Setenv("ORDER_CONSUMER_CONCURRENCY", value)

			_, err := LoadRabbitMQConfig()
			if err == nil || !strings.Contains(err.Error(), "ORDER_CONSUMER_CONCURRENCY") {
				t.Fatalf("LoadRabbitMQConfig() error = %v", err)
			}
		})
	}
}
