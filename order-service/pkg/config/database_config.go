package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const (
	defaultMaxOpenConnections = 10
	defaultMaxIdleConnections = 5
	defaultConnectionLifetime = 30 * time.Minute
)

// OpenDB opens the database reserved for tests.
func OpenDB() (*gorm.DB, error) {
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

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get database connection: %w", err)
	}

	maxOpenConnections, err := envInt("DB_MAX_OPEN_CONNS", defaultMaxOpenConnections)
	if err != nil {
		return nil, err
	}
	maxIdleConnections, err := envInt("DB_MAX_IDLE_CONNS", defaultMaxIdleConnections)
	if err != nil {
		return nil, err
	}
	connectionLifetime, err := envDuration("DB_CONN_MAX_LIFETIME", defaultConnectionLifetime)
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxOpenConns(maxOpenConnections)
	sqlDB.SetMaxIdleConns(maxIdleConnections)
	sqlDB.SetConnMaxLifetime(connectionLifetime)

	return db, nil
}

func envInt(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", name, err)
	}

	return parsed, nil
}

func envDuration(name string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration: %w", name, err)
	}

	return parsed, nil
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
