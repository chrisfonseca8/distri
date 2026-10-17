package routes

import (
	"encoding/json"
	"errors"
	"net/http"
	"uuid"

	"github.com/chrisfonseca08/distri/payment-service/database/models"
	"gorm.io/gorm"
)

func RegisterPaymentRoutes(mux *http.ServeMux, db *gorm.DB) {
	mux.HandleFunc("GET /v1/payments/{paymentID}", func(w http.ResponseWriter, r *http.Request) {
		paymentID, err := uuid.Parse(r.PathValue("paymentID"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payment ID"})
			return
		}

		var attempt models.PaymentAttempt
		result := db.WithContext(r.Context()).First(&attempt, "id = ?", paymentID)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "payment not found"})
			return
		}
		if result.Error != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not get payment"})
			return
		}

		writeJSON(w, http.StatusOK, attempt)
	})
}

func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}
