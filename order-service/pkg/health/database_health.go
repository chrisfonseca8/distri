package health

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// TestDBHealth checks the existing application database connection.
func TestDBHealth(ctx context.Context, db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get database connection: %w", err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	return nil
}
