package controllers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"uuid"

	"github.com/chrisfonseca08/distri/order-service/pkg/services"
)

type OrderController struct {
	service *services.OrderService
}

// NewOrderController creates the order HTTP controller.
func NewOrderController(service *services.OrderService) *OrderController {
	return &OrderController{service: service}
}

// GetOrder handles GET /v1/orders/{orderID} and replies with the order and its items.
func (c *OrderController) GetOrder(w http.ResponseWriter, r *http.Request) {
	orderID, ok := orderIDFromRequest(w, r)
	if !ok {
		return
	}

	result, err := c.service.GetOrder(r.Context(), orderID)
	if err != nil {
		writeOrderError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// orderIDFromRequest parses the order ID from the URL, replying 400 if it is not a valid UUID.
func orderIDFromRequest(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	orderID, err := uuid.Parse(r.PathValue("orderID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid order ID"})
		return uuid.Nil(), false
	}
	return orderID, true
}

// writeOrderError turns an order error into the matching HTTP status and message.
func writeOrderError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, services.ErrOrderNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not process order"})
	}
}

// writeJSON encodes value as JSON and writes it with the given status.
func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("write response", "error", err)
	}
}
