package models

import (
	"time"
	"uuid"
)

// OrderTombstone records that an order was cancelled before it was ever created.
//
// order.cancel can reach the order service for an order that does not exist yet (the
// saga gave up on order.create and compensates). Recording the cancel, instead of
// failing it, lets the saga finish, and makes a late order.create for the same id a
// no-op: the two commands can arrive in either order and the result is the same.
type OrderTombstone struct {
	OrderID   uuid.UUID `gorm:"type:uuid;primaryKey"`
	CreatedAt time.Time `gorm:"type:timestamptz;not null;autoCreateTime"`
}

// TableName puts the tombstone table in the orders schema.
func (OrderTombstone) TableName() string {
	return "orders.order_tombstones"
}
