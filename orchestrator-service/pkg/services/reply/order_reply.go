package reply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/orchestrator-service/database/models"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/messages"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/messaging"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/outbox"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/saga"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errConcurrentSagaTransition = errors.New("saga was moved concurrently")

type commandMapping struct {
	routingKey string
	build      func(models.Saga) (json.RawMessage, error)
}

// OR6 adds TAKE_PAYMENT and its payload builder. Until then, decisions for
// commands without a mapping deliberately leave the next step PENDING.
var commandMappings = map[saga.Step]commandMapping{}

type Handler struct {
	db *gorm.DB
}

func NewHandler(db *gorm.DB) *Handler {
	return &Handler{db: db}
}

func (h *Handler) Handle(ctx context.Context, delivery messaging.Delivery) error {
	var reply messages.OrderReply
	if err := json.Unmarshal(delivery.Body, &reply); err != nil {
		return messaging.Permanent(fmt.Errorf("decode order reply: %w", err))
	}
	if reply.OrderID == uuid.Nil() {
		return messaging.Permanent(errors.New("order reply order_id is required"))
	}

	messageID := strings.TrimSpace(delivery.MessageId)
	if messageID == "" {
		return messaging.Permanent(errors.New("order reply AMQP MessageId is required"))
	}

	err := h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		insertProcessed := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(
			&models.ProcessedMessage{MessageID: messageID},
		)
		if insertProcessed.Error != nil {
			return fmt.Errorf("record processed order reply: %w", insertProcessed.Error)
		}
		if insertProcessed.RowsAffected == 0 {
			return nil
		}

		var sagaRow models.Saga
		loadSaga := tx.Table("orchestrator.sagas").
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", reply.OrderID).
			Take(&sagaRow)
		if loadSaga.Error != nil {
			return fmt.Errorf("load order reply saga: %w", loadSaga.Error)
		}

		if reply.Step != messages.Step(sagaRow.CurrentStep) {
			slog.Warn(
				"ignore order reply for inactive saga step",
				"order_id", reply.OrderID,
				"reply_step", reply.Step,
				"current_step", sagaRow.CurrentStep,
			)
			return nil
		}

		var currentStep models.SagaStep
		loadStep := tx.Table("orchestrator.saga_steps").
			Where("saga_id = ? AND step = ?", sagaRow.ID, sagaRow.CurrentStep).
			Take(&currentStep)
		if errors.Is(loadStep.Error, gorm.ErrRecordNotFound) {
			slog.Warn(
				"ignore order reply for missing saga step",
				"order_id", reply.OrderID,
				"step", reply.Step,
			)
			return nil
		}
		if loadStep.Error != nil {
			return fmt.Errorf("load current saga step: %w", loadStep.Error)
		}
		if currentStep.Status != "RUNNING" {
			slog.Warn(
				"ignore order reply for non-running saga step",
				"order_id", reply.OrderID,
				"step", reply.Step,
				"step_status", currentStep.Status,
			)
			return nil
		}

		decision, err := saga.Next(
			saga.Step(reply.Step),
			saga.Outcome(reply.Outcome),
		)
		if err != nil {
			return err
		}

		now := time.Now().UTC()
		nextCurrentStep := string(decision.NextStep)
		if decision.SagaStatus == saga.SagaStuck {
			nextCurrentStep = sagaRow.CurrentStep
		}
		updateSaga := tx.Table("orchestrator.sagas").
			Where("id = ? AND version = ?", sagaRow.ID, sagaRow.Version).
			Updates(map[string]any{
				"current_step": nextCurrentStep,
				"status":       string(decision.SagaStatus),
				"version":      sagaRow.Version + 1,
				"updated_at":   now,
			})
		if updateSaga.Error != nil {
			return fmt.Errorf("advance saga: %w", updateSaga.Error)
		}
		if updateSaga.RowsAffected == 0 {
			return errConcurrentSagaTransition
		}

		stepStatus, err := finishedStepStatus(reply.Outcome)
		if err != nil {
			return err
		}
		finishStep := tx.Table("orchestrator.saga_steps").
			Where("id = ? AND status = ?", currentStep.ID, "RUNNING").
			Updates(map[string]any{
				"status":      stepStatus,
				"finished_at": now,
				"updated_at":  now,
			})
		if finishStep.Error != nil {
			return fmt.Errorf("finish saga step: %w", finishStep.Error)
		}
		if finishStep.RowsAffected == 0 {
			return errConcurrentSagaTransition
		}

		mapping, hasCommandMapping := commandMappings[decision.Command]
		if decision.NextStep != "" && decision.NextStep != saga.Step(currentStep.Step) {
			nextStep := models.SagaStep{
				SagaID: sagaRow.ID,
				Step:   string(decision.NextStep),
				Status: "PENDING",
			}
			if hasCommandMapping {
				nextStep.Status = "RUNNING"
				nextStep.Attempts = 1
				nextStep.StartedAt = &now
			}
			if err := tx.Table("orchestrator.saga_steps").
				Omit(clause.Associations).
				Create(&nextStep).Error; err != nil {
				return fmt.Errorf("create next saga step: %w", err)
			}
		}

		if decision.Command == "" {
			return nil
		}
		if !hasCommandMapping {
			slog.Info(
				"no command mapping yet",
				"order_id", sagaRow.ID,
				"command", decision.Command,
			)
			return nil
		}

		payload, err := mapping.build(sagaRow)
		if err != nil {
			return fmt.Errorf("build %s command: %w", decision.Command, err)
		}
		if err := outbox.Enqueue(tx, outbox.Message{
			MessageID:  uuid.New(),
			RoutingKey: mapping.routingKey,
			Payload:    payload,
		}); err != nil {
			return err
		}
		return nil
	})
	if err == nil {
		return nil
	}

	wrapped := fmt.Errorf("process order reply: %w", err)
	if errors.Is(err, gorm.ErrRecordNotFound) ||
		errors.Is(err, saga.ErrInvalidTransition) ||
		isPermanentDatabaseError(err) {
		return messaging.Permanent(wrapped)
	}
	return messaging.Transient(wrapped)
}

func finishedStepStatus(outcome messages.Outcome) (string, error) {
	switch outcome {
	case messages.Succeeded:
		return "SUCCEEDED", nil
	case messages.Failed:
		return "FAILED", nil
	case messages.Ambiguous:
		return "AMBIGUOUS", nil
	default:
		return "", fmt.Errorf("unsupported order reply outcome %q", outcome)
	}
}

type sqlStateError interface {
	SQLState() string
}

func isPermanentDatabaseError(err error) bool {
	var databaseError sqlStateError
	if !errors.As(err, &databaseError) {
		return false
	}
	class := databaseError.SQLState()
	return strings.HasPrefix(class, "22") || strings.HasPrefix(class, "23")
}
