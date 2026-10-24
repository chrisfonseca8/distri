package health

import (
	"context"

	"gorm.io/gorm"
)

type Status struct {
	Healthy bool            `json:"healthy"`
	Checks  map[string]bool `json:"checks"`
}

// Check runs the database and server checks and returns the combined status.
func Check(ctx context.Context, db *gorm.DB, serverHealthURL string) Status {
	checks := map[string]func(context.Context) error{
		"database": func(ctx context.Context) error {
			return TestDB(ctx, db)
		},
		"server": func(ctx context.Context) error {
			return TestServer(ctx, serverHealthURL)
		},
	}

	status := Status{
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
