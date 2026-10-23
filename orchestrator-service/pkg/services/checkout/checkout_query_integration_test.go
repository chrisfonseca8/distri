package checkout

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/orchestrator-service/database/models"
	"gorm.io/gorm/clause"
)

func TestGetCheckoutReturnsSagaAndOrderedSteps(t *testing.T) {
	db := checkoutIntegrationDB(t)
	orderID := uuid.New()
	now := time.Now().UTC()
	t.Cleanup(func() {
		if err := db.Table("orchestrator.sagas").Where("id = ?", orderID).
			Delete(&models.Saga{}).Error; err != nil {
			t.Errorf("delete checkout query test saga: %v", err)
		}
	})

	sagaRow := models.Saga{
		ID:          orderID,
		Status:      "RUNNING",
		CurrentStep: "TAKE_PAYMENT",
		TotalPaise:  10000,
		Currency:    "INR",
		Version:     2,
		DeadlineAt:  now.Add(15 * time.Minute),
	}
	if err := db.Table("orchestrator.sagas").Create(&sagaRow).Error; err != nil {
		t.Fatalf("create checkout query test saga: %v", err)
	}

	started := now.Add(-time.Minute)
	finished := now.Add(-30 * time.Second)
	lastError := "payment timed out"
	steps := []models.SagaStep{
		{
			SagaID: orderID, Step: "RESERVE_STOCK", Status: "SUCCEEDED",
			StartedAt: &started, FinishedAt: &finished, CreatedAt: now.Add(-3 * time.Second),
		},
		{
			SagaID: orderID, Step: "CREATE_ORDER", Status: "SUCCEEDED", Attempts: 1,
			StartedAt: &started, FinishedAt: &finished, CreatedAt: now.Add(-2 * time.Second),
		},
		{
			SagaID: orderID, Step: "TAKE_PAYMENT", Status: "PENDING", Attempts: 2,
			LastError: &lastError, CreatedAt: now.Add(-time.Second),
		},
	}
	if err := db.Table("orchestrator.saga_steps").Omit(clause.Associations).
		Create(&steps).Error; err != nil {
		t.Fatalf("create checkout query test steps: %v", err)
	}

	result, err := NewCheckoutService(db, nil, 0, 0).GetCheckout(context.Background(), orderID)
	if err != nil {
		t.Fatalf("get checkout: %v", err)
	}
	if result.OrderID != orderID || result.Status != "RUNNING" ||
		result.CurrentStep != "TAKE_PAYMENT" || result.TotalPaise != 10000 || result.Currency != "INR" {
		t.Fatalf("checkout result = %+v", result)
	}
	if len(result.Steps) != 3 {
		t.Fatalf("step count = %d, want 3", len(result.Steps))
	}
	for index, step := range []string{"RESERVE_STOCK", "CREATE_ORDER", "TAKE_PAYMENT"} {
		if result.Steps[index].Step != step {
			t.Fatalf("step %d = %q, want %q", index, result.Steps[index].Step, step)
		}
	}
	if result.Steps[2].LastError == nil || *result.Steps[2].LastError != lastError {
		t.Fatalf("last step = %+v", result.Steps[2])
	}
}

func TestGetCheckoutReturnsNotFound(t *testing.T) {
	db := checkoutIntegrationDB(t)
	_, err := NewCheckoutService(db, nil, 0, 0).GetCheckout(context.Background(), uuid.New())
	if !errors.Is(err, ErrCheckoutNotFound) {
		t.Fatalf("error = %v, want ErrCheckoutNotFound", err)
	}
}
