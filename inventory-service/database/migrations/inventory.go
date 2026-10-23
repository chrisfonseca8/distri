package migrations

import (
	"github.com/chrisfonseca08/distri/inventory-service/database/models"
	"gorm.io/gorm"
)

func MigrateInventory(db *gorm.DB) error {
	return db.AutoMigrate(
		&models.Product{},
		&models.Inventory{},
		&models.Reservation{},
		&models.ReservationItem{},
		&models.Outbox{},
		&models.ProcessedMessage{},
	)
}
