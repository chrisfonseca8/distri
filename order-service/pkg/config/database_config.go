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

// OpenDB opens the order service's application database.
func OpenDB() (*gorm.DB, error) {
	return openDB("DATABASE_URL")
}

// OpenTestDB opens the database reserved for tests.
func OpenTestDB() (*gorm.DB, error) {
	return openDB("DATABASE_TEST_URL")
}

func openDB(environmentVariable string) (*gorm.DB, error) {
	if err := loadEnvFile(); err != nil {
		return nil, fmt.Errorf("load .env: %w", err)
	}

	dsn := os.Getenv(environmentVariable)
	if dsn == "" {
		return nil, fmt.Errorf("%s is not set", environmentVariable)
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	return db, nil
}

func loadEnvFile() error {
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
