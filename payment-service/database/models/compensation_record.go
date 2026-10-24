package models

import (
	"time"
	"uuid"
)

// Compensation lifecycle (the workflow state, kept apart from the provider's own
// refund status in ProviderStatus):
//
//	REQUIRED   a refund is owed and has not been submitted
//	SUBMITTING an operation is claimed (lease) and being submitted; its identity
//	           (OperationID) was stored before the request was sent
//	UNKNOWN    the submission outcome is not known (timeout, reset, 5xx, or a worker
//	           that stopped after sending): never read as "no refund exists"
//	PENDING    the provider accepted a refund (RefundID known) and reports it pending
//	PROCESSED  the provider reports the refund processed: the only "complete" state
//	FAILED     a definitive failure; waits for the recovery policy (retry or REVIEW)
//	REVIEW     unresolved: needs a person; ReviewReason says what and why
const (
	CompensationRequired   = "REQUIRED"
	CompensationSubmitting = "SUBMITTING"
	CompensationUnknown    = "UNKNOWN"
	CompensationPending    = "PENDING"
	CompensationProcessed  = "PROCESSED"
	CompensationFailed     = "FAILED"
	CompensationReview     = "REVIEW"
)

// CompensationRecord tracks captured money that did not pay for a completed
// checkout, one row per (gateway, gateway payment id). It is the source of truth
// for refunds: the attempt's refund_status column is legacy and unused.
//
// Gateway is the environment label of the attempt that owns the money ("razorpay",
// "razorpay-sim"); a worker only handles records of its own label, so simulator
// work can never reach the real API.
type CompensationRecord struct {
	ID               uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	PaymentAttemptID uuid.UUID `gorm:"type:uuid;not null;index:idx_compensation_attempt"`
	OrderID          uuid.UUID `gorm:"type:uuid;not null"`
	Gateway          string    `gorm:"type:text;not null;default:'';uniqueIndex:uidx_compensation_payment,priority:1"`
	GatewayPaymentID string    `gorm:"type:text;not null;uniqueIndex:uidx_compensation_payment,priority:2"`
	// AmountPaise is the captured amount, which is also the amount to refund (full
	// refunds only).
	AmountPaise  int64   `gorm:"not null"`
	Currency     string  `gorm:"type:text;not null"`
	Reason       string  `gorm:"type:text;not null"`
	Status       string  `gorm:"type:text;not null;check:chk_compensation_status,status IN ('REQUIRED','SUBMITTING','UNKNOWN','PENDING','PROCESSED','FAILED','REVIEW')"`
	ReviewReason *string `gorm:"type:text"`

	// OperationID identifies the current refund operation. It is minted when the
	// operation is claimed, stored before anything is sent, and is the refund's
	// receipt and idempotency key, so a recovered worker reuses it.
	OperationID *uuid.UUID `gorm:"type:uuid"`
	RefundID    *string    `gorm:"type:text"`
	// ProviderStatus is the provider's refund status: pending, processed or failed.
	ProviderStatus *string `gorm:"type:text"`
	// AmountRefundedBefore is payment.amount_refunded observed just before the
	// submission, to notice refunds that appear without our request.
	AmountRefundedBefore *int64 `gorm:"type:bigint"`

	Attempts      int        `gorm:"not null;default:0"` // operations started
	FailureCount  int        `gorm:"not null;default:0"` // definitive failures
	RetryCount    int        `gorm:"not null;default:0"` // same-key resubmissions after UNKNOWN
	ReviewRetries int        `gorm:"not null;default:0"` // automatic retries after a rejected refund went to REVIEW
	NextAttemptAt time.Time  `gorm:"type:timestamptz;not null;default:now();index:idx_compensation_work,priority:2"`
	LockedBy      *string    `gorm:"type:text"`
	LockedUntil   *time.Time `gorm:"type:timestamptz"`
	SubmittedAt   *time.Time `gorm:"type:timestamptz"` // set just before the request is sent
	UnknownSince  *time.Time `gorm:"type:timestamptz"`
	LastError     *string    `gorm:"type:text"`

	ObservedAt time.Time `gorm:"type:timestamptz;not null"`
	Source     string    `gorm:"type:text;not null"`
	CreatedAt  time.Time `gorm:"type:timestamptz;not null;autoCreateTime"`
	UpdatedAt  time.Time `gorm:"type:timestamptz;not null;autoUpdateTime"`
}

// TableName puts the compensation-records table in the payments schema.
func (CompensationRecord) TableName() string {
	return "payments.compensation_records"
}

// CompensationEvent is the append-only history of a compensation record. Nothing
// is ever updated or deleted here, so an earlier failure stays visible after the
// record moves on.
type CompensationEvent struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	RecordID   uuid.UUID `gorm:"type:uuid;not null;index:idx_compensation_events_record"`
	FromStatus string    `gorm:"type:text;not null"`
	ToStatus   string    `gorm:"type:text;not null"`
	Detail     string    `gorm:"type:text;not null"`
	At         time.Time `gorm:"type:timestamptz;not null;default:now()"`
}

// TableName puts the compensation-events table in the payments schema.
func (CompensationEvent) TableName() string {
	return "payments.compensation_events"
}
