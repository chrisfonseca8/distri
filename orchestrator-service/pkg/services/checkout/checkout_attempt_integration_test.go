package checkout

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/orchestrator-service/database/models"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/config"
)

func TestClaimCheckoutAttemptIsIdempotentAndRejectsHashConflict(t *testing.T) {
	if os.Getenv("RUN_CHECKOUT_INTEGRATION") != "1" {
		t.Skip("set RUN_CHECKOUT_INTEGRATION=1 to run database integration tests")
	}

	db, err := config.OpenDB()
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := config.RequireDedicatedTestDB(db); err != nil {
		t.Skip(err)
	}

	key := "checkout-attempt-test-" + uuid.New().String()
	t.Cleanup(func() {
		if err := db.Table("orchestrator.checkout_attempts").
			Where("idempotency_key = ?", key).
			Delete(&models.CheckoutAttempt{}).Error; err != nil {
			t.Errorf("delete checkout attempt: %v", err)
		}
	})

	service := NewCheckoutService(db, nil, 15*time.Minute, 30*time.Second)
	prepared := preparedCheckoutRequest{Attempt: models.CheckoutAttempt{
		IdempotencyKey: key,
		RequestHash:    "same-hash",
		Status:         "in_progress",
	}}

	first, created, err := service.claimCheckoutAttempt(context.Background(), prepared)
	if err != nil {
		t.Fatalf("claim first attempt: %v", err)
	}
	if !created || first.OrderID == nil {
		t.Fatalf("first claim created = %t, order ID = %v", created, first.OrderID)
	}

	second, created, err := service.claimCheckoutAttempt(context.Background(), prepared)
	if err != nil {
		t.Fatalf("claim duplicate attempt: %v", err)
	}
	if created {
		t.Fatal("duplicate claim reported a new attempt")
	}
	if second.OrderID == nil || *second.OrderID != *first.OrderID {
		t.Fatalf("duplicate order ID = %v, want %v", second.OrderID, first.OrderID)
	}

	conflict := prepared
	conflict.Attempt.RequestHash = "different-hash"
	_, _, err = service.claimCheckoutAttempt(context.Background(), conflict)
	if !errors.Is(err, ErrIdempotencyKeyConflict) {
		t.Fatalf("conflict error = %v, want ErrIdempotencyKeyConflict", err)
	}
}
