package config

import (
	"fmt"
	"os"
	"strings"

	"gorm.io/gorm"
)

// RequireDedicatedTestDB makes an integration test refuse to run against a database a
// running service may be relaying from. These tests write outbox rows; a live relay would
// publish them as real messages for orders nobody knows (the 2026-10-24 dead-queue
// investigation). Set ALLOW_SHARED_TEST_DB=1 only if no relay runs against the database.
func RequireDedicatedTestDB(db *gorm.DB) error {
	var name string
	if err := db.Raw("SELECT current_database()").Scan(&name).Error; err != nil {
		return fmt.Errorf("read database name: %w", err)
	}
	if !strings.HasSuffix(name, "_test") && os.Getenv("ALLOW_SHARED_TEST_DB") != "1" {
		return fmt.Errorf("database %q is not a dedicated *_test database; set ALLOW_SHARED_TEST_DB=1 only if no relay runs against it", name)
	}
	return nil
}
