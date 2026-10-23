package models

import "time"

type ProcessedMessage struct {
	MessageID   string    `gorm:"type:text;primaryKey"`
	ProcessedAt time.Time `gorm:"type:timestamptz;not null;autoCreateTime"`
}

func (ProcessedMessage) TableName() string {
	return "inventory.processed_messages"
}
