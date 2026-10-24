package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/chrisfonseca08/distri/orchestrator-service/database/migrations"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/config"
	"gorm.io/gorm"
)

// main runs the schema migration and exits non-zero if it fails.
func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if err := run(); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
	slog.Info("migration completed")
}

// run opens the database and applies the orchestrator migrations.
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
		if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS orchestrator").Error; err != nil {
			return fmt.Errorf("create orchestrator schema: %w", err)
		}
		if err := tx.Exec("SET LOCAL search_path TO orchestrator").Error; err != nil {
			return fmt.Errorf("select orchestrator schema: %w", err)
		}

		var schema string
		if err := tx.Raw("SELECT current_schema()").Scan(&schema).Error; err != nil {
			return fmt.Errorf("read current schema: %w", err)
		}
		if schema != "orchestrator" {
			return fmt.Errorf("expected orchestrator schema, got %q", schema)
		}

		slog.Info("running migration", "schema", schema)
		if err := migrations.MigrateOrchestrator(tx); err != nil {
			return fmt.Errorf("migrate orchestrator: %w", err)
		}
		return nil
	})
}
