package services

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/order-service/database/models"
	"gorm.io/gorm"
)

var ErrOrderNotFound = errors.New("order not found")

type OrderItemResult struct {
	ID             uuid.UUID `json:"id"`
	ProductID      uuid.UUID `json:"product_id"`
	Quantity       int64     `json:"quantity"`
	UnitPricePaise int64     `json:"unit_price_paise"`
	CreatedAt      time.Time `json:"created_at"`
}

type OrderResult struct {
	ID         uuid.UUID         `json:"id"`
	Status     string            `json:"status"`
	TotalPaise int64             `json:"total_paise"`
	Currency   string            `json:"currency"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
	Items      []OrderItemResult `json:"items"`
}

// OrderService reads order data. Orders are created and moved between states
// only by the order command handlers; checkout is owned by the orchestrator.
type OrderService struct {
	db *gorm.DB
}

// NewOrderService creates the order service.
func NewOrderService(db *gorm.DB) *OrderService {
	return &OrderService{db: db}
}

// GetOrder returns an order and its items.
func (s *OrderService) GetOrder(ctx context.Context, orderID uuid.UUID) (OrderResult, error) {
	order, err := s.loadOrder(ctx, orderID)
	if err != nil {
		return OrderResult{}, err
	}

	var items []models.OrderItem
	query := s.db.WithContext(ctx).
		Table("orders.order_items").
		Where("order_id = ?", orderID).
		Order("created_at, id").
		Find(&items)
	if query.Error != nil {
		return OrderResult{}, fmt.Errorf("get order items: %w", query.Error)
	}

	result := OrderResult{
		ID:         order.ID,
		Status:     order.Status,
		TotalPaise: order.TotalPaise,
		Currency:   order.Currency,
		CreatedAt:  order.CreatedAt,
		UpdatedAt:  order.UpdatedAt,
		Items:      make([]OrderItemResult, 0, len(items)),
	}
	for _, item := range items {
		result.Items = append(result.Items, OrderItemResult{
			ID:             item.ID,
			ProductID:      item.ProductID,
			Quantity:       item.Quantity,
			UnitPricePaise: item.UnitPricePaise,
			CreatedAt:      item.CreatedAt,
		})
	}
	return result, nil
}

// loadOrder reads one order, returning ErrOrderNotFound if it does not exist.
func (s *OrderService) loadOrder(ctx context.Context, orderID uuid.UUID) (models.Order, error) {
	var order models.Order
	query := s.db.WithContext(ctx).
		Table("orders.orders").
		Where("id = ?", orderID).
		First(&order)
	if errors.Is(query.Error, gorm.ErrRecordNotFound) {
		return models.Order{}, ErrOrderNotFound
	}
	if query.Error != nil {
		return models.Order{}, fmt.Errorf("get order: %w", query.Error)
	}
	return order, nil
}
