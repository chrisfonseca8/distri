package migrations

import (
	"errors"
	"fmt"

	"github.com/chrisfonseca08/distri/orchestrator-service/database/models"
	"gorm.io/gorm"
)

// MigrateOrchestrator creates or updates every orchestrator table.
func MigrateOrchestrator(db *gorm.DB) error {
	if err := migrateSagaCheckoutValues(db); err != nil {
		return err
	}
	if err := db.AutoMigrate(
		&models.Saga{},
		&models.SagaStep{},
		&models.CheckoutAttempt{},
		&models.Outbox{},
		&models.ProcessedMessage{},
	); err != nil {
		return err
	}

	// AutoMigrate does not remove fields deleted from a model.
	if db.Migrator().HasColumn(&models.Saga{}, "request_hash") {
		return db.Migrator().DropColumn(&models.Saga{}, "request_hash")
	}
	return nil
}

// migrateSagaCheckoutValues adds the total and currency columns to sagas and fills them from completed checkout attempts; it fails if any saga is left without values.
func migrateSagaCheckoutValues(db *gorm.DB) error {
	if !db.Migrator().HasTable(&models.Saga{}) {
		return nil
	}

	return db.Transaction(func(tx *gorm.DB) error {
		if !tx.Migrator().HasColumn(&models.Saga{}, "total_paise") {
			if err := tx.Exec(
				"ALTER TABLE orchestrator.sagas ADD COLUMN total_paise bigint",
			).Error; err != nil {
				return fmt.Errorf("add sagas.total_paise: %w", err)
			}
		}
		if !tx.Migrator().HasColumn(&models.Saga{}, "currency") {
			if err := tx.Exec(
				"ALTER TABLE orchestrator.sagas ADD COLUMN currency text",
			).Error; err != nil {
				return fmt.Errorf("add sagas.currency: %w", err)
			}
		}

		if tx.Migrator().HasTable(&models.CheckoutAttempt{}) {
			const backfill = `
UPDATE orchestrator.sagas AS sagas
SET total_paise = COALESCE(
		sagas.total_paise,
		(checkout_attempts.response_body ->> 'total_paise')::bigint
	),
	currency = COALESCE(
		sagas.currency,
		checkout_attempts.response_body ->> 'currency'
	)
FROM orchestrator.checkout_attempts AS checkout_attempts
WHERE checkout_attempts.order_id = sagas.id
	AND checkout_attempts.status = 'completed'`
			if err := tx.Exec(backfill).Error; err != nil {
				return fmt.Errorf("backfill saga checkout values: %w", err)
			}
		}

		var missing int64
		if err := tx.Raw(`
SELECT count(*)
FROM orchestrator.sagas
WHERE total_paise IS NULL OR currency IS NULL`).Scan(&missing).Error; err != nil {
			return fmt.Errorf("check saga checkout values: %w", err)
		}
		if missing != 0 {
			return errors.New("cannot migrate saga checkout values: existing sagas have no completed checkout response")
		}

		if err := tx.Exec(
			"ALTER TABLE orchestrator.sagas ALTER COLUMN total_paise SET NOT NULL",
		).Error; err != nil {
			return fmt.Errorf("make sagas.total_paise non-null: %w", err)
		}
		if err := tx.Exec(
			"ALTER TABLE orchestrator.sagas ALTER COLUMN currency SET NOT NULL",
		).Error; err != nil {
			return fmt.Errorf("make sagas.currency non-null: %w", err)
		}
		return nil
	})
}
