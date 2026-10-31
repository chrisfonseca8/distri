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
	cleanupReplyTest(t, db, orderID, messageID)

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

	if err := db.Table("orchestrator.sagas").Where("id = ?", orderID).Take(&advanced).Error; err != nil {
		t.Fatalf("reload advanced saga: %v", err)
	}
	if advanced.Version != 2 || advanced.CurrentStep != "TAKE_PAYMENT" {
		t.Fatalf("stale reply changed saga: %+v", advanced)
	}
}

func TestHandlerIgnoresReplyForInactiveStep(t *testing.T) {
	db := replyIntegrationDB(t)
	orderID := uuid.New()
	messageID := "inactive-step-reply-" + uuid.New().String()
	cleanupReplyTest(t, db, orderID, messageID)
	now := time.Now().UTC()
	seedReplySaga(t, db, models.Saga{
		ID: orderID, Status: "RUNNING", CurrentStep: "TAKE_PAYMENT", TotalPaise: 10000, Currency: "INR",
		Version: 1, DeadlineAt: now.Add(15 * time.Minute),
	}, models.SagaStep{
		SagaID: orderID, Step: "TAKE_PAYMENT", Status: "RUNNING", Attempts: 1, StartedAt: &now,
	})

	body := replyBody(t, orderID, messages.CREATE_ORDER, messages.Succeeded)
	if err := NewHandler(db, 30*time.Second).Handle(t.Context(), messaging.Delivery{MessageId: messageID, Body: body}); err != nil {
		t.Fatalf("handle inactive-step reply: %v", err)
	}

	var sagaRow models.Saga
	if err := db.Table("orchestrator.sagas").Where("id = ?", orderID).Take(&sagaRow).Error; err != nil {
		t.Fatal(err)
	}
	if sagaRow.Status != "RUNNING" || sagaRow.CurrentStep != "TAKE_PAYMENT" || sagaRow.Version != 1 {
		t.Fatalf("inactive-step reply changed saga: %+v", sagaRow)
	}
	var step models.SagaStep
	if err := db.Table("orchestrator.saga_steps").Where("saga_id = ? AND step = 'TAKE_PAYMENT'", orderID).Take(&step).Error; err != nil {
		t.Fatal(err)
	}
	if step.Status != "RUNNING" || step.Attempts != 1 {
		t.Fatalf("inactive-step reply changed current step: %+v", step)
	}
	assertReplyCounts(t, db, orderID, []string{messageID}, 1)
	assertReplyOutboxCount(t, db, orderID, 0)
}

