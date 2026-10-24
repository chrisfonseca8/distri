package outbox

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/orchestrator-service/database/models"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/config"
	"gorm.io/gorm"
)

func TestEnqueueUsesCallerTransaction(t *testing.T) {
	if os.Getenv("RUN_OUTBOX_INTEGRATION") != "1" {
		t.Skip("set RUN_OUTBOX_INTEGRATION=1 to run database integration tests")
	}

	db, err := config.OpenDB()
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := config.RequireDedicatedTestDB(db); err != nil {
		t.Skip(err)
	}

	rolledBackID := uuid.New()
	rolledBackSagaID := uuid.New()
	rollback := errors.New("roll back test transaction")
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := createTestSaga(tx, rolledBackSagaID); err != nil {
			return err
		}
		if err := Enqueue(tx, Message{
			MessageID:  rolledBackID,
			RoutingKey: "order.create",
			Payload:    json.RawMessage(`{"order_id":"rollback-test"}`),
		}); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("transaction error = %v, want rollback marker", err)
	}

	var count int64
	if err := db.Model(&models.Outbox{}).
		Where("message_id = ?", rolledBackID).
		Count(&count).Error; err != nil {
		t.Fatalf("count rolled-back message: %v", err)
	}
	if count != 0 {
		t.Fatalf("rolled-back transaction retained %d outbox rows", count)
	}
	count = 0
	if err := db.Model(&models.Saga{}).
		Where("id = ?", rolledBackSagaID).
		Count(&count).Error; err != nil {
		t.Fatalf("count rolled-back saga: %v", err)
	}
	if count != 0 {
		t.Fatalf("rolled-back transaction retained %d sagas", count)
	}

	committedID := uuid.New()
	committedSagaID := uuid.New()
	t.Cleanup(func() {
		if err := db.Where("message_id = ?", committedID).Delete(&models.Outbox{}).Error; err != nil {
			t.Errorf("delete committed test message: %v", err)
		}
		if err := db.Where("id = ?", committedSagaID).Delete(&models.Saga{}).Error; err != nil {
			t.Errorf("delete committed test saga: %v", err)
		}
	})
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := createTestSaga(tx, committedSagaID); err != nil {
			return err
		}
		return Enqueue(tx, Message{
			MessageID:  committedID,
			RoutingKey: "order.create",
			Payload:    json.RawMessage(`{"order_id":"commit-test"}`),
		})
	}); err != nil {
		t.Fatalf("commit transaction: %v", err)
	}

	count = 0
	if err := db.Model(&models.Outbox{}).
		Where("message_id = ?", committedID).
		Count(&count).Error; err != nil {
		t.Fatalf("count committed message: %v", err)
	}
	if count != 1 {
		t.Fatalf("committed transaction has %d outbox rows, want 1", count)
	}
	count = 0
	if err := db.Model(&models.Saga{}).
		Where("id = ?", committedSagaID).
		Count(&count).Error; err != nil {
		t.Fatalf("count committed saga: %v", err)
	}
	if count != 1 {
		t.Fatalf("committed transaction has %d sagas, want 1", count)
	}
}

func createTestSaga(tx *gorm.DB, sagaID uuid.UUID) error {
	if err := tx.Create(&models.Saga{
		ID:          sagaID,
		Status:      "RUNNING",
		CurrentStep: "CREATE_ORDER",
		DeadlineAt:  time.Now().Add(time.Minute),
	}).Error; err != nil {
		return err
	}
	return tx.Omit("Saga").Create(&models.SagaStep{
		SagaID: sagaID,
		Step:   "CREATE_ORDER",
		Status: "RUNNING",
	}).Error
}
