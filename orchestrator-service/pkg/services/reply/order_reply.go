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
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/telemetry"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errConcurrentSagaTransition = errors.New("saga was moved concurrently")

type commandMapping struct {
	routingKey string
	build      func(models.Saga) (json.RawMessage, error)
}

var commandMappings = map[saga.Step]commandMapping{
	saga.TAKE_PAYMENT: {
		routingKey: messages.PaymentInitiateRoutingKey,
		build: func(sagaRow models.Saga) (json.RawMessage, error) {
			return marshalCommand(messages.PaymentInitiate{
				OrderID:     sagaRow.ID,
				AmountPaise: sagaRow.TotalPaise,
				Currency:    sagaRow.Currency,
			})
		},
	},
	saga.MARK_ORDER_PAID: {
		routingKey: messages.OrderMarkPaidRoutingKey,
		build: func(sagaRow models.Saga) (json.RawMessage, error) {
			return marshalCommand(messages.OrderMarkPaid{OrderID: sagaRow.ID})
		},
	},
	saga.MARK_ORDER_CANCELLED: {
		routingKey: messages.OrderCancelRoutingKey,
		build: func(sagaRow models.Saga) (json.RawMessage, error) {
			return marshalCommand(messages.OrderCancel{OrderID: sagaRow.ID})
		},
	},
	saga.CONFIRM_STOCK: {
		routingKey: messages.InventoryConfirmRoutingKey,
		build: func(sagaRow models.Saga) (json.RawMessage, error) {
			return marshalCommand(messages.InventoryConfirm{OrderID: sagaRow.ID})
		},
	},
	saga.REFUND_PAYMENT: {
		routingKey: messages.PaymentRefundRoutingKey,
		build: func(sagaRow models.Saga) (json.RawMessage, error) {
			return marshalCommand(messages.PaymentRefund{OrderID: sagaRow.ID})
		},
	},
	saga.CANCEL_PAID_ORDER: {
		routingKey: messages.OrderCancelRoutingKey,
		build: func(sagaRow models.Saga) (json.RawMessage, error) {
			return marshalCommand(messages.OrderCancel{OrderID: sagaRow.ID, Refunded: true})
		},
	},
	saga.RELEASE_STOCK: {
		routingKey: messages.InventoryReleaseRoutingKey,
		build: func(sagaRow models.Saga) (json.RawMessage, error) {
			return marshalCommand(messages.InventoryRelease{OrderID: sagaRow.ID})
		},
	},
}

// marshalCommand encodes a command payload as JSON.
func marshalCommand(payload any) (json.RawMessage, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return body, nil
}

type Handler struct {
	db           *gorm.DB
	stepTimeout  time.Duration
	sagaDeadline time.Duration
	telemetry    *telemetry.Recorder
	notifier     statusNotifier
}

type statusNotifier interface {
	Notify(orderID string)
}

// HandlerOption customises a Handler.
type HandlerOption func(*Handler)

// WithSagaDeadline gives a saga revived from STUCK by a late reply a fresh
// deadline of now+deadline. Without it the old deadline has usually passed and
// the sweeper would mark the saga STUCK again on its next pass.
func WithSagaDeadline(deadline time.Duration) HandlerOption {
	return func(h *Handler) { h.sagaDeadline = deadline }
}

// WithTelemetry records load-test timing without adding writes to the reply transaction.
func WithTelemetry(recorder *telemetry.Recorder) HandlerOption {
	return func(h *Handler) { h.telemetry = recorder }
}

// WithStatusNotifier publishes committed saga changes to local SSE subscribers.
func WithStatusNotifier(notifier statusNotifier) HandlerOption {
	return func(h *Handler) { h.notifier = notifier }
}

// NewHandler creates the handler for replies from other services.
func NewHandler(db *gorm.DB, stepTimeout time.Duration, options ...HandlerOption) *Handler {
	handler := &Handler{db: db, stepTimeout: stepTimeout}
	for _, option := range options {
		option(handler)
	}
	return handler
}

