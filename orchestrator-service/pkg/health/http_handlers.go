package health

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"gorm.io/gorm"
)

const readinessTimeout = 2 * time.Second

func ServerHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, Status{
		Healthy: true,
		Checks:  map[string]bool{"server": true},
	})
}

func ReadinessHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, request *http.Request) {
		ctx, cancel := context.WithTimeout(request.Context(), readinessTimeout)
		defer cancel()

		databaseHealthy := CheckDatabase(ctx, db) == nil
		statusCode := http.StatusOK
		if !databaseHealthy {
			statusCode = http.StatusServiceUnavailable
		}

		writeJSON(w, statusCode, Status{
			Healthy: databaseHealthy,
			Checks:  map[string]bool{"database": databaseHealthy},
		})
	}
}

func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}
