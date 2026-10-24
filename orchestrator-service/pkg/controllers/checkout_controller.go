package controllers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"uuid"

	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/services/checkout"
)

const maxCheckoutRequestBody = 8 << 10

type CheckoutController struct {
	service *checkout.CheckoutService
}

// NewCheckoutController creates the checkout HTTP controller.
func NewCheckoutController(service *checkout.CheckoutService) *CheckoutController {
	return &CheckoutController{service: service}
}

// CreateCheckout handles POST /v1/checkouts and replies with the stored result of the checkout.
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

// GetCheckout handles GET /v1/checkouts/{orderID} and replies with the checkout's current state.
func (c *CheckoutController) GetCheckout(w http.ResponseWriter, r *http.Request) {
	orderID, err := uuid.Parse(r.PathValue("orderID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid order ID"})
		return
	}

	result, err := c.service.GetCheckout(r.Context(), orderID)
	if err != nil {
		switch {
		case errors.Is(err, checkout.ErrCheckoutNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		default:
			slog.Error("get checkout", "order_id", orderID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not get checkout"})
		}
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// decodeCheckoutInput reads the Idempotency-Key header and JSON body, rejecting unknown fields.
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

// writeCheckoutError turns a checkout error into the matching HTTP status and message.
func writeCheckoutError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, checkout.ErrInvalidCheckoutInput):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, checkout.ErrIdempotencyKeyConflict):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
	case errors.Is(err, checkout.ErrCheckoutInventoryUnavailable):
		slog.Warn("checkout inventory unavailable", "error", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "checkout inventory is unavailable, retry with the same idempotency key"})
	default:
		slog.Error("create checkout", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not create checkout"})
	}
}

// writeRawJSON writes an already-encoded JSON body with the given status.
func writeRawJSON(w http.ResponseWriter, statusCode int, body json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_, _ = w.Write(body)
}

// writeJSON encodes value as JSON and writes it with the given status.
func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}
