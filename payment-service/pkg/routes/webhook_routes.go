package routes

import "net/http"

func RegisterWebhookRoutes(mux *http.ServeMux, handler http.Handler) {
	mux.Handle("POST /v1/webhooks/razorpay", handler)
}
