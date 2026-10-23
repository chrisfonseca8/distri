package config

import (
	"errors"
	"fmt"
	"os"
)

const defaultOrchestratorConsumerConcurrency = 1

type RabbitMQConfig struct {
	URL                 string
	ConsumerConcurrency int
	ConsumerPool        ConsumerPoolConfig
	ConsumerMaxAttempts int
}

const defaultConsumerMaxAttempts = 5

func LoadRabbitMQConfig() (RabbitMQConfig, error) {
	if err := loadEnvFile(); err != nil {
		return RabbitMQConfig{}, fmt.Errorf("load .env: %w", err)
	}

	url := os.Getenv("RABBITMQ_URL")
	if url == "" {
		return RabbitMQConfig{}, errors.New("RABBITMQ_URL is not set")
	}

	concurrency, err := envInt(
		"ORCHESTRATOR_CONSUMER_CONCURRENCY",
		defaultOrchestratorConsumerConcurrency,
	)
	if err != nil {
		return RabbitMQConfig{}, err
	}
	if concurrency <= 0 {
		return RabbitMQConfig{}, errors.New("ORCHESTRATOR_CONSUMER_CONCURRENCY must be greater than zero")
	}
	consumerPool, err := loadConsumerPoolConfig("ORCHESTRATOR", concurrency)
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
		ConsumerConcurrency: concurrency,
		ConsumerPool:        consumerPool,
	}, nil
}
