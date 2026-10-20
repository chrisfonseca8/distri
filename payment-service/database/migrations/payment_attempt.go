package migrations

import (
	"github.com/chrisfonseca08/distri/payment-service/database/models"
	"gorm.io/gorm"
)

func MigratePaymentAttempts(db *gorm.DB) error {
	return db.AutoMigrate(
		&models.PaymentAttempt{},
		&models.Job{},
	)
}
