package webhooks

import (
	"context"
	"fmt"

	"github.com/chrisfonseca08/distri/payment-service/database/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormStore records events in payments.webhook_events. The unique index on
// event_id makes a redelivery insert nothing.
type GormStore struct {
	db *gorm.DB
}

// NewGormStore creates the webhook event store backed by db.
func NewGormStore(db *gorm.DB) *GormStore {
	return &GormStore{db: db}
}

// Insert stores a webhook event and reports whether it was new.
func (s *GormStore) Insert(ctx context.Context, event NewEvent) (bool, error) {
	row := models.WebhookEvent{
		EventID:   event.EventID,
		EventType: event.EventType,
		Body:      event.Body,
		Status:    event.Status,
	}
	if event.GatewayOrderID != "" {
		id := event.GatewayOrderID
		row.GatewayOrderID = &id
	}
	result := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "event_id"}}, DoNothing: true}).
		Create(&row)
	if result.Error != nil {
		return false, fmt.Errorf("record webhook event: %w", result.Error)
	}
	return result.RowsAffected == 1, nil
}
