package routes

import (
	"net/http"

	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/controllers"
)

func RegisterCheckoutRoutes(mux *http.ServeMux, controller *controllers.CheckoutController) {
	mux.HandleFunc("POST /v1/checkouts", controller.CreateCheckout)
	mux.HandleFunc("GET /v1/checkouts/{orderID}", controller.GetCheckout)
}
