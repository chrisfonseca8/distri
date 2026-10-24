package routes

import (
	"net/http"

	"github.com/chrisfonseca08/distri/inventory-service/pkg/controllers"
)

// RegisterProductRoutes adds the product endpoint to mux.
func RegisterProductRoutes(mux *http.ServeMux, controller *controllers.ProductController) {
	mux.HandleFunc("GET /v1/products/{productID}", controller.GetProduct)
}
