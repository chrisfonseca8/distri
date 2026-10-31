package config

import (
	"strings"
	"testing"
)

func TestLoadRabbitMQConfigDefaultsConsumerConcurrency(t *testing.T) {
	isolateRabbitMQConfigTest(t)
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
	isolateRabbitMQConfigTest(t)
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
			isolateRabbitMQConfigTest(t)
			t.Setenv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
			t.Setenv("ORDER_CONSUMER_CONCURRENCY", value)

			_, err := LoadRabbitMQConfig()
			if err == nil || !strings.Contains(err.Error(), "ORDER_CONSUMER_CONCURRENCY") {
				t.Fatalf("LoadRabbitMQConfig() error = %v", err)
			}
		})
	}
}

func isolateRabbitMQConfigTest(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	for _, name := range []string{
		"RABBITMQ_URL",
		"ORDER_CONSUMER_CONCURRENCY",
		"ORDER_CONSUMER_MAX_CONCURRENCY",
		"ORDER_CONSUMER_SCALE_INTERVAL",
		"ORDER_CONSUMER_SCALE_DEPTH_PER_WORKER",
		"ORDER_CONSUMER_SCALE_DOWN_AFTER",
		"CONSUMER_MAX_ATTEMPTS",
	} {
		t.Setenv(name, "")
	}
}
