package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/chrisfonseca08/distri/payment-service/database/seeding"
	"github.com/chrisfonseca08/distri/payment-service/pkg/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("seeding failed", "error", err)
		os.Exit(1)
	}

	slog.Info("seeding completed", "payment_attempts", 3)
}

func run() error {
	db, err := config.OpenDB()
	if err != nil {
		return fmt.Errorf("open test database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get database connection: %w", err)
	}
	defer sqlDB.Close()

	if err := seeding.SeedPaymentAttempts(db); err != nil {
		return fmt.Errorf("seed payment attempts: %w", err)
	}

	return nil
}
