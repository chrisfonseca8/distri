package health

import (
	"context"

	"gorm.io/gorm"
)

// HealthStatus reports the overall health and the result of each named check.
type HealthStatus struct {
	Healthy bool            `json:"healthy"`
	Checks  map[string]bool `json:"checks"`
}

// HealthCheck runs both the database and HTTP server checks.
func HealthCheck(ctx context.Context, db *gorm.DB, serverHealthURL string) HealthStatus {
	checks := map[string]func(context.Context) error{
		"database": func(ctx context.Context) error {
			return TestDBHealth(ctx, db)
		},
		"server": func(ctx context.Context) error {
			return TestServerHealth(ctx, serverHealthURL)
		},
	}

	status := HealthStatus{
		Healthy: true,
		Checks:  make(map[string]bool, len(checks)),
	}

	for name, check := range checks {
		err := check(ctx)
		status.Checks[name] = err == nil
		if err != nil {
			status.Healthy = false
		}
	}

	return status
}
