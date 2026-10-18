package controllers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/chrisfonseca08/distri/order-service/pkg/services"
)

type CheckoutController struct {
	service *services.CheckoutService
}

func NewCheckoutController(service *services.CheckoutService) *CheckoutController {
	return &CheckoutController{service: service}
}

func (c *CheckoutController) CreateCheckout(w http.ResponseWriter, r *http.Request) {
	input, err := decodeCheckoutInput(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	result, err := c.service.CreateCheckout(r.Context(), input)
	if err != nil {
		writeCheckoutError(w, err)
		return
	}

	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	writeJSON(w, status, result)
}

func decodeCheckoutInput(r *http.Request) (services.CreateCheckoutInput, error) {
	input := services.CreateCheckoutInput{
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
	}

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return services.CreateCheckoutInput{}, err
	}

	return input, nil
}

func writeCheckoutError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, services.ErrInvalidCheckoutInput):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, services.ErrIdempotencyKeyConflict):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
	case errors.Is(err, services.ErrCheckoutProductNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, services.ErrCheckoutInsufficientStock),
		errors.Is(err, services.ErrCheckoutReservationInactive):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, services.ErrCheckoutInventoryUnavailable):
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not create checkout"})
	}
}

func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}
