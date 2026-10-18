package models

import (
	"time"
	"uuid"
)

type Inventory struct {
	ProductID uuid.UUID `gorm:"type:uuid;primaryKey"`
	OnHand    int64     `gorm:"not null;check:chk_inventory_on_hand_nonnegative,on_hand >= 0"`
	Reserved  int64     `gorm:"not null;default:0;check:chk_inventory_reserved_nonnegative,reserved >= 0"`
	Allocated int64     `gorm:"not null;default:0;check:chk_inventory_allocation_bounds,allocated >= 0 AND reserved + allocated <= on_hand"`
	CreatedAt time.Time `gorm:"type:timestamptz;not null;autoCreateTime"`
	UpdatedAt time.Time `gorm:"type:timestamptz;not null;autoUpdateTime"`
}
