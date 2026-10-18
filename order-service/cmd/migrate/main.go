package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/chrisfonseca08/distri/order-service/database/migrations"
	"github.com/chrisfonseca08/distri/order-service/pkg/config"
	"gorm.io/gorm"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}

	slog.Info("migration completed")
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

	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS orders").Error; err != nil {
			return fmt.Errorf("create orders schema: %w", err)
		}

		if err := tx.Exec("SET LOCAL search_path TO orders").Error; err != nil {
			return fmt.Errorf("select orders schema: %w", err)
		}

		var schema string
		if err := tx.Raw("SELECT current_schema()").Scan(&schema).Error; err != nil {
			return fmt.Errorf("read current schema: %w", err)
		}
		if schema != "orders" {
			return fmt.Errorf("expected orders schema, got %q", schema)
		}

		slog.Info("running migration", "schema", schema)

		if err := migrations.MigrateProductInventory(tx); err != nil {
			return fmt.Errorf("migrate product and inventory: %w", err)
		}

		if err := migrations.MigrateOrders(tx); err != nil {
			return fmt.Errorf("migrate orders: %w", err)
		}

		return nil
	})
}
