package controllers

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/inventory-service/pkg/services"
)

type productResponse struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	PricePaise int64     `json:"price_paise"`
	Currency   string    `json:"currency"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type ProductController struct {
	service *services.ProductService
}

// NewProductController creates the product HTTP controller.
func NewProductController(service *services.ProductService) *ProductController {
	return &ProductController{service: service}
}

// GetProduct handles GET /v1/products/{productID} and replies with the product.
func (c *ProductController) GetProduct(w http.ResponseWriter, r *http.Request) {
	productID, err := uuid.Parse(r.PathValue("productID"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid product ID"})
		return
	}

	product, err := c.service.GetProduct(r.Context(), productID)
	if errors.Is(err, services.ErrProductNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
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
}

// writeJSON encodes value as JSON and writes it with the given status.
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
