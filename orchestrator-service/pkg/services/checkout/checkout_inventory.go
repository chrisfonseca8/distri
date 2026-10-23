package checkout

import (
	"context"
	"errors"
	"fmt"

	"github.com/chrisfonseca08/distri/orchestrator-service/database/models"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/clients"
)

var (
	ErrCheckoutProductNotFound      = errors.New("checkout product not found")
	ErrCheckoutInsufficientStock    = errors.New("checkout has insufficient stock")
	ErrCheckoutReservationInactive  = errors.New("checkout reservation is not active")
	ErrCheckoutInventoryUnavailable = errors.New("checkout inventory is unavailable")
)

func (s *CheckoutService) reserveCheckoutInventory(
	ctx context.Context,
	attempt models.CheckoutAttempt,
	items []CheckoutItemInput,
) (clients.ReserveResult, error) {
	if attempt.OrderID == nil {
		return clients.ReserveResult{}, errors.New("checkout attempt has no order ID")
	}

	reserveItems := make([]clients.ReserveItem, 0, len(items))
	for _, item := range items {
		reserveItems = append(reserveItems, clients.ReserveItem{
			ProductID: item.ProductID,
			Quantity:  item.Quantity,
		})
	}

	result, err := s.inventoryClient.Reserve(
		ctx,
		*attempt.OrderID,
		attempt.RequestHash,
		reserveItems,
	)
	if err == nil {
		return result, nil
	}

	switch {
	case errors.Is(err, clients.ErrInvalidRequest):
		return clients.ReserveResult{}, fmt.Errorf("%w: %v", ErrInvalidCheckoutInput, err)
	case errors.Is(err, clients.ErrProductNotFound):
		return clients.ReserveResult{}, fmt.Errorf("%w: %v", ErrCheckoutProductNotFound, err)
	case errors.Is(err, clients.ErrInsufficientStock):
		return clients.ReserveResult{}, fmt.Errorf("%w: %v", ErrCheckoutInsufficientStock, err)
	case errors.Is(err, clients.ErrReservationNotReserved):
		return clients.ReserveResult{}, fmt.Errorf("%w: %v", ErrCheckoutReservationInactive, err)
	default:
		return clients.ReserveResult{}, fmt.Errorf("%w: %v", ErrCheckoutInventoryUnavailable, err)
	}
}