func TestHandlerAcceptsLatePaymentSuccess(t *testing.T) {
	tests := []struct {
		name         string
		sagaStatus   string
		stepStatus   string
		attempts     int
		wantDeadline bool
	}{
		{name: "ambiguous step", sagaStatus: "RUNNING", stepStatus: "AMBIGUOUS", attempts: 1},
		{name: "stuck saga", sagaStatus: "STUCK", stepStatus: "AMBIGUOUS", attempts: 2, wantDeadline: true},
		{name: "attempt cap reached", sagaStatus: "STUCK", stepStatus: "RUNNING", attempts: 5, wantDeadline: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := replyIntegrationDB(t)
			orderID := uuid.New()
			messageID := "late-payment-success-" + uuid.New().String()
			cleanupReplyTest(t, db, orderID, messageID)
			now := time.Now().UTC()
			oldDeadline := now.Add(-time.Hour)
			seedReplySaga(t, db, models.Saga{
				ID: orderID, Status: test.sagaStatus, CurrentStep: "TAKE_PAYMENT", TotalPaise: 10000, Currency: "INR",
				Version: 1, DeadlineAt: oldDeadline,
			}, models.SagaStep{
				SagaID: orderID, Step: "TAKE_PAYMENT", Status: test.stepStatus, Attempts: test.attempts, StartedAt: &now,
			})

			handler := NewHandler(db, 30*time.Second, WithSagaDeadline(15*time.Minute))
			body := replyBody(t, orderID, messages.TAKE_PAYMENT, messages.Succeeded)
			if err := handler.Handle(t.Context(), messaging.Delivery{MessageId: messageID, Body: body}); err != nil {
				t.Fatalf("handle late payment success: %v", err)
			}

			var sagaRow models.Saga
			if err := db.Table("orchestrator.sagas").Where("id = ?", orderID).Take(&sagaRow).Error; err != nil {
				t.Fatal(err)
			}
			if sagaRow.Status != "RUNNING" || sagaRow.CurrentStep != "MARK_ORDER_PAID" || sagaRow.Version != 2 {
				t.Fatalf("saga after late payment success = %+v", sagaRow)
			}
			if test.wantDeadline && !sagaRow.DeadlineAt.After(now.Add(14*time.Minute)) {
				t.Fatalf("revived saga deadline = %v, want a fresh deadline", sagaRow.DeadlineAt)
			}

			var paymentStep models.SagaStep
			if err := db.Table("orchestrator.saga_steps").Where("saga_id = ? AND step = 'TAKE_PAYMENT'", orderID).Take(&paymentStep).Error; err != nil {
				t.Fatal(err)
			}
			if paymentStep.Status != "SUCCEEDED" || paymentStep.Attempts != test.attempts {
				t.Fatalf("payment step = %+v, want SUCCEEDED with %d attempts", paymentStep, test.attempts)
			}
			assertReplyCounts(t, db, orderID, []string{messageID}, 2)
			assertReplyOutboxCount(t, db, orderID, 1)
		})
	}
}

