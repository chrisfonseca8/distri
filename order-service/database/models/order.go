package models

import (
	"time"
	"uuid"
)

type Order struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Status     string    `gorm:"type:text;not null;default:PENDING_PAYMENT;check:chk_orders_status,status IN ('PENDING_PAYMENT','PAID','CANCELLED','EXPIRED')"`
	TotalPaise int64     `gorm:"not null;check:chk_orders_total_positive,total_paise > 0"`
	Currency   string    `gorm:"type:varchar(3);not null;default:INR"`
	CreatedAt  time.Time `gorm:"type:timestamptz;not null;autoCreateTime"`
	UpdatedAt  time.Time `gorm:"type:timestamptz;not null;autoUpdateTime"`
}
