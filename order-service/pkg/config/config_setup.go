package config

import "gorm.io/gorm"

// Config holds the service configuration assembled by Config_setup.
type Config struct {
	DB *gorm.DB
}

// Config_setup opens the database and returns the service configuration.
func Config_setup() (*Config, error) {
	db, err := OpenDB()
	if err != nil {
		return nil, err
	}

	return &Config{DB: db}, nil
}