// FailStep applies a synthetic Failed reply for step, as if the participant had reported
// the failure itself. The saga sweeper uses it for a step that can no longer succeed
// before any money is involved, so the normal compensation path (cancel the order,
// release the stock) runs instead of leaving the saga STUCK with stock held. It is
// idempotent: a reply for a step that is no longer current is ignored.
func (h *Handler) FailStep(ctx context.Context, orderID uuid.UUID, step messages.Step) error {
	body, err := json.Marshal(messages.Reply{
		OrderID: orderID,
		Step:    step,
		Outcome: messages.Failed,
		Error:   "step could not complete before payment started",
	})
	if err != nil {
		return fmt.Errorf("encode synthetic reply: %w", err)
	}
	return h.Handle(ctx, messaging.Delivery{MessageId: uuid.New().String(), Body: body})
}

// Handle applies one reply to its saga, once per message ID, ignoring replies for steps that are no longer active.
func (h *Handler) Handle(ctx context.Context, delivery messaging.Delivery) error {
	receivedAt := time.Now()
	var reply messages.Reply
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

	h.telemetry.Increment("reply_received")
	var txStartedAt, txWorkFinishedAt, outboxCreatedAt time.Time
	var stateChanged bool
	err := h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txStartedAt = time.Now()
		defer func() { txWorkFinishedAt = time.Now() }()
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

		// A payment reported as successful after compensation started is not an
		// ordinary stale reply: the customer may have been charged while the order
		// and reservation are being cancelled. Stop automatic progress so an
		// operator can reconcile the payment and the compensation commands.
		if reply.Step == messages.TAKE_PAYMENT && reply.Outcome == messages.Succeeded &&
			(sagaRow.Status == string(saga.SagaCompensating) || sagaRow.Status == string(saga.SagaCompensated)) {
			now := time.Now().UTC()
			updateSaga := tx.Table("orchestrator.sagas").
				Where("id = ? AND version = ?", sagaRow.ID, sagaRow.Version).
				Updates(map[string]any{
					"status":     string(saga.SagaStuck),
					"version":    sagaRow.Version + 1,
					"updated_at": now,
				})
			if updateSaga.Error != nil {
				return fmt.Errorf("mark saga stuck after conflicting payment result: %w", updateSaga.Error)
			}
			if updateSaga.RowsAffected == 0 {
				return errConcurrentSagaTransition
			}
			stateChanged = true
			slog.Error(
				"saga STUCK: payment succeeded after compensation started",
				"order_id", reply.OrderID,
				"saga_status", sagaRow.Status,
				"current_step", sagaRow.CurrentStep,
			)
			return nil
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
		if currentStep.Status != "RUNNING" && currentStep.Status != "AMBIGUOUS" {
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
		if decision.NextStep == "" || decision.SagaStatus == saga.SagaStuck {
			nextCurrentStep = sagaRow.CurrentStep
		}
		sagaUpdates := map[string]any{
			"current_step": nextCurrentStep,
			"status":       string(decision.SagaStatus),
			"version":      sagaRow.Version + 1,
			"updated_at":   now,
		}
		revived := sagaRow.Status == string(saga.SagaStuck) &&
			(decision.SagaStatus == saga.SagaRunning || decision.SagaStatus == saga.SagaCompensating)
		if revived && h.sagaDeadline > 0 {
			sagaUpdates["deadline_at"] = now.Add(h.sagaDeadline)
		}
		updateSaga := tx.Table("orchestrator.sagas").
			Where("id = ? AND version = ?", sagaRow.ID, sagaRow.Version).
			Updates(sagaUpdates)
		if updateSaga.Error != nil {
			return fmt.Errorf("advance saga: %w", updateSaga.Error)
		}
		if updateSaga.RowsAffected == 0 {
			return errConcurrentSagaTransition
		}
		stateChanged = true

		mapping, hasCommandMapping := commandMappings[decision.Command]
		var payload json.RawMessage
		if decision.Command != "" && hasCommandMapping {
			payload, err = mapping.build(sagaRow)
			if err != nil {
				return fmt.Errorf("build %s command: %w", decision.Command, err)
			}
		}

		resend := decision.NextStep == saga.Step(currentStep.Step) && decision.Command != ""
		stepUpdates := map[string]any{
			"updated_at": now,
		}
		if resend {
			stepUpdates["status"] = "RUNNING"
			stepUpdates["attempts"] = gorm.Expr("attempts + 1")
			stepUpdates["started_at"] = now
			stepUpdates["finished_at"] = nil
			stepUpdates["next_attempt_at"] = now.Add(h.stepTimeout)
			if hasCommandMapping {
				stepUpdates["routing_key"] = mapping.routingKey
				stepUpdates["command_payload"] = datatypes.JSON(payload)
			}
		} else {
			stepStatus, err := finishedStepStatus(reply.Outcome)
			if err != nil {
				return err
			}
			stepUpdates["status"] = stepStatus
			stepUpdates["finished_at"] = now
		}
		finishStep := tx.Table("orchestrator.saga_steps").
			Where("id = ? AND status IN ?", currentStep.ID, []string{"RUNNING", "AMBIGUOUS"}).
			Updates(stepUpdates)
		if finishStep.Error != nil {
			return fmt.Errorf("finish saga step: %w", finishStep.Error)
		}
		if finishStep.RowsAffected == 0 {
			return errConcurrentSagaTransition
		}

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
				nextAttemptAt := now.Add(h.stepTimeout)
				nextStep.NextAttemptAt = &nextAttemptAt
				nextStep.RoutingKey = &mapping.routingKey
				nextStep.CommandPayload = datatypes.JSON(payload)
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

		if err := outbox.Enqueue(tx, outbox.Message{
			MessageID:  uuid.New(),
			RoutingKey: mapping.routingKey,
			Payload:    payload,
		}); err != nil {
			return err
		}
		outboxCreatedAt = time.Now()
		return nil
	})
	finishedAt := time.Now()
	h.recordTiming(receivedAt, txStartedAt, outboxCreatedAt, txWorkFinishedAt, finishedAt, err)
	if err == nil {
		if stateChanged && h.notifier != nil {
			h.notifier.Notify(reply.OrderID.String())
		}
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

func (h *Handler) recordTiming(receivedAt, txStartedAt, outboxCreatedAt, txWorkFinishedAt, finishedAt time.Time, err error) {
	if h.telemetry == nil || txStartedAt.IsZero() {
		return
	}
	h.telemetry.Observe("reply_received_to_tx_started", txStartedAt.Sub(receivedAt))
	if !txWorkFinishedAt.IsZero() {
		h.telemetry.Observe("reply_transaction_work", txWorkFinishedAt.Sub(txStartedAt))
		h.telemetry.Observe("reply_transaction_finalize", finishedAt.Sub(txWorkFinishedAt))
	}
	h.telemetry.Observe("reply_received_to_transaction_finished", finishedAt.Sub(receivedAt))
	if !outboxCreatedAt.IsZero() {
		h.telemetry.Increment("reply_outbox_created")
		h.telemetry.Observe("reply_tx_started_to_outbox_created", outboxCreatedAt.Sub(txStartedAt))
		if !txWorkFinishedAt.IsZero() {
			h.telemetry.Observe("reply_outbox_created_to_tx_work_finished", txWorkFinishedAt.Sub(outboxCreatedAt))
		}
	}
	if err == nil {
		h.telemetry.Increment("reply_transaction_committed")
	} else {
		h.telemetry.Increment("reply_transaction_failed")
	}
}

// finishedStepStatus maps a reply outcome to the saga step status.
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

// isPermanentDatabaseError reports whether a database error can never succeed on retry.
func isPermanentDatabaseError(err error) bool {
	var databaseError sqlStateError
	if !errors.As(err, &databaseError) {
		return false
	}
	class := databaseError.SQLState()
	return strings.HasPrefix(class, "22") || strings.HasPrefix(class, "23")
}
