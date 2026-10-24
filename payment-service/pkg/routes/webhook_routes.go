package routes

import "net/http"

// RegisterWebhookRoutes adds the Razorpay webhook endpoint to mux.
func RegisterWebhookRoutes(mux *http.ServeMux, handler http.Handler) {
	mux.Handle("POST /v1/webhooks/razorpay", handler)
}
