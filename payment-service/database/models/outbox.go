package models

import (
	"time"
	"uuid"

	"gorm.io/datatypes"
)

type Outbox struct {
	ID            uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	MessageID     uuid.UUID      `gorm:"type:uuid;not null;uniqueIndex:uidx_outbox_message_id"`
	RoutingKey    string         `gorm:"type:text;not null"`
	Payload       datatypes.JSON `gorm:"type:jsonb;not null"`
	Attempts      int            `gorm:"not null;default:0;check:chk_outbox_attempts_nonnegative,attempts >= 0"`
	NextAttemptAt time.Time      `gorm:"type:timestamptz;not null;default:now();index:idx_outbox_relay,priority:1,where:published_at IS NULL"`
	LockedBy      *string        `gorm:"type:text"`
	LockedUntil   *time.Time     `gorm:"type:timestamptz"`
	LastError     *string        `gorm:"type:text"`
	PublishedAt   *time.Time     `gorm:"type:timestamptz"`
	CreatedAt     time.Time      `gorm:"type:timestamptz;not null;autoCreateTime;index:idx_outbox_relay,priority:2"`
}

func (Outbox) TableName() string {
	return "payments.outbox"
}
