package routes

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/order-service/database/models"
	"gorm.io/gorm"
)

type productResponse struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	PricePaise int64     `json:"price_paise"`
	Currency   string    `json:"currency"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// RegisterProductRoutes registers the product HTTP routes.
func RegisterProductRoutes(mux *http.ServeMux, db *gorm.DB) {
	mux.HandleFunc("GET /products/{productID}", func(w http.ResponseWriter, r *http.Request) {
		productID, err := uuid.Parse(r.PathValue("productID"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid product ID"})
			return
		}

		var product models.Product
		result := db.WithContext(r.Context()).Table("orders.products").First(&product, "id = ?", productID)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "product not found"})
			return
		}
		if result.Error != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not get product"})
			return
		}

		writeJSON(w, http.StatusOK, productResponse{
			ID:         product.ID,
			Name:       product.Name,
			PricePaise: product.PricePaise,
			Currency:   product.Currency,
			CreatedAt:  product.CreatedAt,
			UpdatedAt:  product.UpdatedAt,
		})
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
