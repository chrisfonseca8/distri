package models

import (
	"time"
	"uuid"
)

type OrderItem struct {
	ID             uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	OrderID        uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:uidx_order_items_order_product"`
	ProductID      uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:uidx_order_items_order_product"`
	Quantity       int64     `gorm:"not null;check:chk_order_items_quantity_positive,quantity > 0"`
	UnitPricePaise int64     `gorm:"not null;check:chk_order_items_unit_price_positive,unit_price_paise > 0"`
	CreatedAt      time.Time `gorm:"type:timestamptz;not null;autoCreateTime"`

	Order   Order   `gorm:"foreignKey:OrderID;references:ID;constraint:OnDelete:RESTRICT"`
	Product Product `gorm:"foreignKey:ProductID;references:ID;constraint:OnDelete:RESTRICT"`
}
