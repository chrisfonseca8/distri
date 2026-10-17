package seeding

import (
	"fmt"
	"uuid"

	"github.com/chrisfonseca08/distri/payment-service/database/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func SeedPaymentAttempts(db *gorm.DB) error {
	gatewayOrderPending := "sim_order_pending_001"
	gatewayOrderSucceeded := "sim_order_succeeded_001"
	gatewayPaymentSucceeded := "sim_payment_succeeded_001"

	attempts := []models.PaymentAttempt{
		{
			ID:          uuid.MustParse("20000000-0000-4000-8000-000000000001"),
			OrderID:     uuid.MustParse("30000000-0000-4000-8000-000000000001"),
			AmountPaise: 149900,
			Currency:    "INR",
			Status:      "CREATED",
			Gateway:     "sim",
		},
		{
			ID:             uuid.MustParse("20000000-0000-4000-8000-000000000002"),
			OrderID:        uuid.MustParse("30000000-0000-4000-8000-000000000002"),
			AmountPaise:    499900,
			Currency:       "INR",
			Status:         "PENDING",
			Gateway:        "sim",
			GatewayOrderID: &gatewayOrderPending,
		},
		{
			ID:               uuid.MustParse("20000000-0000-4000-8000-000000000003"),
			OrderID:          uuid.MustParse("30000000-0000-4000-8000-000000000003"),
			AmountPaise:      249900,
			Currency:         "INR",
			Status:           "SUCCEEDED",
			Gateway:          "sim",
			GatewayOrderID:   &gatewayOrderSucceeded,
			GatewayPaymentID: &gatewayPaymentSucceeded,
		},
	}

	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL search_path TO payments").Error; err != nil {
			return fmt.Errorf("select payments schema: %w", err)
		}

		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "order_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"amount_paise",
				"currency",
				"status",
				"gateway",
				"gateway_order_id",
				"gateway_payment_id",
				"failure_code",
				"updated_at",
			}),
		}).Create(&attempts).Error; err != nil {
			return fmt.Errorf("seed payment attempts: %w", err)
		}

		orderIDs := []uuid.UUID{attempts[0].OrderID, attempts[1].OrderID, attempts[2].OrderID}
		var count int64
		if err := tx.Model(&models.PaymentAttempt{}).Where("order_id IN ?", orderIDs).Count(&count).Error; err != nil {
			return fmt.Errorf("verify payment attempts: %w", err)
		}
		if count != 3 {
			return fmt.Errorf("verify seed: got %d payment attempts", count)
		}

		return nil
	})
}
