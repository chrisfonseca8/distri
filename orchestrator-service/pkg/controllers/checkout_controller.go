package controllers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/services/checkout"
)

const maxCheckoutRequestBody = 8 << 10

type CheckoutController struct {
	service *checkout.CheckoutService
}

func NewCheckoutController(service *checkout.CheckoutService) *CheckoutController {
	return &CheckoutController{service: service}
}

func (c *CheckoutController) CreateCheckout(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxCheckoutRequestBody)
	input, err := decodeCheckoutInput(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	response, err := c.service.CreateCheckout(r.Context(), input)
	if err != nil {
		writeCheckoutError(w, err)
		return
	}
	writeRawJSON(w, response.StatusCode, response.Body)
}

func decodeCheckoutInput(r *http.Request) (checkout.CreateCheckoutInput, error) {
	input := checkout.CreateCheckoutInput{
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
	}

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return checkout.CreateCheckoutInput{}, err
	}
	return input, nil
}

func writeCheckoutError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, checkout.ErrInvalidCheckoutInput):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, checkout.ErrIdempotencyKeyConflict):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
	case errors.Is(err, checkout.ErrCheckoutInventoryUnavailable):
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	default:
		slog.Error("create checkout", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not create checkout"})
	}
}

func writeRawJSON(w http.ResponseWriter, statusCode int, body json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_, _ = w.Write(body)
}

func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}
