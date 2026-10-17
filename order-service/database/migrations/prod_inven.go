package migrations

import (
	"github.com/chrisfonseca08/distri/order-service/database/models"
	"gorm.io/gorm"
)

func MigrateProductInventory(db *gorm.DB) error {
	return db.AutoMigrate(
		&models.Product{},
		&models.Inventory{},
	)

}
