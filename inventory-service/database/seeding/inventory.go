package seeding

import (
	"fmt"
	"uuid"

	"github.com/chrisfonseca08/distri/inventory-service/database/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const InventorySeedCount = 50

type productSpec struct {
	name       string
	pricePaise int64
}

func SeedInventory(db *gorm.DB) error {
	specs := []productSpec{
		{name: "Wireless Mouse", pricePaise: 149900},
		{name: "Mechanical Keyboard", pricePaise: 499900},
		{name: "USB-C Hub", pricePaise: 249900},
		{name: "Laptop Stand", pricePaise: 199900},
		{name: "Noise-Cancelling Headphones", pricePaise: 899900},
		{name: "Webcam 1080p", pricePaise: 329900},
		{name: "Portable SSD 1TB", pricePaise: 749900},
		{name: "Bluetooth Speaker", pricePaise: 279900},
		{name: "Smartphone Tripod", pricePaise: 99900},
		{name: "Ergonomic Office Chair", pricePaise: 1299900},
		{name: "Desk Lamp", pricePaise: 179900},
		{name: "Monitor 24 Inch", pricePaise: 1099900},
		{name: "Monitor Arm", pricePaise: 349900},
		{name: "USB-C Cable", pricePaise: 69900},
		{name: "Wireless Charger", pricePaise: 159900},
		{name: "Power Bank 20000mAh", pricePaise: 249900},
		{name: "Smart Watch", pricePaise: 599900},
		{name: "Fitness Band", pricePaise: 299900},
		{name: "Tablet Sleeve", pricePaise: 89900},
		{name: "Laptop Backpack", pricePaise: 229900},
		{name: "Gaming Controller", pricePaise: 399900},
		{name: "Gaming Headset", pricePaise: 449900},
		{name: "Desk Mat", pricePaise: 119900},
		{name: "Screen Cleaning Kit", pricePaise: 49900},
		{name: "HDMI Cable", pricePaise: 79900},
		{name: "DisplayPort Cable", pricePaise: 99900},
		{name: "Ethernet Cable", pricePaise: 59900},
		{name: "Wi-Fi Router", pricePaise: 349900},
		{name: "Mesh Wi-Fi System", pricePaise: 999900},
		{name: "Surge Protector", pricePaise: 129900},
		{name: "Smart Plug", pricePaise: 89900},
		{name: "LED Light Strip", pricePaise: 139900},
		{name: "Action Camera", pricePaise: 1199900},
		{name: "Camera Backpack", pricePaise: 299900},
		{name: "Ring Light", pricePaise: 189900},
		{name: "Podcast Microphone", pricePaise: 549900},
		{name: "Audio Interface", pricePaise: 699900},
		{name: "Studio Headphones", pricePaise: 499900},
		{name: "E-Reader", pricePaise: 999900},
		{name: "Digital Drawing Tablet", pricePaise: 649900},
		{name: "Portable Projector", pricePaise: 1499900},
		{name: "Mini PC", pricePaise: 2499900},
		{name: "Raspberry Pi Case", pricePaise: 129900},
		{name: "Precision Screwdriver Set", pricePaise: 109900},
		{name: "Cable Organizer", pricePaise: 49900},
		{name: "Keyboard Wrist Rest", pricePaise: 79900},
		{name: "Mouse Pad", pricePaise: 59900},
		{name: "Phone Stand", pricePaise: 69900},
		{name: "USB Flash Drive 128GB", pricePaise: 99900},
		{name: "Memory Card 256GB", pricePaise: 189900},
	}
	if len(specs) != InventorySeedCount {
		return fmt.Errorf("expected %d product specs, got %d", InventorySeedCount, len(specs))
	}

	products := make([]models.Product, 0, InventorySeedCount)
	inventories := make([]models.Inventory, 0, InventorySeedCount)
	productIDs := make([]uuid.UUID, 0, InventorySeedCount)

	for index, spec := range specs {
		productID := uuid.MustParse(fmt.Sprintf(
			"10000000-0000-4000-8000-%012d",
			index+1,
		))
		products = append(products, models.Product{
			ID:         productID,
			Name:       spec.name,
			PricePaise: spec.pricePaise,
			Currency:   "INR",
		})
		inventories = append(inventories, models.Inventory{
			ProductID: productID,
			OnHand:    int64(40 + (index*13)%161),
			Reserved:  0,
			Allocated: 0,
		})
		productIDs = append(productIDs, productID)
	}

	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL search_path TO inventory").Error; err != nil {
			return fmt.Errorf("select inventory schema: %w", err)
		}

		if err := tx.Omit("Inventory").Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"name",
				"price_paise",
				"currency",
				"updated_at",
			}),
		}).Create(&products).Error; err != nil {
			return fmt.Errorf("seed products: %w", err)
		}

		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "product_id"}},
			DoNothing: true,
		}).Create(&inventories).Error; err != nil {
			return fmt.Errorf("seed inventories: %w", err)
		}

		var productCount int64
		if err := tx.Model(&models.Product{}).
			Where("id IN ?", productIDs).
			Count(&productCount).Error; err != nil {
			return fmt.Errorf("verify products: %w", err)
		}

		var inventoryCount int64
		if err := tx.Model(&models.Inventory{}).
			Where("product_id IN ?", productIDs).
			Count(&inventoryCount).Error; err != nil {
			return fmt.Errorf("verify inventories: %w", err)
		}
		if productCount != InventorySeedCount || inventoryCount != InventorySeedCount {
			return fmt.Errorf(
				"verify seed: got %d products and %d inventories",
				productCount,
				inventoryCount,
			)
		}

		return nil
	})
}
