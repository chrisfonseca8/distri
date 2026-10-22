package config

import (
	"errors"
	"fmt"
	"os"
)

type RabbitMQConfig struct {
	URL string
}

func LoadRabbitMQConfig() (RabbitMQConfig, error) {
	if err := loadEnvFile(); err != nil {
		return RabbitMQConfig{}, fmt.Errorf("load .env: %w", err)
	}

	url := os.Getenv("RABBITMQ_URL")
	if url == "" {
		return RabbitMQConfig{}, errors.New("RABBITMQ_URL is not set")
	}

	return RabbitMQConfig{URL: url}, nil
}
