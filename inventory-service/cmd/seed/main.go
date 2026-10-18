package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/chrisfonseca08/distri/inventory-service/database/seeding"
	"github.com/chrisfonseca08/distri/inventory-service/pkg/config"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if err := run(); err != nil {
		slog.Error("seeding failed", "error", err)
		os.Exit(1)
	}
	slog.Info(
		"seeding completed",
		"products", seeding.InventorySeedCount,
		"inventories", seeding.InventorySeedCount,
	)
}

func run() error {
	db, err := config.OpenDB()
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get database connection: %w", err)
	}
	defer sqlDB.Close()

	return seeding.SeedInventory(db)
}
