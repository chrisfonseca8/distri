package models

import (
	"time"
	"uuid"
)

type ReservationItem struct {
	ReservationID  uuid.UUID `gorm:"type:uuid;primaryKey"`
	ProductID      uuid.UUID `gorm:"type:uuid;primaryKey"`
	Quantity       int64     `gorm:"not null;check:chk_reservation_items_quantity_positive,quantity > 0"`
	UnitPricePaise int64     `gorm:"not null;check:chk_reservation_items_price_positive,unit_price_paise > 0"`
	Currency       string    `gorm:"type:varchar(3);not null"`
	CreatedAt      time.Time `gorm:"type:timestamptz;not null;autoCreateTime"`

	Product Product `gorm:"foreignKey:ProductID;references:ID;constraint:OnDelete:RESTRICT"`
}
