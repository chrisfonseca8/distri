package models

import (
	"time"
	"uuid"

	"gorm.io/datatypes"
)

type Job struct {
	ID          uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Type        string         `gorm:"type:text;not null;uniqueIndex:uidx_jobs_type_payment"`
	PaymentID   uuid.UUID      `gorm:"type:uuid;not null;uniqueIndex:uidx_jobs_type_payment"`
	Payload     datatypes.JSON `gorm:"type:jsonb"`
	Status      string         `gorm:"type:text;not null;default:queued;check:chk_jobs_status,status IN ('queued','running','succeeded','dead')"`
	Attempts    int            `gorm:"not null;default:0;check:chk_jobs_attempts_nonnegative,attempts >= 0"`
	MaxAttempts int            `gorm:"not null;default:5;check:chk_jobs_max_attempts_positive,max_attempts > 0"`
	NextRunAt   time.Time      `gorm:"type:timestamptz;not null;default:now();index:idx_jobs_claim,where:status IN ('queued'\\,'running')"`
	LockedUntil *time.Time     `gorm:"type:timestamptz"`
	LockedBy    *string        `gorm:"type:text"`
	LastError   *string        `gorm:"type:text"`
	CreatedAt   time.Time      `gorm:"type:timestamptz;not null;autoCreateTime"`
	UpdatedAt   time.Time      `gorm:"type:timestamptz;not null;autoUpdateTime"`
}
