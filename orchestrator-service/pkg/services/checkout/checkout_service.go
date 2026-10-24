package checkout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/chrisfonseca08/distri/orchestrator-service/database/models"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/clients"
	"gorm.io/gorm"
)

type Response struct {
	StatusCode int
	Body       json.RawMessage
}

type CheckoutService struct {
	db              *gorm.DB
	inventoryClient clients.InventoryClient
	sagaDeadline    time.Duration
	stepTimeout     time.Duration
}

// NewCheckoutService creates the checkout service.
func NewCheckoutService(
	db *gorm.DB,
	inventoryClient clients.InventoryClient,
	sagaDeadline time.Duration,
	stepTimeout time.Duration,
) *CheckoutService {
	return &CheckoutService{
		db:              db,
		inventoryClient: inventoryClient,
		sagaDeadline:    sagaDeadline,
		stepTimeout:     stepTimeout,
	}
}

// CreateCheckout runs the first part of a checkout: claim the key, reserve stock, then start the saga.
func (s *CheckoutService) CreateCheckout(
	ctx context.Context,
	request CreateCheckoutInput,
) (Response, error) {
	prepared, err := prepareCheckoutRequest(request)
	if err != nil {
		return Response{}, err
	}

	attempt, _, err := s.claimCheckoutAttempt(ctx, prepared)
	if err != nil {
		return Response{}, err
	}
	if attempt.Status == "completed" {
		return responseFromAttempt(attempt)
	}
	if attempt.Status != "in_progress" {
		return Response{}, fmt.Errorf("unexpected checkout attempt status %q", attempt.Status)
	}

	reservation, err := s.reserveCheckoutInventory(ctx, attempt, prepared.Items)
	if err != nil {
		statusCode, definitive := definitiveInventoryError(err)
		if !definitive {
			return Response{}, err
		}

		response, encodeErr := errorResponse(statusCode, err)
		if encodeErr != nil {
			return Response{}, encodeErr
		}
		return s.completeDefinitiveFailure(ctx, attempt.IdempotencyKey, response)
	}

	response, err := s.createSagaFromReservation(ctx, attempt, reservation)
	if errors.Is(err, errConcurrentSagaWinner) {
		return s.loadCompletedResponse(ctx, attempt.IdempotencyKey)
	}
	return response, err
}

// errorResponse builds a JSON error response with the given status.
func errorResponse(statusCode int, responseErr error) (Response, error) {
	body, err := json.Marshal(map[string]string{"error": responseErr.Error()})
	if err != nil {
		return Response{}, fmt.Errorf("encode checkout error response: %w", err)
	}
	return Response{StatusCode: statusCode, Body: body}, nil
}

// responseFromAttempt rebuilds the stored response of a completed attempt.
func responseFromAttempt(attempt models.CheckoutAttempt) (Response, error) {
	if attempt.Status != "completed" || attempt.ResponseCode == nil || len(attempt.ResponseBody) == 0 {
		return Response{}, errors.New("checkout attempt has no completed response")
	}
	if !json.Valid(attempt.ResponseBody) {
		return Response{}, errors.New("checkout attempt has an invalid response body")
	}
	return Response{
		StatusCode: *attempt.ResponseCode,
		Body:       append(json.RawMessage(nil), attempt.ResponseBody...),
	}, nil
}

// definitiveInventoryError maps inventory errors that will never succeed on retry to an HTTP status.
func definitiveInventoryError(err error) (int, bool) {
	switch {
	case errors.Is(err, ErrInvalidCheckoutInput):
		return http.StatusBadRequest, true
	case errors.Is(err, ErrCheckoutProductNotFound):
		return http.StatusNotFound, true
	case errors.Is(err, ErrCheckoutInsufficientStock),
		errors.Is(err, ErrCheckoutReservationInactive):
		return http.StatusConflict, true
	default:
		return 0, false
	}
}
