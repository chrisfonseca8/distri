package controllers

import (
	"encoding/json"
	"errors"
	"net/http"
	"uuid"

	"github.com/chrisfonseca08/distri/payment-service/pkg/services"
)

type createPaymentRequest struct {
	OrderID     string `json:"order_id"`
	AmountPaise int64  `json:"amount_paise"`
	Currency    string `json:"currency"`
}

type PaymentController struct {
	service *services.PaymentService
}

func NewPaymentController(service *services.PaymentService) *PaymentController {
	return &PaymentController{service: service}
}

func (c *PaymentController) CreatePayment(w http.ResponseWriter, r *http.Request) {
	var request createPaymentRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	orderID, err := uuid.Parse(request.OrderID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid order ID"})
		return
	}

	result, err := c.service.CreatePayment(r.Context(), services.CreatePaymentInput{
		OrderID:     orderID,
		AmountPaise: request.AmountPaise,
		Currency:    request.Currency,
	})
	if errors.Is(err, services.ErrInvalidOrderID) ||
		errors.Is(err, services.ErrInvalidAmount) ||
		errors.Is(err, services.ErrInvalidCurrency) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not create payment"})
		return
	}

	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	writeJSON(w, status, result.Attempt)
}

func (c *PaymentController) GetPayment(w http.ResponseWriter, r *http.Request) {
	paymentID, err := uuid.Parse(r.PathValue("paymentID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payment ID"})
		return
	}

	attempt, err := c.service.GetPayment(r.Context(), paymentID)
	if errors.Is(err, services.ErrPaymentNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not get payment"})
		return
	}

	writeJSON(w, http.StatusOK, attempt)
}

func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}
