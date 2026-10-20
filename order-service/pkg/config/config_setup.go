package config

import "gorm.io/gorm"

// Config holds the service configuration assembled by Config_setup.
type Config struct {
	DB      *gorm.DB
	Worker  WorkerConfig
	Sweeper SweeperConfig
}

func Config_setup() (*Config, error) {
	workerConfig, err := LoadWorkerConfig()
	if err != nil {
		return nil, err
	}
	sweeperConfig, err := LoadSweeperConfig()
	if err != nil {
		return nil, err
	}

	db, err := OpenDB()
	if err != nil {
		return nil, err
	}

	return &Config{DB: db, Worker: workerConfig, Sweeper: sweeperConfig}, nil
}
