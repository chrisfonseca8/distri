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
}

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
	return RabbitMQConfig{
		URL:                 url,
		ConsumerConcurrency: concurrency,
		ConsumerPrefetch:    prefetch,
	}, nil
}
