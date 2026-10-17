package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/chrisfonseca08/distri/order-service/database/seeding"
	"github.com/chrisfonseca08/distri/order-service/pkg/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("seeding failed", "error", err)
		os.Exit(1)
	}

	slog.Info("seeding completed", "products", 3, "inventories", 3)
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

	if err := seeding.SeedProductInventory(db); err != nil {
		return fmt.Errorf("seed product inventory: %w", err)
	}

	return nil
}
