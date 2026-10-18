package routes

import (
	"net/http"

	"github.com/chrisfonseca08/distri/inventory-service/pkg/controllers"
)

func RegisterProductRoutes(mux *http.ServeMux, controller *controllers.ProductController) {
	mux.HandleFunc("GET /v1/products/{productID}", controller.GetProduct)
}
