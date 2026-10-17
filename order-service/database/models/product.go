package models

import (
	"time"
	"uuid"
)

type Product struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Name       string    `gorm:"type:text;not null"`
	PricePaise int64     `gorm:"not null;check:chk_products_price_positive,price_paise > 0"`
	Currency   string    `gorm:"type:varchar(3);not null;default:INR"`
	CreatedAt  time.Time `gorm:"not null;autoCreateTime"`
	UpdatedAt  time.Time `gorm:"not null;autoUpdateTime"`

	Inventory Inventory `gorm:"foreignKey:ProductID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT"`
}
