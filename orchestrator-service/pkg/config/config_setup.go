package config

import "gorm.io/gorm"

// Config holds the orchestrator's application dependencies.
type Config struct {
	DB       *gorm.DB
	RabbitMQ RabbitMQConfig
}

func Setup() (*Config, error) {
	rabbitMQConfig, err := LoadRabbitMQConfig()
	if err != nil {
		return nil, err
	}

	db, err := OpenDB()
	if err != nil {
		return nil, err
	}

	return &Config{
		DB:       db,
		RabbitMQ: rabbitMQConfig,
	}, nil
}
