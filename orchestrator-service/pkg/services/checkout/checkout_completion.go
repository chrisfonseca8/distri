package checkout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/orchestrator-service/database/models"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/clients"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/messages"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/outbox"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/saga"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errConcurrentSagaWinner = errors.New("concurrent checkout created the saga")

func (s *CheckoutService) createSagaFromReservation(
	ctx context.Context,
	attempt models.CheckoutAttempt,
	reservation clients.ReserveResult,
) (Response, error) {
	if attempt.OrderID == nil {
		return Response{}, errors.New("checkout attempt has no order ID")
	}

	result := CreateCheckoutResult{
		OrderID:    *attempt.OrderID,
		Status:     string(saga.SagaRunning),
		TotalPaise: reservation.TotalPaise,
		Currency:   reservation.Currency,
	}
	responseBody, err := json.Marshal(result)
	if err != nil {
		return Response{}, fmt.Errorf("encode checkout response: %w", err)
	}
	response := Response{StatusCode: http.StatusAccepted, Body: responseBody}

	commandBody, err := orderCreatePayload(*attempt.OrderID, reservation)
	if err != nil {
		return Response{}, err
	}

	now := time.Now().UTC()
	nextAttemptAt := now.Add(s.stepTimeout)
	orderCreateRoutingKey := messages.OrderCreateRoutingKey
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		sagaRow := models.Saga{
			ID:          *attempt.OrderID,
			Status:      string(saga.SagaRunning),
			CurrentStep: string(saga.CREATE_ORDER),
			TotalPaise:  reservation.TotalPaise,
			Currency:    reservation.Currency,
			DeadlineAt:  now.Add(s.sagaDeadline),
		}
		if err := tx.Table("orchestrator.sagas").Create(&sagaRow).Error; err != nil {
			if isUniqueViolation(err) {
				return errConcurrentSagaWinner
			}
			return fmt.Errorf("create saga: %w", err)
		}

		reserveStep := models.SagaStep{
			SagaID:     *attempt.OrderID,
			Step:       string(saga.RESERVE_STOCK),
			Status:     "SUCCEEDED",
			FinishedAt: &now,
		}
		if err := tx.Table("orchestrator.saga_steps").
			Omit(clause.Associations).
			Create(&reserveStep).Error; err != nil {
			return fmt.Errorf("create reserve-stock saga step: %w", err)
		}

		createOrderStep := models.SagaStep{
			SagaID:         *attempt.OrderID,
			Step:           string(saga.CREATE_ORDER),
			Status:         "RUNNING",
			Attempts:       1,
			NextAttemptAt:  &nextAttemptAt,
			StartedAt:      &now,
			RoutingKey:     &orderCreateRoutingKey,
			CommandPayload: datatypes.JSON(commandBody),
		}
		if err := tx.Table("orchestrator.saga_steps").
			Omit(clause.Associations).
			Create(&createOrderStep).Error; err != nil {
			return fmt.Errorf("create create-order saga step: %w", err)
		}

		if err := outbox.Enqueue(tx, outbox.Message{
			MessageID:  uuid.New(),
			RoutingKey: messages.OrderCreateRoutingKey,
			Payload:    commandBody,
		}); err != nil {
			return err
		}

		return completeCheckoutAttempt(tx, attempt.IdempotencyKey, response, now)
	})
	if err != nil {
		return Response{}, err
	}
	return s.loadCompletedResponse(ctx, attempt.IdempotencyKey)
}

func orderCreatePayload(orderID uuid.UUID, reservation clients.ReserveResult) (json.RawMessage, error) {
	items := make([]messages.OrderCreateItem, 0, len(reservation.Items))
	for _, item := range reservation.Items {
		items = append(items, messages.OrderCreateItem{
			ProductID:      item.ProductID,
			Quantity:       item.Quantity,
			UnitPricePaise: item.UnitPricePaise,
		})
	}

	body, err := json.Marshal(messages.OrderCreate{
		OrderID:    orderID,
		Currency:   reservation.Currency,
		TotalPaise: reservation.TotalPaise,
		Items:      items,
	})
	if err != nil {
		return nil, fmt.Errorf("encode order.create payload: %w", err)
	}
	return body, nil
}

func (s *CheckoutService) completeDefinitiveFailure(
	ctx context.Context,
	idempotencyKey string,
	response Response,
) (Response, error) {
	update := s.db.WithContext(ctx).
		Table("orchestrator.checkout_attempts").
		Where("idempotency_key = ? AND status = ?", idempotencyKey, "in_progress").
		Updates(map[string]any{
			"status":        "completed",
			"response_code": response.StatusCode,
			"response_body": datatypes.JSON(response.Body),
			"updated_at":    time.Now().UTC(),
		})
	if update.Error != nil {
		return Response{}, fmt.Errorf("complete failed checkout attempt: %w", update.Error)
	}
	return s.loadCompletedResponse(ctx, idempotencyKey)
}

func completeCheckoutAttempt(
	tx *gorm.DB,
	idempotencyKey string,
	response Response,
	now time.Time,
) error {
	update := tx.Table("orchestrator.checkout_attempts").
		Where("idempotency_key = ? AND status = ?", idempotencyKey, "in_progress").
		Updates(map[string]any{
			"status":        "completed",
			"response_code": response.StatusCode,
			"response_body": datatypes.JSON(response.Body),
			"updated_at":    now,
		})
	if update.Error != nil {
		return fmt.Errorf("complete checkout attempt: %w", update.Error)
	}
	if update.RowsAffected != 1 {
		return errors.New("checkout attempt was not completed")
	}
	return nil
}

func (s *CheckoutService) loadCompletedResponse(
	ctx context.Context,
	idempotencyKey string,
) (Response, error) {
	var attempt models.CheckoutAttempt
	query := s.db.WithContext(ctx).
		Table("orchestrator.checkout_attempts").
		Where("idempotency_key = ? AND status = ?", idempotencyKey, "completed").
		First(&attempt)
	if query.Error != nil {
		return Response{}, fmt.Errorf("load completed checkout attempt: %w", query.Error)
	}
	return responseFromAttempt(attempt)
}

type sqlStateError interface {
	SQLState() string
}

func isUniqueViolation(err error) bool {
	var databaseError sqlStateError
	return errors.As(err, &databaseError) && databaseError.SQLState() == "23505"
}