func TestHandlerMarksConflictingPaymentSuccessStuck(t *testing.T) {
	tests := []struct {
		name        string
		sagaStatus  string
		currentStep string
	}{
		{name: "compensation in progress", sagaStatus: "COMPENSATING", currentStep: "MARK_ORDER_CANCELLED"},
		{name: "compensation finished", sagaStatus: "COMPENSATED", currentStep: "RELEASE_STOCK"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := replyIntegrationDB(t)
			orderID := uuid.New()
			messageID := "conflicting-payment-success-" + uuid.New().String()
			cleanupReplyTest(t, db, orderID, messageID)
			now := time.Now().UTC()
			seedReplySaga(t, db, models.Saga{
				ID: orderID, Status: test.sagaStatus, CurrentStep: test.currentStep, TotalPaise: 10000, Currency: "INR",
				Version: 1, DeadlineAt: now.Add(15 * time.Minute),
			},
				models.SagaStep{SagaID: orderID, Step: "TAKE_PAYMENT", Status: "FAILED", Attempts: 1, StartedAt: &now, FinishedAt: &now},
				models.SagaStep{SagaID: orderID, Step: test.currentStep, Status: "RUNNING", Attempts: 1, StartedAt: &now},
			)

			body := replyBody(t, orderID, messages.TAKE_PAYMENT, messages.Succeeded)
			if err := NewHandler(db, 30*time.Second).Handle(t.Context(), messaging.Delivery{MessageId: messageID, Body: body}); err != nil {
				t.Fatalf("handle conflicting payment success: %v", err)
			}

			var sagaRow models.Saga
			if err := db.Table("orchestrator.sagas").Where("id = ?", orderID).Take(&sagaRow).Error; err != nil {
				t.Fatal(err)
			}
			if sagaRow.Status != "STUCK" || sagaRow.CurrentStep != test.currentStep || sagaRow.Version != 2 {
				t.Fatalf("saga after conflicting payment success = %+v", sagaRow)
			}
			assertReplyCounts(t, db, orderID, []string{messageID}, 2)
			assertReplyOutboxCount(t, db, orderID, 0)
		})
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

func assertReplyOutboxCount(t *testing.T, db *gorm.DB, orderID uuid.UUID, want int64) {
	t.Helper()
	var count int64
	if err := db.Table("orchestrator.outbox").
		Where("payload ->> 'order_id' = ?", orderID.String()).
		Count(&count).Error; err != nil {
		t.Fatalf("count reply outbox rows: %v", err)
	}
	if count != want {
		t.Fatalf("reply outbox rows = %d, want %d", count, want)
	}
}

func replyBody(t *testing.T, orderID uuid.UUID, step messages.Step, outcome messages.Outcome) []byte {
	t.Helper()
	body, err := json.Marshal(messages.Reply{OrderID: orderID, Step: step, Outcome: outcome})
	if err != nil {
		t.Fatalf("encode reply: %v", err)
	}
	return body
}

func seedReplySaga(t *testing.T, db *gorm.DB, sagaRow models.Saga, steps ...models.SagaStep) {
	t.Helper()
	if err := db.Table("orchestrator.sagas").Create(&sagaRow).Error; err != nil {
		t.Fatalf("create reply-test saga: %v", err)
	}
	for index := range steps {
		if err := db.Table("orchestrator.saga_steps").
			Omit(clause.Associations).
			Create(&steps[index]).Error; err != nil {
			t.Fatalf("create reply-test saga step %s: %v", steps[index].Step, err)
		}
	}
}

func replyIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("RUN_REPLY_INTEGRATION") != "1" {
		t.Skip("set RUN_REPLY_INTEGRATION=1 to run database integration tests")
	}
	t.Chdir(t.TempDir())
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

// FailStep is what lets the sweeper compensate a CREATE_ORDER that can no longer succeed:
// the saga must cancel the order first (it may exist) and only then release the stock.
func TestFailStepMovesACreateOrderSagaIntoCompensationOnce(t *testing.T) {
	db := replyIntegrationDB(t)
	orderID := uuid.New()
	cleanupReplyTest(t, db, orderID)
	now := time.Now().UTC()
	if err := db.Table("orchestrator.sagas").Create(&models.Saga{
		ID: orderID, Status: "RUNNING", CurrentStep: "CREATE_ORDER", TotalPaise: 10000, Currency: "INR", Version: 1,
		DeadlineAt: now.Add(15 * time.Minute),
	}).Error; err != nil {
		t.Fatalf("create saga: %v", err)
	}
	if err := db.Table("orchestrator.saga_steps").Omit(clause.Associations).Create(&models.SagaStep{
		SagaID: orderID, Step: "CREATE_ORDER", Status: "RUNNING", Attempts: 5, StartedAt: &now,
	}).Error; err != nil {
		t.Fatalf("create saga step: %v", err)
	}

	handler := NewHandler(db, 30*time.Second)
	if err := handler.FailStep(t.Context(), orderID, messages.CREATE_ORDER); err != nil {
		t.Fatalf("fail step: %v", err)
	}

	var saga models.Saga
	if err := db.Table("orchestrator.sagas").Where("id = ?", orderID).Take(&saga).Error; err != nil {
		t.Fatal(err)
	}
	if saga.Status != "COMPENSATING" || saga.CurrentStep != "MARK_ORDER_CANCELLED" {
		t.Fatalf("saga = %s at %s, want COMPENSATING at MARK_ORDER_CANCELLED (cancel the order, then release the stock)", saga.Status, saga.CurrentStep)
	}
	var routingKeys []string
	if err := db.Table("orchestrator.outbox").Where("payload ->> 'order_id' = ?", orderID.String()).Pluck("routing_key", &routingKeys).Error; err != nil {
		t.Fatal(err)
	}
	if len(routingKeys) != 1 || routingKeys[0] != messages.OrderCancelRoutingKey {
		t.Fatalf("queued commands = %v, want exactly one %s", routingKeys, messages.OrderCancelRoutingKey)
	}

	// A second call (the sweeper runs every few seconds) must change nothing.
	if err := handler.FailStep(t.Context(), orderID, messages.CREATE_ORDER); err != nil {
		t.Fatalf("second fail step: %v", err)
	}
	var count int64
	db.Table("orchestrator.outbox").Where("payload ->> 'order_id' = ?", orderID.String()).Count(&count)
	if count != 1 {
		t.Fatalf("queued commands after a repeat = %d, want still 1", count)
	}
}
