package health

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"gorm.io/gorm"
)

const readinessTimeout = 2 * time.Second

// ServerHandler reports that the process is up, without checking dependencies.
func ServerHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, Status{
		Healthy: true,
		Checks:  map[string]bool{"server": true},
	})
}

// ReadinessHandler reports ready only when the database answers within the timeout.
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

// writeJSON encodes value as JSON and writes it with the given status.
func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}
