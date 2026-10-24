package routes

import (
	"net/http"

	"github.com/chrisfonseca08/distri/order-service/pkg/controllers"
)

// RegisterOrderRoutes adds the order endpoints to mux.
func RegisterOrderRoutes(mux *http.ServeMux, controller *controllers.OrderController) {
	mux.HandleFunc("GET /v1/orders/{orderID}", controller.GetOrder)
}
