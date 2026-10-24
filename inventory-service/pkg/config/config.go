package config

import "gorm.io/gorm"

type Config struct {
	DB *gorm.DB
}

// Setup opens the database and returns the service configuration.
func Setup() (*Config, error) {
	db, err := OpenDB()
	if err != nil {
		return nil, err
	}

	return &Config{DB: db}, nil
}
