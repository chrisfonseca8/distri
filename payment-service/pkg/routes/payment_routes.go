package routes

import (
	"net/http"

	"github.com/chrisfonseca08/distri/payment-service/pkg/controllers"
)

// RegisterPaymentRoutes adds the payment endpoints to mux.
func RegisterPaymentRoutes(mux *http.ServeMux, controller *controllers.PaymentController) {
	mux.HandleFunc("POST /v1/payments", controller.CreatePayment)
	mux.HandleFunc("GET /v1/payments/{paymentID}", controller.GetPayment)
}
