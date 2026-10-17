package config

import "gorm.io/gorm"

type Config struct {
	DB *gorm.DB
}

func Setup() (*Config, error) {
	db, err := OpenDB()
	if err != nil {
		return nil, err
	}

	return &Config{DB: db}, nil
}
