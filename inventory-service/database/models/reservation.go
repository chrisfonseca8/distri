package models

import (
	"time"
	"uuid"
)

type Reservation struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	OrderID     uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:uidx_reservations_order_id"`
	RequestHash string    `gorm:"type:text;not null"`
	Status      string    `gorm:"type:text;not null;default:PENDING;check:chk_reservations_status,status IN ('PENDING','RESERVED','CONFIRMED','RELEASED')"`
	CreatedAt   time.Time `gorm:"type:timestamptz;not null;autoCreateTime"`
	UpdatedAt   time.Time `gorm:"type:timestamptz;not null;autoUpdateTime"`

	Items []ReservationItem `gorm:"foreignKey:ReservationID;constraint:OnDelete:RESTRICT"`
}
