package models

import "time"

type ProcessedMessage struct {
	MessageID   string    `gorm:"type:text;primaryKey"`
	ProcessedAt time.Time `gorm:"type:timestamptz;not null;autoCreateTime"`
}

// TableName puts the processed-messages table in the orchestrator schema.
func (ProcessedMessage) TableName() string {
	return "orchestrator.processed_messages"
}
