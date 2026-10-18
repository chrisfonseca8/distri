package services

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/chrisfonseca08/distri/inventory-service/database/models"
	"gorm.io/gorm"
)

var ErrProductNotFound = errors.New("product not found")

type ProductService struct {
	db *gorm.DB
}

func NewProductService(db *gorm.DB) *ProductService {
	return &ProductService{db: db}
}

func (s *ProductService) GetProduct(ctx context.Context, productID uuid.UUID) (models.Product, error) {
	var product models.Product
	result := s.db.WithContext(ctx).
		Table("inventory.products").
		First(&product, "id = ?", productID)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return models.Product{}, ErrProductNotFound
	}
	if result.Error != nil {
		return models.Product{}, fmt.Errorf("get product: %w", result.Error)
	}
	return product, nil
}
