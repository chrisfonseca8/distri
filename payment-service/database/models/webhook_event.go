package models

import (
	"time"
	"uuid"
)

// WebhookEvent is the durable inbox of gateway webhooks. A row is written (and
// the request acknowledged) only after the signature verified; a worker applies
// it later. EventID is Razorpay's x-razorpay-event-id, unique per event, so a
// redelivery is a no-op at the database.
//
// Status: PENDING (to be applied), PROCESSED (effects committed), IGNORED (an
// authenticated event type we deliberately do not act on), PARKED (could not be
// applied after bounded retries or is malformed: kept for review, never dropped).
type WebhookEvent struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	EventID       string    `gorm:"type:text;not null;uniqueIndex:uidx_webhook_events_event_id"`
	EventType     string    `gorm:"type:text;not null"`
	Body          []byte    `gorm:"type:bytea;not null"`
	Status        string    `gorm:"type:text;not null;default:PENDING;check:chk_webhook_events_status,status IN ('PENDING','PROCESSED','IGNORED','PARKED')"`
	Attempts      int       `gorm:"not null;default:0;check:chk_webhook_events_attempts_nonnegative,attempts >= 0"`
	NextAttemptAt time.Time `gorm:"type:timestamptz;not null;default:now();index:idx_webhook_events_claim,where:status = 'PENDING'"`
	// GatewayOrderID is the Razorpay order the event is about (from the verified
	// payload), so parked events can be found again once the mapping exists.
	GatewayOrderID *string `gorm:"type:text;index:idx_webhook_events_gateway_order,where:gateway_order_id IS NOT NULL"`
	// ParkReason says why a PARKED event is parked: NO_MAPPING events are replayed
	// automatically when the mapping appears; the rest need a human.
	ParkReason  *string    `gorm:"type:text;index:idx_webhook_events_parked,where:status = 'PARKED'"`
	LockedBy    *string    `gorm:"type:text"`
	LockedUntil *time.Time `gorm:"type:timestamptz"`
	LastError   *string    `gorm:"type:text"`
	ReceivedAt  time.Time  `gorm:"type:timestamptz;not null;autoCreateTime"`
	ProcessedAt *time.Time `gorm:"type:timestamptz"`
}

func (WebhookEvent) TableName() string {
	return "payments.webhook_events"
}
