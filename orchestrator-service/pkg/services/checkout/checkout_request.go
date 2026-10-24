package checkout

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"uuid"

	"github.com/chrisfonseca08/distri/orchestrator-service/database/models"
)

var (
	ErrInvalidCheckoutInput = errors.New("invalid checkout input")
)

// The limits inventory enforces are checked here first, so the client gets a clean
// message and a request that can never succeed does not reach inventory at all.
const (
	maxIdempotencyKeyLength = 255
	maxCheckoutLines        = 20
	maxQuantityPerLine      = 100
)

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

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

// prepareCheckoutRequest validates the input and builds the attempt record and request hash.
func prepareCheckoutRequest(input CreateCheckoutInput) (preparedCheckoutRequest, error) {
	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	if idempotencyKey == "" {
		return preparedCheckoutRequest{}, fmt.Errorf("%w: idempotency key is required", ErrInvalidCheckoutInput)
	}
	if len(idempotencyKey) > maxIdempotencyKeyLength {
		return preparedCheckoutRequest{}, fmt.Errorf("%w: idempotency key must be at most %d characters", ErrInvalidCheckoutInput, maxIdempotencyKeyLength)
	}
	if !idempotencyKeyPattern.MatchString(idempotencyKey) {
		return preparedCheckoutRequest{}, fmt.Errorf("%w: idempotency key may contain only letters, digits, '.', '_', ':' and '-'", ErrInvalidCheckoutInput)
	}
	if len(input.Items) == 0 {
		return preparedCheckoutRequest{}, fmt.Errorf("%w: at least one item is required", ErrInvalidCheckoutInput)
	}
	if len(input.Items) > maxCheckoutLines {
		return preparedCheckoutRequest{}, fmt.Errorf("%w: at most %d items are allowed", ErrInvalidCheckoutInput, maxCheckoutLines)
	}

	items := slices.Clone(input.Items)
	slices.SortFunc(items, func(left, right CheckoutItemInput) int {
		return strings.Compare(left.ProductID.String(), right.ProductID.String())
	})

	for index, item := range items {
		if item.ProductID == uuid.Nil() || item.Quantity <= 0 {
			return preparedCheckoutRequest{}, fmt.Errorf("%w: every item needs a product_id and a quantity above zero", ErrInvalidCheckoutInput)
		}
		if item.Quantity > maxQuantityPerLine {
			return preparedCheckoutRequest{}, fmt.Errorf("%w: quantity per item must be at most %d", ErrInvalidCheckoutInput, maxQuantityPerLine)
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
