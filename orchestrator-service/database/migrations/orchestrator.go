package migrations

import (
	"github.com/chrisfonseca08/distri/orchestrator-service/database/models"
	"gorm.io/gorm"
)

func MigrateOrchestrator(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&models.Saga{},
		&models.SagaStep{},
		&models.CheckoutAttempt{},
	); err != nil {
		return err
	}

	// AutoMigrate does not remove fields deleted from a model.
	if db.Migrator().HasColumn(&models.Saga{}, "request_hash") {
		return db.Migrator().DropColumn(&models.Saga{}, "request_hash")
	}
	return nil
}
