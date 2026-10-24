package migrations

import (
	"github.com/chrisfonseca08/distri/order-service/database/models"
	"gorm.io/gorm"
)

// MigrateOrders creates or updates every order table.
func MigrateOrders(db *gorm.DB) error {
	return db.AutoMigrate(
		&models.Order{},
		&models.OrderItem{},
		&models.Outbox{},
		&models.ProcessedMessage{},
	)
}
