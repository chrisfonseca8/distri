package models

import (
	"time"
	"uuid"
)

type SagaStep struct {
	ID            uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	SagaID        uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex:uidx_saga_steps_saga_step,priority:1"`
	Saga          Saga       `gorm:"foreignKey:SagaID;references:ID;constraint:OnDelete:CASCADE"`
	Step          string     `gorm:"type:text;not null;uniqueIndex:uidx_saga_steps_saga_step,priority:2"`
	Status        string     `gorm:"type:text;not null;default:PENDING;check:chk_saga_steps_status,status IN ('PENDING','RUNNING','SUCCEEDED','FAILED','COMPENSATED','AMBIGUOUS')"`
	Attempts      int        `gorm:"not null;default:0;check:chk_saga_steps_attempts_nonnegative,attempts >= 0"`
	NextAttemptAt *time.Time `gorm:"type:timestamptz;column:next_attempt_at"`
	LastError     *string    `gorm:"type:text"`
	StartedAt     *time.Time `gorm:"type:timestamptz"`
	FinishedAt    *time.Time `gorm:"type:timestamptz"`
	CreatedAt     time.Time  `gorm:"type:timestamptz;not null;autoCreateTime"`
	UpdatedAt     time.Time  `gorm:"type:timestamptz;not null;autoUpdateTime"`
}
