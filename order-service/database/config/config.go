package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func OpenTestDB() (*gorm.DB, error) {
	if err := loadEnvFile(); err != nil {
		return nil, fmt.Errorf("load .env: %w", err)
	}

	dsn := os.Getenv("DATABASE_TEST_URL")
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_TEST_URL is not set")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	return db, nil
}

func loadEnvFile() error {
	if _, exists := os.LookupEnv("DATABASE_TEST_URL"); exists {
		return nil
	}

	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	for {
		path := filepath.Join(dir, ".env")
		if _, err := os.Stat(path); err == nil {
			return godotenv.Load(path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect %s: %w", path, err)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}
