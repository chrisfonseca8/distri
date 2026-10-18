package controllers

import (
	"errors"
	"net/http"
	"uuid"

	"github.com/chrisfonseca08/distri/order-service/pkg/services"
)

type OrderController struct {
	service *services.OrderService
}

func NewOrderController(service *services.OrderService) *OrderController {
	return &OrderController{service: service}
}

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

func (c *OrderController) PayOrder(w http.ResponseWriter, r *http.Request) {
	orderID, ok := orderIDFromRequest(w, r)
	if !ok {
		return
	}

	result, err := c.service.SettleOrder(r.Context(), orderID)
	if err != nil {
		writeOrderError(w, err)
		return
	}

	statusCode := http.StatusOK
	if result.Status == "PENDING_PAYMENT" {
		statusCode = http.StatusAccepted
	}
	writeJSON(w, statusCode, result)
}

func orderIDFromRequest(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	orderID, err := uuid.Parse(r.PathValue("orderID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid order ID"})
		return uuid.Nil(), false
	}
	return orderID, true
}

func writeOrderError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, services.ErrOrderNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, services.ErrOrderInventoryNotFound),
		errors.Is(err, services.ErrOrderInventoryConflict),
		errors.Is(err, services.ErrOrderStatusConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, services.ErrOrderInventoryUnavailable):
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	case errors.Is(err, services.ErrOrderPaymentRejected),
		errors.Is(err, services.ErrOrderPaymentInconsistent):
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not process order"})
	}
}
