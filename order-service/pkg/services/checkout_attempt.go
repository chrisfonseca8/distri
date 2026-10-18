package services

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/chrisfonseca08/distri/order-service/database/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrIdempotencyKeyConflict = errors.New(
	"idempotency key was already used for a different checkout request",
)

func (s *CheckoutService) claimCheckoutAttempt(
	ctx context.Context,
	prepared preparedCheckoutRequest,
) (models.CheckoutAttempt, bool, error) {
	orderID := uuid.New()
	attempt := prepared.Attempt
	attempt.OrderID = &orderID

	created, err := s.createCheckoutAttempt(ctx, &attempt, orderID)
	if err != nil {
		return models.CheckoutAttempt{}, false, err
	}

	return attempt, created, nil
}

func (s *CheckoutService) createCheckoutAttempt(
	ctx context.Context,
	attempt *models.CheckoutAttempt,
	newOrderID uuid.UUID,
) (bool, error) {
	insert := s.db.WithContext(ctx).
		Table("orders.checkout_attempts").
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "idempotency_key"}},
			DoUpdates: clause.Assignments(map[string]any{
				"idempotency_key": gorm.Expr("EXCLUDED.idempotency_key"),
			}),
			Where: clause.Where{Exprs: []clause.Expression{
				clause.Expr{SQL: "checkout_attempts.request_hash = EXCLUDED.request_hash"},
			}},
		}).
		Clauses(clause.Returning{}).
		Create(attempt)
	if insert.Error != nil {
		return false, fmt.Errorf(
			"create checkout attempt: %w",
			insert.Error,
		)
	}
	if insert.RowsAffected == 0 {
		return false, ErrIdempotencyKeyConflict
	}

	created := attempt.OrderID != nil && *attempt.OrderID == newOrderID
	return created, nil
}
