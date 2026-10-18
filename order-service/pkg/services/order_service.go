package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/order-service/database/models"
	"github.com/chrisfonseca08/distri/order-service/pkg/clients"
	"github.com/chrisfonseca08/distri/order-service/pkg/utils/state"
	"gorm.io/gorm"
)

const (
	paymentWaitTimeout  = 4 * time.Second
	paymentPollInterval = 100 * time.Millisecond
)

var (
	ErrOrderNotFound             = errors.New("order not found")
	ErrOrderPaymentRejected      = errors.New("payment request was rejected")
	ErrOrderPaymentInconsistent  = errors.New("payment does not match the order")
	ErrOrderStatusConflict       = errors.New("payment conflicts with the final order status")
	ErrOrderInventoryNotFound    = errors.New("order reservation not found")
	ErrOrderInventoryConflict    = errors.New("order reservation status conflict")
	ErrOrderInventoryUnavailable = errors.New("order inventory is unavailable")
)

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

type OrderService struct {
	db              *gorm.DB
	paymentClient   clients.PaymentClient
	inventoryClient clients.InventoryClient
}

func NewOrderService(
	db *gorm.DB,
	paymentClient clients.PaymentClient,
	inventoryClient clients.InventoryClient,
) *OrderService {
	return &OrderService{
		db:              db,
		paymentClient:   paymentClient,
		inventoryClient: inventoryClient,
	}
}

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

func (s *OrderService) SettleOrder(ctx context.Context, orderID uuid.UUID) (OrderResult, error) {
	order, err := s.loadOrder(ctx, orderID)
	if err != nil {
		return OrderResult{}, err
	}

	switch state.OrderStatus(order.Status) {
	case state.OrderPaid, state.OrderCancelled, state.OrderExpired:
		if err := s.settleInventory(ctx, order); err != nil {
			return OrderResult{}, err
		}
	case state.OrderPendingPayment:
		order, err = s.settlePendingOrder(ctx, order)
		if err != nil {
			return OrderResult{}, err
		}
	default:
		return OrderResult{}, fmt.Errorf("unknown order status %q", order.Status)
	}

	return s.GetOrder(ctx, order.ID)
}

func (s *OrderService) settlePendingOrder(
	ctx context.Context,
	order models.Order,
) (models.Order, error) {
	payment, err := s.paymentClient.Create(
		ctx,
		order.ID,
		order.TotalPaise,
		order.Currency,
	)
	if err != nil {
		if errors.Is(err, clients.ErrPaymentUnavailable) {
			return order, nil
		}
		return models.Order{}, fmt.Errorf("%w: %v", ErrOrderPaymentRejected, err)
	}
	if err := validatePaymentForOrder(payment, order); err != nil {
		return models.Order{}, err
	}

	payment, err = s.waitForPayment(ctx, payment)
	if err != nil {
		if errors.Is(err, clients.ErrPaymentUnavailable) ||
			errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {
			return order, nil
		}
		return models.Order{}, fmt.Errorf("%w: %v", ErrOrderPaymentRejected, err)
	}
	if err := validatePaymentForOrder(payment, order); err != nil {
		return models.Order{}, err
	}

	var target state.OrderStatus
	switch payment.Status {
	case "SUCCEEDED":
		target = state.OrderPaid
	case "FAILED":
		target = state.OrderCancelled
	case "UNKNOWN", "CREATED", "PENDING":
		return order, nil
	default:
		return models.Order{}, fmt.Errorf("unknown payment status %q", payment.Status)
	}

	order, err = s.transitionOrder(ctx, order.ID, target, payment.Status)
	if err != nil {
		return models.Order{}, err
	}
	if err := s.settleInventory(ctx, order); err != nil {
		return models.Order{}, err
	}
	return order, nil
}

func (s *OrderService) waitForPayment(
	ctx context.Context,
	payment clients.Payment,
) (clients.Payment, error) {
	if payment.Status != "CREATED" && payment.Status != "PENDING" {
		return payment, nil
	}

	waitContext, cancel := context.WithTimeout(ctx, paymentWaitTimeout)
	defer cancel()

	ticker := time.NewTicker(paymentPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-waitContext.Done():
			return payment, waitContext.Err()
		case <-ticker.C:
			current, err := s.paymentClient.Get(waitContext, payment.ID)
			if err != nil {
				return payment, err
			}
			payment = current
			if payment.Status != "CREATED" && payment.Status != "PENDING" {
				return payment, nil
			}
		}
	}
}

func validatePaymentForOrder(payment clients.Payment, order models.Order) error {
	if payment.ID == uuid.Nil() ||
		payment.OrderID != order.ID ||
		payment.AmountPaise != order.TotalPaise ||
		payment.Currency != order.Currency {
		return ErrOrderPaymentInconsistent
	}
	return nil
}

func (s *OrderService) transitionOrder(
	ctx context.Context,
	orderID uuid.UUID,
	target state.OrderStatus,
	paymentStatus string,
) (models.Order, error) {
	if !state.CanTransition(state.OrderPendingPayment, target) {
		return models.Order{}, fmt.Errorf(
			"invalid order transition %s to %s",
			state.OrderPendingPayment,
			target,
		)
	}

	update := s.db.WithContext(ctx).
		Table("orders.orders").
		Where("id = ? AND status = ?", orderID, state.OrderPendingPayment).
		Updates(map[string]any{
			"status":     target,
			"updated_at": time.Now(),
		})
	if update.Error != nil {
		return models.Order{}, fmt.Errorf("change order status: %w", update.Error)
	}

	order, err := s.loadOrder(ctx, orderID)
	if err != nil {
		return models.Order{}, err
	}
	if update.RowsAffected == 0 && state.OrderStatus(order.Status) != target {
		slog.Error(
			"payment conflicts with final order status; manual refund review required",
			"order_id", orderID.String(),
			"payment_status", paymentStatus,
			"order_status", order.Status,
			"expected_order_status", target,
		)
		return models.Order{}, fmt.Errorf(
			"%w: payment is %s but order is %s",
			ErrOrderStatusConflict,
			paymentStatus,
			order.Status,
		)
	}

	return order, nil
}

func (s *OrderService) settleInventory(ctx context.Context, order models.Order) error {
	var (
		result         clients.ChangeReservationResult
		expectedStatus string
		err            error
	)
	switch state.OrderStatus(order.Status) {
	case state.OrderPaid:
		expectedStatus = "CONFIRMED"
		result, err = s.inventoryClient.Confirm(ctx, order.ID)
	case state.OrderCancelled, state.OrderExpired:
		expectedStatus = "RELEASED"
		result, err = s.inventoryClient.Release(ctx, order.ID)
	default:
		return nil
	}
	if err == nil && result.Status == expectedStatus {
		return nil
	}
	if err == nil {
		return fmt.Errorf(
			"%w: inventory returned reservation status %q",
			ErrOrderInventoryConflict,
			result.Status,
		)
	}

	switch {
	case errors.Is(err, clients.ErrReservationNotFound):
		return fmt.Errorf("%w: %v", ErrOrderInventoryNotFound, err)
	case errors.Is(err, clients.ErrReservationConflict),
		errors.Is(err, clients.ErrInvalidRequest):
		return fmt.Errorf("%w: %v", ErrOrderInventoryConflict, err)
	default:
		return fmt.Errorf("%w: %v", ErrOrderInventoryUnavailable, err)
	}
}

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
