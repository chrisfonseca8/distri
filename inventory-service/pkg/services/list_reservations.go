package services

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/inventory-service/database/models"
)

const maxReservationListLimit = 1000

type ReservationSummary struct {
	ReservationID uuid.UUID `json:"reservation_id"`
	OrderID       uuid.UUID `json:"order_id"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

type ReservationCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// ListReservedReservations returns one page of RESERVED reservations older than olderThan, after the cursor.
func (s *ReservationService) ListReservedReservations(
	ctx context.Context,
	olderThan time.Time,
	after *ReservationCursor,
	limit int,
) ([]ReservationSummary, error) {
	if olderThan.IsZero() || limit <= 0 || limit > maxReservationListLimit {
		return nil, ErrInvalidReservationInput
	}
	if after != nil && (after.CreatedAt.IsZero() || after.ID == uuid.Nil() || !after.CreatedAt.Before(olderThan)) {
		return nil, ErrInvalidReservationInput
	}

	var reservations []models.Reservation
	query := s.db.WithContext(ctx).
		Table("inventory.reservations").
		Select("id, order_id, status, created_at").
		Where("status = ? AND created_at < ?", "RESERVED", olderThan)
	if after != nil {
		query = query.Where(
			"(created_at > ?) OR (created_at = ? AND id > ?)",
			after.CreatedAt,
			after.CreatedAt,
			after.ID,
		)
	}
	query = query.
		Order("created_at ASC").
		Order("id ASC").
		Limit(limit).
		Find(&reservations)
	if query.Error != nil {
		return nil, fmt.Errorf("list reserved reservations: %w", query.Error)
	}

	results := make([]ReservationSummary, 0, len(reservations))
	for _, reservation := range reservations {
		if reservation.OrderID == uuid.Nil() {
			return nil, errors.New("reserved reservation has no order ID")
		}
		results = append(results, ReservationSummary{
			ReservationID: reservation.ID,
			OrderID:       reservation.OrderID,
			Status:        reservation.Status,
			CreatedAt:     reservation.CreatedAt,
		})
	}
	return results, nil
}
