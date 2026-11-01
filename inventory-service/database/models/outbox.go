package models

import (
	"time"
	"uuid"

	"gorm.io/datatypes"
)

type Outbox struct {
	ID                    uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	MessageID             uuid.UUID      `gorm:"type:uuid;not null;uniqueIndex:uidx_outbox_message_id"`
	RoutingKey            string         `gorm:"type:text;not null"`
	Payload               datatypes.JSON `gorm:"type:jsonb;not null"`
	Attempts              int            `gorm:"not null;default:0;check:chk_outbox_attempts_nonnegative,attempts >= 0"`
	UnpublishableAttempts int            `gorm:"not null;default:0;check:chk_outbox_unpublishable_attempts_nonnegative,unpublishable_attempts >= 0"`
	NextAttemptAt         time.Time      `gorm:"type:timestamptz;not null;default:now();index:idx_outbox_relay,priority:1,where:published_at IS NULL"`
	LockedBy              *string        `gorm:"type:text"`
	LockedUntil           *time.Time     `gorm:"type:timestamptz"`
	LastError             *string        `gorm:"type:text"`
	PublishedAt           *time.Time     `gorm:"type:timestamptz"`
	DeadAt                *time.Time     `gorm:"type:timestamptz"`
	// Correlation carries test_run_id, saga_id, order_id and the W3C trace context
	// captured when the row was written, so the relay can resume them on publish.
	Correlation datatypes.JSON `gorm:"type:jsonb"`
	CreatedAt   time.Time      `gorm:"type:timestamptz;not null;autoCreateTime;index:idx_outbox_relay,priority:2"`
}

// TableName puts the outbox table in the inventory schema.
func (Outbox) TableName() string {
	return "inventory.outbox"
}
