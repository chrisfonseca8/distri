package routes

import (
	"net/http"

	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/health"
	"gorm.io/gorm"
)

// RegisterHealthRoutes adds the health and readiness endpoints to mux.
func RegisterHealthRoutes(mux *http.ServeMux, db *gorm.DB) {
	mux.HandleFunc("GET /health", health.ServerHandler)
	mux.HandleFunc("GET /readyz", health.ReadinessHandler(db))
}
