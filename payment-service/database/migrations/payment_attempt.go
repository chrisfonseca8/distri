package migrations

import (
	"github.com/chrisfonseca08/distri/payment-service/database/models"
	"gorm.io/gorm"
)

func MigratePaymentAttempts(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&models.PaymentAttempt{},
		&models.Job{},
	); err != nil {
		return err
	}

	// Result notification was deliberately removed from the C6a scope. These
	// rows never had a handler, so retaining them would leave a false backlog.
	return db.Table("payments.jobs").
		Where("type = ?", "notify_order").
		Delete(&models.Job{}).Error
}
