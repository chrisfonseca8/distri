package seeding

import (
	"fmt"
	"uuid"

	"github.com/chrisfonseca08/distri/order-service/database/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SeedProductInventory inserts or updates the three test products and their
// inventory records. Fixed IDs make the seed safe to run more than once.
func SeedProductInventory(db *gorm.DB) error {
	products := []models.Product{
		{
			ID:         uuid.MustParse("10000000-0000-4000-8000-000000000001"),
			Name:       "Wireless Mouse",
			PricePaise: 149900,
			Currency:   "INR",
		},
		{
			ID:         uuid.MustParse("10000000-0000-4000-8000-000000000002"),
			Name:       "Mechanical Keyboard",
			PricePaise: 499900,
			Currency:   "INR",
		},
		{
			ID:         uuid.MustParse("10000000-0000-4000-8000-000000000003"),
			Name:       "USB-C Hub",
			PricePaise: 249900,
			Currency:   "INR",
		},
	}

	inventories := []models.Inventory{
		{ProductID: products[0].ID, OnHand: 100, Reserved: 10, Allocated: 5},
		{ProductID: products[1].ID, OnHand: 50, Reserved: 5, Allocated: 2},
		{ProductID: products[2].ID, OnHand: 75, Reserved: 8, Allocated: 4},
	}

	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL search_path TO orders").Error; err != nil {
			return fmt.Errorf("select orders schema: %w", err)
		}

		if err := tx.Omit("Inventory").Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{"name", "price_paise", "currency", "updated_at"}),
		}).Create(&products).Error; err != nil {
			return fmt.Errorf("seed products: %w", err)
		}

		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "product_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"on_hand", "reserved", "allocated", "updated_at"}),
		}).Create(&inventories).Error; err != nil {
			return fmt.Errorf("seed inventories: %w", err)
		}

		ids := []uuid.UUID{products[0].ID, products[1].ID, products[2].ID}
		var productCount int64
		if err := tx.Model(&models.Product{}).Where("id IN ?", ids).Count(&productCount).Error; err != nil {
			return fmt.Errorf("verify products: %w", err)
		}

		var inventoryCount int64
		if err := tx.Model(&models.Inventory{}).Where("product_id IN ?", ids).Count(&inventoryCount).Error; err != nil {
			return fmt.Errorf("verify inventories: %w", err)
		}

		if productCount != 3 || inventoryCount != 3 {
			return fmt.Errorf("verify seed: got %d products and %d inventories", productCount, inventoryCount)
		}

		return nil
	})
}
