package config

import (
	"errors"
	"fmt"
	"os"
)

type RabbitMQConfig struct {
	URL                 string
	ConsumerConcurrency int
	ConsumerPool        ConsumerPoolConfig
	ConsumerMaxAttempts int
}

const defaultOrderConsumerConcurrency = 1

const defaultConsumerMaxAttempts = 5

// LoadRabbitMQConfig reads the RabbitMQ URL and consumer settings from the environment.
func LoadRabbitMQConfig() (RabbitMQConfig, error) {
	if err := loadEnvFile(); err != nil {
		return RabbitMQConfig{}, fmt.Errorf("load .env: %w", err)
	}

	url := os.Getenv("RABBITMQ_URL")
	if url == "" {
		return RabbitMQConfig{}, errors.New("RABBITMQ_URL is not set")
	}

	consumerConcurrency, err := envInt(
		"ORDER_CONSUMER_CONCURRENCY",
		defaultOrderConsumerConcurrency,
	)
	if err != nil {
		return RabbitMQConfig{}, err
	}
	if consumerConcurrency <= 0 {
		return RabbitMQConfig{}, errors.New("ORDER_CONSUMER_CONCURRENCY must be greater than zero")
	}

	consumerPool, err := loadConsumerPoolConfig("ORDER", consumerConcurrency)
	if err != nil {
		return RabbitMQConfig{}, err
	}

	maxAttempts, err := envInt("CONSUMER_MAX_ATTEMPTS", defaultConsumerMaxAttempts)
	if err != nil {
		return RabbitMQConfig{}, err
	}
	if maxAttempts <= 0 {
		return RabbitMQConfig{}, errors.New("CONSUMER_MAX_ATTEMPTS must be greater than zero")
	}

	return RabbitMQConfig{
		ConsumerMaxAttempts: maxAttempts,
		URL:                 url,
		ConsumerConcurrency: consumerConcurrency,
		ConsumerPool:        consumerPool,
	}, nil
}
