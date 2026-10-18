package routes

import (
	"net/http"

	"github.com/chrisfonseca08/distri/order-service/pkg/controllers"
)

func RegisterCheckoutRoutes(mux *http.ServeMux, controller *controllers.CheckoutController) {
	mux.HandleFunc("POST /v1/checkouts", controller.CreateCheckout)
}
