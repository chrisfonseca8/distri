package migrations

import (
	"github.com/chrisfonseca08/distri/order-service/database/models"
	"gorm.io/gorm"
)

func MigrateOrders(db *gorm.DB) error {
	return db.AutoMigrate(
		&models.Order{},
		&models.OrderItem{},
		&models.CheckoutAttempt{},
		&models.Job{},
	)
}
