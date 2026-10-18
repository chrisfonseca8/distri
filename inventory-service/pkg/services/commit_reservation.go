package services

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/inventory-service/database/models"
	"gorm.io/gorm"
)

var (
	ErrReservationNotFound      = errors.New("reservation not found")
	ErrReservationStateConflict = errors.New("reservation status conflict")
)

type ChangeReservationResult struct {
	ReservationID uuid.UUID `json:"reservation_id"`
	OrderID       uuid.UUID `json:"order_id"`
	Status        string    `json:"status"`
}

type stockMovement string

const (
	confirmStock stockMovement = "confirm"
	releaseStock stockMovement = "release"
)

func (s *ReservationService) ConfirmReservation(
	ctx context.Context,
	orderID uuid.UUID,
) (ChangeReservationResult, error) {
	return s.changeReservationStatus(ctx, orderID, "CONFIRMED", confirmStock)
}

func (s *ReservationService) ReleaseReservation(
	ctx context.Context,
	orderID uuid.UUID,
) (ChangeReservationResult, error) {
	return s.changeReservationStatus(ctx, orderID, "RELEASED", releaseStock)
}

func (s *ReservationService) changeReservationStatus(
	ctx context.Context,
	orderID uuid.UUID,
	targetStatus string,
	movement stockMovement,
) (ChangeReservationResult, error) {
	if orderID == uuid.Nil() {
		return ChangeReservationResult{}, ErrInvalidReservationInput
	}

	var result ChangeReservationResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		reservation, changed, err := transitionReservedReservation(tx, orderID, targetStatus)
		if err != nil {
			return err
		}

		result = ChangeReservationResult{
			ReservationID: reservation.ID,
			OrderID:       reservation.OrderID,
			Status:        reservation.Status,
		}
		if !changed {
			return nil
		}

		items, err := loadReservationItems(tx, reservation.ID)
		if err != nil {
			return err
		}
		return moveReservedStock(tx, items, movement)
	})

	return result, err
}

func transitionReservedReservation(
	tx *gorm.DB,
	orderID uuid.UUID,
	targetStatus string,
) (models.Reservation, bool, error) {
	update := tx.Table("inventory.reservations").
		Where("order_id = ? AND status = ?", orderID, "RESERVED").
		Updates(map[string]any{
			"status":     targetStatus,
			"updated_at": time.Now(),
		})
	if update.Error != nil {
		return models.Reservation{}, false, fmt.Errorf("change reservation status: %w", update.Error)
	}

	var reservation models.Reservation
	query := tx.Table("inventory.reservations").Where("order_id = ?", orderID).First(&reservation)
	if errors.Is(query.Error, gorm.ErrRecordNotFound) {
		return models.Reservation{}, false, ErrReservationNotFound
	}
	if query.Error != nil {
		return models.Reservation{}, false, fmt.Errorf("get reservation status: %w", query.Error)
	}

	if update.RowsAffected == 1 {
		return reservation, true, nil
	}
	if reservation.Status == targetStatus {
		return reservation, false, nil
	}
	return models.Reservation{}, false, fmt.Errorf(
		"%w: cannot change %s to %s",
		ErrReservationStateConflict,
		reservation.Status,
		targetStatus,
	)
}

func loadReservationItems(tx *gorm.DB, reservationID uuid.UUID) ([]models.ReservationItem, error) {
	var items []models.ReservationItem
	query := tx.Table("inventory.reservation_items").
		Where("reservation_id = ?", reservationID).
		Order("product_id").
		Find(&items)
	if query.Error != nil {
		return nil, fmt.Errorf("load reservation items: %w", query.Error)
	}
	if len(items) == 0 {
		return nil, errors.New("reservation has no items")
	}
	return items, nil
}

func moveReservedStock(
	tx *gorm.DB,
	items []models.ReservationItem,
	movement stockMovement,
) error {
	now := time.Now()
	for _, item := range items {
		updates := map[string]any{
			"reserved":   gorm.Expr("reserved - ?", item.Quantity),
			"updated_at": now,
		}
		if movement == confirmStock {
			updates["on_hand"] = gorm.Expr("on_hand - ?", item.Quantity)
		}

		stockRow := tx.Table("inventory.inventories").
			Where("product_id = ? AND reserved >= ?", item.ProductID, item.Quantity)
		if movement == confirmStock {
			stockRow = stockRow.Where("on_hand >= ?", item.Quantity)
		}

		update := stockRow.Updates(updates)
		if update.Error != nil {
			return fmt.Errorf("%s stock for product %s: %w", movement, item.ProductID, update.Error)
		}
		if update.RowsAffected != 1 {
			return fmt.Errorf(
				"%s stock for product %s: reserved stock invariant failed",
				movement,
				item.ProductID,
			)
		}
	}
	return nil
}
