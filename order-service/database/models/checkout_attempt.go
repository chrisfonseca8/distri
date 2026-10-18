package models

import (
	"time"
	"uuid"

	"gorm.io/datatypes"
)

type CheckoutAttempt struct {
	IdempotencyKey string         `gorm:"type:text;primaryKey"`
	RequestHash    string         `gorm:"type:text;not null"`
	Status         string         `gorm:"type:text;not null;default:in_progress;check:chk_checkout_attempts_status,status IN ('in_progress','completed')"`
	OrderID        *uuid.UUID     `gorm:"type:uuid"`
	ResponseCode   *int           `gorm:"type:integer;check:chk_checkout_attempts_completed_response,status = 'in_progress' OR response_code IS NOT NULL"`
	ResponseBody   datatypes.JSON `gorm:"type:jsonb"`
	CreatedAt      time.Time      `gorm:"type:timestamptz;not null;autoCreateTime"`
	UpdatedAt      time.Time      `gorm:"type:timestamptz;not null;autoUpdateTime"`
}
