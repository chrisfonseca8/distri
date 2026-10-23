package checkout

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"uuid"

	"github.com/chrisfonseca08/distri/orchestrator-service/database/models"
)

var (
	ErrInvalidCheckoutInput = errors.New("invalid checkout input")
)

type CheckoutItemInput struct {
	ProductID uuid.UUID `json:"product_id"`
	Quantity  int64     `json:"quantity"`
}

type CreateCheckoutInput struct {
	IdempotencyKey string              `json:"-"`
	Items          []CheckoutItemInput `json:"items"`
}

type CreateCheckoutResult struct {
	OrderID    uuid.UUID `json:"order_id"`
	Status     string    `json:"status"`
	TotalPaise int64     `json:"total_paise"`
	Currency   string    `json:"currency"`
}

type preparedCheckoutRequest struct {
	Attempt models.CheckoutAttempt
	Items   []CheckoutItemInput
}

func prepareCheckoutRequest(input CreateCheckoutInput) (preparedCheckoutRequest, error) {
	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	if idempotencyKey == "" {
		return preparedCheckoutRequest{}, fmt.Errorf("%w: idempotency key is required", ErrInvalidCheckoutInput)
	}
	if len(input.Items) == 0 {
		return preparedCheckoutRequest{}, fmt.Errorf("%w: at least one item is required", ErrInvalidCheckoutInput)
	}

	items := slices.Clone(input.Items)
	slices.SortFunc(items, func(left, right CheckoutItemInput) int {
		return strings.Compare(left.ProductID.String(), right.ProductID.String())
	})

	for index, item := range items {
		if item.ProductID == uuid.Nil() || item.Quantity <= 0 {
			return preparedCheckoutRequest{}, ErrInvalidCheckoutInput
		}
		if index > 0 && item.ProductID == items[index-1].ProductID {
			return preparedCheckoutRequest{}, fmt.Errorf(
				"%w: duplicate product %s",
				ErrInvalidCheckoutInput,
				item.ProductID,
			)
		}
	}

	canonicalBody, err := json.Marshal(items)
	if err != nil {
		return preparedCheckoutRequest{}, fmt.Errorf("marshal canonical checkout request: %w", err)
	}

	hash := sha256.Sum256(canonicalBody)
	return preparedCheckoutRequest{
		Attempt: models.CheckoutAttempt{
			IdempotencyKey: idempotencyKey,
			RequestHash:    fmt.Sprintf("%x", hash),
			Status:         "in_progress",
		},
		Items: items,
	}, nil
}
