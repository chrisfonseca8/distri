package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/chrisfonseca08/distri/inventory-service/database/migrations"
	"github.com/chrisfonseca08/distri/inventory-service/pkg/config"
	"gorm.io/gorm"
)

// main runs the schema migration and exits non-zero if it fails.
func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if err := run(); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
	slog.Info("migration completed", "schema", "inventory")
}

// run opens the database and applies the inventory migrations.
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

	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS inventory").Error; err != nil {
			return fmt.Errorf("create inventory schema: %w", err)
		}
		if err := tx.Exec("SET LOCAL search_path TO inventory").Error; err != nil {
			return fmt.Errorf("select inventory schema: %w", err)
		}

		return migrations.MigrateInventory(tx)
	})
}
