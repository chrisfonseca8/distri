package models

import (
	"time"
	"uuid"
)

type Saga struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey"`
	Status      string    `gorm:"type:text;not null;default:RUNNING;check:chk_sagas_status,status IN ('RUNNING','COMPLETED','COMPENSATING','COMPENSATED','STUCK')"`
	CurrentStep string    `gorm:"type:text;not null"`
	TotalPaise  int64     `gorm:"type:bigint;not null"`
	Currency    string    `gorm:"type:text;not null"`
	Version     int64     `gorm:"not null;default:1;check:chk_sagas_version_positive,version > 0"`
	DeadlineAt  time.Time `gorm:"type:timestamptz;not null"`
	CreatedAt   time.Time `gorm:"type:timestamptz;not null;autoCreateTime"`
	UpdatedAt   time.Time `gorm:"type:timestamptz;not null;autoUpdateTime"`
}
