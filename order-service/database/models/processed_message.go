package models

import "time"

type ProcessedMessage struct {
	MessageID   string    `gorm:"type:text;primaryKey"`
	ProcessedAt time.Time `gorm:"type:timestamptz;not null;autoCreateTime"`
}

// TableName puts the processed-messages table in the orders schema.
func (ProcessedMessage) TableName() string {
	return "orders.processed_messages"
}
