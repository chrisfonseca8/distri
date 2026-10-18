package seeding

import (
	"fmt"
	"uuid"

	"github.com/chrisfonseca08/distri/order-service/database/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const ProductInventorySeedCount = 50

type productSpec struct {
	name       string
	pricePaise int64
}

// SeedProductInventory inserts or updates the test products and their
// inventory records. Fixed IDs make the seed safe to run more than once.
func SeedProductInventory(db *gorm.DB) error {
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

	if len(specs) != ProductInventorySeedCount {
		return fmt.Errorf("expected %d product specs, got %d", ProductInventorySeedCount, len(specs))
	}

	products := make([]models.Product, 0, len(specs))
	inventories := make([]models.Inventory, 0, len(specs))
	ids := make([]uuid.UUID, 0, len(specs))
	for index, spec := range specs {
		id := uuid.MustParse(fmt.Sprintf("10000000-0000-4000-8000-%012d", index+1))
		products = append(products, models.Product{
			ID:         id,
			Name:       spec.name,
			PricePaise: spec.pricePaise,
			Currency:   "INR",
		})
		inventories = append(inventories, models.Inventory{
			ProductID: id,
			OnHand:    int64(40 + (index*13)%161),
			Reserved:  int64((index * 3) % 15),
			Allocated: int64((index * 2) % 10),
		})
		ids = append(ids, id)
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

		var productCount int64
		if err := tx.Model(&models.Product{}).Where("id IN ?", ids).Count(&productCount).Error; err != nil {
			return fmt.Errorf("verify products: %w", err)
		}

		var inventoryCount int64
		if err := tx.Model(&models.Inventory{}).Where("product_id IN ?", ids).Count(&inventoryCount).Error; err != nil {
			return fmt.Errorf("verify inventories: %w", err)
		}

		if productCount != int64(len(products)) || inventoryCount != int64(len(inventories)) {
			return fmt.Errorf("verify seed: got %d products and %d inventories", productCount, inventoryCount)
		}

		return nil
	})
}
