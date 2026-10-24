package config

import (
	"errors"
	"fmt"
	"os"
)

const (
	defaultPaymentConsumerConcurrency = 1
	defaultPaymentConsumerPrefetch    = 10
)

type RabbitMQConfig struct {
	URL                 string
	ConsumerConcurrency int
	ConsumerPrefetch    int
	ConsumerPool        ConsumerPoolConfig
	ConsumerMaxAttempts int
}

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
	concurrency, err := envInt("PAYMENT_CONSUMER_CONCURRENCY", defaultPaymentConsumerConcurrency)
	if err != nil {
		return RabbitMQConfig{}, err
	}
	prefetch, err := envInt("PAYMENT_CONSUMER_PREFETCH", defaultPaymentConsumerPrefetch)
	if err != nil {
		return RabbitMQConfig{}, err
	}
	if concurrency <= 0 {
		return RabbitMQConfig{}, errors.New("PAYMENT_CONSUMER_CONCURRENCY must be greater than zero")
	}
	if prefetch <= 0 {
		return RabbitMQConfig{}, errors.New("PAYMENT_CONSUMER_PREFETCH must be greater than zero")
	}
	consumerPool, err := loadConsumerPoolConfig("PAYMENT", concurrency)
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
		ConsumerPool:        consumerPool,
		ConsumerConcurrency: concurrency,
		ConsumerPrefetch:    prefetch,
	}, nil
}
