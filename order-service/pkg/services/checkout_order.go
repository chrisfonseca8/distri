package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/order-service/database/models"
	"github.com/chrisfonseca08/distri/order-service/pkg/clients"
	"github.com/chrisfonseca08/distri/order-service/pkg/utils"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *CheckoutService) createOrderFromReservation(
	ctx context.Context,
	attempt models.CheckoutAttempt,
	reservation clients.ReserveResult,
) (CreateCheckoutResult, error) {
	if attempt.OrderID == nil {
		return CreateCheckoutResult{}, errors.New("checkout attempt has no order ID")
	}

	result := CreateCheckoutResult{
		OrderID:    *attempt.OrderID,
		Status:     "PENDING_PAYMENT",
		TotalPaise: reservation.TotalPaise,
		Currency:   reservation.Currency,
		Created:    true,
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		orderCreated, err := createOrder(tx, result)
		if err != nil {
			return err
		}
		if !orderCreated {
			replayed, err := loadCompletedCheckoutResult(tx, attempt.IdempotencyKey)
			if err != nil {
				return err
			}
			result = replayed
			return nil
		}

		if err := createOrderItems(tx, result.OrderID, reservation.Items); err != nil {
			return err
		}
		if err := enqueueSettleJob(tx, result.OrderID); err != nil {
			return err
		}
		return completeCheckoutAttempt(tx, attempt.IdempotencyKey, result)
	})
	if err != nil {
		return CreateCheckoutResult{}, err
	}

	return result, nil
}

func createOrder(tx *gorm.DB, result CreateCheckoutResult) (bool, error) {
	order := models.Order{
		ID:         result.OrderID,
		Status:     result.Status,
		TotalPaise: result.TotalPaise,
		Currency:   result.Currency,
	}
	insert := tx.Table("orders.orders").
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoNothing: true,
		}).
		Create(&order)
	if insert.Error != nil {
		return false, fmt.Errorf("create order: %w", insert.Error)
	}
	return insert.RowsAffected == 1, nil
}

func enqueueSettleJob(tx *gorm.DB, orderID uuid.UUID) error {
	job := models.Job{
		Type:    utils.JobTypeSettleOrder,
		OrderID: orderID,
	}
	if err := tx.Table("orders.jobs").
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&job).Error; err != nil {
		return fmt.Errorf("enqueue settle job: %w", err)
	}
	return nil
}

func createOrderItems(
	tx *gorm.DB,
	orderID uuid.UUID,
	reservedItems []clients.ReservedLine,
) error {
	if len(reservedItems) == 0 {
		return errors.New("inventory reservation has no items")
	}

	items := make([]models.OrderItem, 0, len(reservedItems))
	for _, item := range reservedItems {
		items = append(items, models.OrderItem{
			OrderID:        orderID,
			ProductID:      item.ProductID,
			Quantity:       item.Quantity,
			UnitPricePaise: item.UnitPricePaise,
		})
	}

	if err := tx.Table("orders.order_items").
		Omit(clause.Associations).
		Create(&items).Error; err != nil {
		return fmt.Errorf("create order items: %w", err)
	}
	return nil
}

func completeCheckoutAttempt(
	tx *gorm.DB,
	idempotencyKey string,
	result CreateCheckoutResult,
) error {
	body, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode checkout response: %w", err)
	}

	responseCode := http.StatusCreated
	update := tx.Table("orders.checkout_attempts").
		Where("idempotency_key = ? AND status = ?", idempotencyKey, "in_progress").
		Updates(map[string]any{
			"status":        "completed",
			"response_code": responseCode,
			"response_body": datatypes.JSON(body),
			"updated_at":    time.Now(),
		})
	if update.Error != nil {
		return fmt.Errorf("complete checkout attempt: %w", update.Error)
	}
	if update.RowsAffected != 1 {
		return errors.New("checkout attempt was not completed")
	}
	return nil
}

func loadCompletedCheckoutResult(
	tx *gorm.DB,
	idempotencyKey string,
) (CreateCheckoutResult, error) {
	var attempt models.CheckoutAttempt
	query := tx.Table("orders.checkout_attempts").
		Where("idempotency_key = ? AND status = ?", idempotencyKey, "completed").
		First(&attempt)
	if query.Error != nil {
		return CreateCheckoutResult{}, fmt.Errorf("load completed checkout attempt: %w", query.Error)
	}
	return checkoutResultFromAttempt(attempt)
}

func checkoutResultFromAttempt(attempt models.CheckoutAttempt) (CreateCheckoutResult, error) {
	if attempt.Status != "completed" || len(attempt.ResponseBody) == 0 {
		return CreateCheckoutResult{}, errors.New("checkout attempt has no completed response")
	}

	var result CreateCheckoutResult
	if err := json.Unmarshal(attempt.ResponseBody, &result); err != nil {
		return CreateCheckoutResult{}, fmt.Errorf("decode completed checkout response: %w", err)
	}
	result.Created = false
	return result, nil
}
