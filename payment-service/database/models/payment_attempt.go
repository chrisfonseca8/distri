package models

import (
	"time"
	"uuid"
)

type PaymentAttempt struct {
	ID               uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrderID          uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_payment_attempts_order_id" json:"order_id"`
	AmountPaise      int64     `gorm:"not null;check:chk_payment_attempts_amount_positive,amount_paise > 0" json:"amount_paise"`
	Currency         string    `gorm:"type:varchar(3);not null;default:INR" json:"currency"`
	Status           string    `gorm:"type:text;not null;check:chk_payment_attempts_status,status IN ('CREATED','PENDING','SUCCEEDED','FAILED','UNKNOWN')" json:"status"`
	Gateway          string    `gorm:"type:text;not null" json:"gateway"`
	GatewayOrderID   *string   `gorm:"type:text" json:"gateway_order_id"`
	GatewayPaymentID *string   `gorm:"type:text" json:"gateway_payment_id"`
	FailureCode      *string   `gorm:"type:text" json:"failure_code"`
	CreatedAt        time.Time `gorm:"type:timestamptz;not null;autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time `gorm:"type:timestamptz;not null;autoUpdateTime" json:"updated_at"`
}

func (PaymentAttempt) TableName() string {
	return "payments.payment_attempts"
}
