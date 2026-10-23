package config

import (
	"errors"
	"fmt"
	"os"
)

type RabbitMQConfig struct {
	URL                 string
	ConsumerConcurrency int
}

const defaultOrderConsumerConcurrency = 1

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

	return RabbitMQConfig{
		URL:                 url,
		ConsumerConcurrency: consumerConcurrency,
	}, nil
}
