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
	Status           string    `gorm:"type:text;not null;check:chk_payment_attempts_status,status IN ('CREATED','PENDING','AWAITING_PAYMENT','SUCCEEDED','FAILED','UNKNOWN')" json:"status"`
	Gateway          string    `gorm:"type:text;not null" json:"gateway"`
	GatewayOrderID   *string   `gorm:"type:text;uniqueIndex:uidx_payment_attempts_gateway_order,where:gateway_order_id IS NOT NULL" json:"gateway_order_id"`
	GatewayPaymentID *string   `gorm:"type:text" json:"gateway_payment_id"`
	FailureCode      *string   `gorm:"type:text" json:"failure_code"`
	// ExpiresAt ends the payment window of an AWAITING_PAYMENT attempt.
	ExpiresAt *time.Time `gorm:"type:timestamptz" json:"expires_at"`
	// RefundStatus tracks a refund owed for a payment captured after the attempt
	// was already failed: REQUIRED, REQUESTED or REFUND_FAILED.
	RefundStatus *string `gorm:"type:text" json:"refund_status"`
	RefundID     *string `gorm:"type:text" json:"refund_id"`
	// CaptureObservedAt is when WE first observed the capture that decided this
	// attempt (webhook receipt or poll response), on our clock. It is the evidence
	// the deadline decision uses; provider timestamps never decide it.
	CaptureObservedAt *time.Time `gorm:"type:timestamptz" json:"capture_observed_at"`
	// CaptureSource is "webhook" or "poll".
	CaptureSource *string `gorm:"type:text" json:"capture_source"`
	// ReconciledAt is when the reconciler last asked the gateway about this attempt.
	ReconciledAt *time.Time `gorm:"type:timestamptz" json:"-"`
	CreatedAt    time.Time  `gorm:"type:timestamptz;not null;autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time  `gorm:"type:timestamptz;not null;autoUpdateTime" json:"updated_at"`
}

func (PaymentAttempt) TableName() string {
	return "payments.payment_attempts"
}
