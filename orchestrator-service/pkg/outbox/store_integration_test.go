package outbox

import (
	"context"
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

func TestUnpublishableRowStopsAtLimitAndCanBeRequeued(t *testing.T) {
	db := edgeDB(t)
	messageID := uuid.New()
	if err := db.Transaction(func(tx *gorm.DB) error {
		return Enqueue(tx, Message{MessageID: messageID, RoutingKey: "no.binding", Payload: json.RawMessage(`{"poison":true}`)})
	}); err != nil {
		t.Fatalf("enqueue poison row: %v", err)
	}
	t.Cleanup(func() { db.Where("message_id = ?", messageID).Delete(&models.Outbox{}) })

	store := NewStore(db)
	const worker = "poison-test"
	for attempt := 1; attempt <= 3; attempt++ {
		rows, err := store.Claim(context.Background(), worker, 1, time.Minute)
		if err != nil || len(rows) != 1 {
			t.Fatalf("claim %d = %d rows, %v", attempt, len(rows), err)
		}
		updated, dead, err := store.RecordUnpublishable(context.Background(), rows[0].ID, worker, 3, 0, "unroutable")
		if err != nil || !updated || dead != (attempt == 3) {
			t.Fatalf("failure %d: updated=%v dead=%v err=%v", attempt, updated, dead, err)
		}
	}

	rows, err := store.Claim(context.Background(), worker, 1, time.Minute)
	if err != nil || len(rows) != 0 {
		t.Fatalf("dead row was claimable: %d rows, %v", len(rows), err)
	}
	var row models.Outbox
	if err := db.Where("message_id = ?", messageID).First(&row).Error; err != nil {
		t.Fatalf("load dead row: %v", err)
	}
	if row.DeadAt == nil || row.UnpublishableAttempts != 3 {
		t.Fatalf("dead state = dead_at %v, failures %d", row.DeadAt, row.UnpublishableAttempts)
	}

	requeued, err := store.Requeue(context.Background(), row.ID)
	if err != nil || !requeued {
		t.Fatalf("requeue = %v, %v", requeued, err)
	}
	rows, err = store.Claim(context.Background(), worker, 1, time.Minute)
	if err != nil || len(rows) != 1 || rows[0].UnpublishableAttempts != 0 || rows[0].DeadAt != nil {
		t.Fatalf("claim after requeue = %+v, %v", rows, err)
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
