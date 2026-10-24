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
	errStockInvariant           = errors.New("reserved stock invariant failed")
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
	// restockStock undoes a confirm: the sale is cancelled after the stock had been
	// consumed, so the units come back to on_hand. It happens only when the saga unwinds a
	// checkout that was paid and then refunded.
	restockStock stockMovement = "restock"
)

// ConfirmReservation marks an order's reservation CONFIRMED and moves its reserved stock to sold.
func (s *ReservationService) ConfirmReservation(
	ctx context.Context,
	orderID uuid.UUID,
) (ChangeReservationResult, error) {
	return s.changeReservationStatus(ctx, orderID, "CONFIRMED", confirmStock)
}

// ReleaseReservation marks an order's reservation RELEASED and returns its reserved stock to available.
func (s *ReservationService) ReleaseReservation(
	ctx context.Context,
	orderID uuid.UUID,
) (ChangeReservationResult, error) {
	return s.changeReservationStatus(ctx, orderID, "RELEASED", releaseStock)
}

// changeReservationStatus runs a reservation status change and its stock movement in one transaction.
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
		var err error
		result, err = changeReservationStatusTx(tx, orderID, targetStatus, movement)
		return err
	})

	return result, err
}

// changeReservationStatusTx runs the guarded status change and the matching
// stock movement inside the caller's transaction, so the AMQP handler can add
// its processed-message and reply-outbox rows to the same commit.
func changeReservationStatusTx(
	tx *gorm.DB,
	orderID uuid.UUID,
	targetStatus string,
	movement stockMovement,
) (ChangeReservationResult, error) {
	reservation, changed, restock, err := transitionReservedReservation(tx, orderID, targetStatus)
	if err != nil {
		return ChangeReservationResult{}, err
	}
	if restock {
		movement = restockStock
	}

	result := ChangeReservationResult{
		ReservationID: reservation.ID,
		OrderID:       reservation.OrderID,
		Status:        reservation.Status,
	}
	if !changed {
		return result, nil
	}

	items, err := loadReservationItems(tx, reservation.ID)
	if err != nil {
		return ChangeReservationResult{}, err
	}
	if err := moveReservedStock(tx, items, movement); err != nil {
		return ChangeReservationResult{}, err
	}
	return result, nil
}

// transitionReservedReservation moves a reservation from RESERVED to targetStatus and reports
// whether it changed. A release may also undo a CONFIRMED reservation (the third result is
// then true and the consumed stock must be restocked): that is how a paid and refunded
// checkout gives its units back.
func transitionReservedReservation(
	tx *gorm.DB,
	orderID uuid.UUID,
	targetStatus string,
) (models.Reservation, bool, bool, error) {
	update := tx.Table("inventory.reservations").
		Where("order_id = ? AND status = ?", orderID, "RESERVED").
		Updates(map[string]any{
			"status":     targetStatus,
			"updated_at": time.Now(),
		})
	if update.Error != nil {
		return models.Reservation{}, false, false, fmt.Errorf("change reservation status: %w", update.Error)
	}
	restock := false
	if update.RowsAffected == 0 && targetStatus == "RELEASED" {
		undo := tx.Table("inventory.reservations").
			Where("order_id = ? AND status = ?", orderID, "CONFIRMED").
			Updates(map[string]any{"status": targetStatus, "updated_at": time.Now()})
		if undo.Error != nil {
			return models.Reservation{}, false, false, fmt.Errorf("undo confirmed reservation: %w", undo.Error)
		}
		update, restock = undo, undo.RowsAffected == 1
	}

	var reservation models.Reservation
	query := tx.Table("inventory.reservations").Where("order_id = ?", orderID).First(&reservation)
	if errors.Is(query.Error, gorm.ErrRecordNotFound) {
		return models.Reservation{}, false, false, ErrReservationNotFound
	}
	if query.Error != nil {
		return models.Reservation{}, false, false, fmt.Errorf("get reservation status: %w", query.Error)
	}

	if update.RowsAffected == 1 {
		return reservation, true, restock, nil
	}
	if reservation.Status == targetStatus {
		return reservation, false, false, nil
	}
	return models.Reservation{}, false, false, fmt.Errorf(
		"%w: cannot change %s to %s",
		ErrReservationStateConflict,
		reservation.Status,
		targetStatus,
	)
}

// loadReservationItems reads the items of a reservation in product order.
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

// moveReservedStock applies the confirm or release stock movement to each reserved item.
func moveReservedStock(
	tx *gorm.DB,
	items []models.ReservationItem,
	movement stockMovement,
) error {
	now := time.Now()
	for _, item := range items {
		if movement == restockStock {
			update := tx.Table("inventory.inventories").
				Where("product_id = ?", item.ProductID).
				Updates(map[string]any{"on_hand": gorm.Expr("on_hand + ?", item.Quantity), "updated_at": now})
			if update.Error != nil {
				return fmt.Errorf("%s stock for product %s: %w", movement, item.ProductID, update.Error)
			}
			if update.RowsAffected != 1 {
				return fmt.Errorf("%s stock for product %s: %w", movement, item.ProductID, errStockInvariant)
			}
			continue
		}
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
				"%s stock for product %s: %w",
				movement,
				item.ProductID,
				errStockInvariant,
			)
		}
	}
	return nil
}
