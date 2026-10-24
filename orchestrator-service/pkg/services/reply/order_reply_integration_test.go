package reply

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/orchestrator-service/database/models"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/config"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/messages"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/messaging"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestHandlerAdvancesSagaAndIsIdempotent(t *testing.T) {
	db := replyIntegrationDB(t)
	orderID := uuid.New()
	messageID := "order-reply-test-" + uuid.New().String()
	guardMessageID := "order-reply-guard-test-" + uuid.New().String()
	cleanupReplyTest(t, db, orderID, messageID, guardMessageID)

	now := time.Now().UTC()
	sagaRow := models.Saga{
		ID:          orderID,
		Status:      "RUNNING",
		CurrentStep: "CREATE_ORDER",
		TotalPaise:  10000,
		Currency:    "INR",
		Version:     1,
		DeadlineAt:  now.Add(15 * time.Minute),
	}
	if err := db.Table("orchestrator.sagas").Create(&sagaRow).Error; err != nil {
		t.Fatalf("create saga: %v", err)
	}
	step := models.SagaStep{
		SagaID:    orderID,
		Step:      "CREATE_ORDER",
		Status:    "RUNNING",
		Attempts:  1,
		StartedAt: &now,
	}
	if err := db.Table("orchestrator.saga_steps").
		Omit(clause.Associations).
		Create(&step).Error; err != nil {
		t.Fatalf("create saga step: %v", err)
	}

	body, err := json.Marshal(messages.Reply{
		OrderID: orderID,
		Step:    messages.CREATE_ORDER,
		Outcome: messages.Succeeded,
	})
	if err != nil {
		t.Fatalf("encode reply: %v", err)
	}
	handler := NewHandler(db, 30*time.Second)
	delivery := messaging.Delivery{MessageId: messageID, Body: body}
	if err := handler.Handle(t.Context(), delivery); err != nil {
		t.Fatalf("handle reply: %v", err)
	}

	var advanced models.Saga
	if err := db.Table("orchestrator.sagas").Where("id = ?", orderID).Take(&advanced).Error; err != nil {
		t.Fatalf("load advanced saga: %v", err)
	}
	if advanced.CurrentStep != "TAKE_PAYMENT" || advanced.Status != "RUNNING" || advanced.Version != 2 {
		t.Fatalf("advanced saga = %+v", advanced)
	}
	if advanced.TotalPaise != 10000 || advanced.Currency != "INR" {
		t.Fatalf("saga checkout values changed: %+v", advanced)
	}

	var steps []models.SagaStep
	if err := db.Table("orchestrator.saga_steps").
		Where("saga_id = ?", orderID).
		Order("created_at").
		Find(&steps).Error; err != nil {
		t.Fatalf("load saga steps: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("step count = %d, want 2", len(steps))
	}
	if steps[0].Step != "CREATE_ORDER" || steps[0].Status != "SUCCEEDED" || steps[0].FinishedAt == nil {
		t.Fatalf("finished step = %+v", steps[0])
	}
	// TAKE_PAYMENT has a command mapping: the step starts RUNNING (attempt 1, resend deadline set)
	// and its command is enqueued in the same transaction.
	if steps[1].Step != "TAKE_PAYMENT" || steps[1].Status != "RUNNING" || steps[1].Attempts != 1 || steps[1].NextAttemptAt == nil {
		t.Fatalf("next step = %+v", steps[1])
	}

	var outboxCount int64
	if err := db.Table("orchestrator.outbox").
		Where("payload ->> 'order_id' = ?", orderID.String()).
		Count(&outboxCount).Error; err != nil {
		t.Fatalf("count outbox rows: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("outbox rows = %d, want exactly 1 payment command", outboxCount)
	}

	if err := handler.Handle(t.Context(), delivery); err != nil {
		t.Fatalf("handle duplicate reply: %v", err)
	}
	assertReplyCounts(t, db, orderID, []string{messageID}, 2)
	if err := db.Table("orchestrator.outbox").Where("payload ->> 'order_id' = ?", orderID.String()).Count(&outboxCount).Error; err != nil || outboxCount != 1 {
		t.Fatalf("outbox rows after duplicate = %d (%v), want still 1", outboxCount, err)
	}

	if err := handler.Handle(t.Context(), messaging.Delivery{
		MessageId: guardMessageID,
		Body:      body,
	}); err != nil {
		t.Fatalf("handle stale reply: %v", err)
	}
	assertReplyCounts(t, db, orderID, []string{messageID, guardMessageID}, 2)

	if err := db.Table("orchestrator.sagas").Where("id = ?", orderID).Take(&advanced).Error; err != nil {
		t.Fatalf("reload advanced saga: %v", err)
	}
	if advanced.Version != 2 || advanced.CurrentStep != "TAKE_PAYMENT" {
		t.Fatalf("stale reply changed saga: %+v", advanced)
	}
}

func TestHandlerTreatsMissingSagaAsPermanentAndRollsBackMessage(t *testing.T) {
	db := replyIntegrationDB(t)
	messageID := "order-reply-missing-test-" + uuid.New().String()
	t.Cleanup(func() {
		db.Table("orchestrator.processed_messages").Where("message_id = ?", messageID).
			Delete(&models.ProcessedMessage{})
	})
	body, err := json.Marshal(messages.Reply{
		OrderID: uuid.New(), Step: messages.CREATE_ORDER, Outcome: messages.Succeeded,
	})
	if err != nil {
		t.Fatalf("encode reply: %v", err)
	}
	err = NewHandler(db, 30*time.Second).Handle(t.Context(), messaging.Delivery{MessageId: messageID, Body: body})
	var permanent *messaging.PermanentError
	if !errors.As(err, &permanent) {
		t.Fatalf("error = %v, want PermanentError", err)
	}

	var count int64
	if err := db.Table("orchestrator.processed_messages").
		Where("message_id = ?", messageID).
		Count(&count).Error; err != nil {
		t.Fatalf("count processed messages: %v", err)
	}
	if count != 0 {
		t.Fatalf("processed rows after rollback = %d, want 0", count)
	}
}

func assertReplyCounts(
	t *testing.T,
	db *gorm.DB,
	orderID uuid.UUID,
	messageIDs []string,
	stepCount int64,
) {
	t.Helper()
	var count int64
	if err := db.Table("orchestrator.saga_steps").Where("saga_id = ?", orderID).Count(&count).Error; err != nil {
		t.Fatalf("count saga steps: %v", err)
	}
	if count != stepCount {
		t.Fatalf("saga step count = %d, want %d", count, stepCount)
	}
	if err := db.Table("orchestrator.processed_messages").
		Where("message_id IN ?", messageIDs).
		Count(&count).Error; err != nil {
		t.Fatalf("count processed messages: %v", err)
	}
	if count != int64(len(messageIDs)) {
		t.Fatalf("processed message count = %d, want %d", count, len(messageIDs))
	}
}

func replyIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("RUN_REPLY_INTEGRATION") != "1" {
		t.Skip("set RUN_REPLY_INTEGRATION=1 to run database integration tests")
	}
	db, err := config.OpenDB()
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := config.RequireDedicatedTestDB(db); err != nil {
		t.Skip(err)
	}
	return db
}

func cleanupReplyTest(t *testing.T, db *gorm.DB, orderID uuid.UUID, messageIDs ...string) {
	t.Helper()
	t.Cleanup(func() {
		if err := db.Table("orchestrator.outbox").
			Where("payload ->> 'order_id' = ?", orderID.String()).
			Delete(&models.Outbox{}).Error; err != nil {
			t.Errorf("delete outbox rows: %v", err)
		}
		if err := db.Table("orchestrator.processed_messages").
			Where("message_id IN ?", messageIDs).
			Delete(&models.ProcessedMessage{}).Error; err != nil {
			t.Errorf("delete processed messages: %v", err)
		}
		if err := db.Table("orchestrator.sagas").
			Where("id = ?", orderID).
			Delete(&models.Saga{}).Error; err != nil {
			t.Errorf("delete saga: %v", err)
		}
	})
}
