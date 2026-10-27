package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/payment-service/database/models"
	"github.com/chrisfonseca08/distri/payment-service/pkg/payment_gateway"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type WebhookProcessorConfig struct {
	// GatewayName is this process's gateway environment ("razorpay" or
	// "razorpay-sim"). The worker only applies events to attempts and compensation
	// records of its own environment.
	GatewayName   string
	Interval      time.Duration
	Batch         int
	Lease         time.Duration
	MaxAttempts   int
	BackoffBase   time.Duration
	BackoffCap    time.Duration
	PaymentWindow time.Duration
}

// WebhookProcessor applies events from the durable inbox. An event is marked
// PROCESSED in the same transaction as its business effects, so a crash can never
// leave an event marked done without its effect, nor an effect without the mark.
// It reuses decideReconcile and applyDecision, the same rules polling uses.
type WebhookProcessor struct {
	db       *gorm.DB
	config   WebhookProcessorConfig
	workerID string
	now      func() time.Time
	// compensation applies refund events; nil when the gateway cannot refund.
	compensation *CompensationWorker
}

// WithCompensation lets the processor apply refund webhooks to compensation records.
func (p *WebhookProcessor) WithCompensation(worker *CompensationWorker) *WebhookProcessor {
	p.compensation = worker
	return p
}

// NewWebhookProcessor creates the processor for webhook events of one gateway.
func NewWebhookProcessor(db *gorm.DB, config WebhookProcessorConfig) (*WebhookProcessor, error) {
	if db == nil {
		return nil, errors.New("webhook processor database is required")
	}
	if strings.TrimSpace(config.GatewayName) == "" {
		return nil, errors.New("webhook processor needs its gateway environment name")
	}
	if config.Interval <= 0 || config.Batch <= 0 || config.Lease <= 0 || config.MaxAttempts <= 0 ||
		config.BackoffBase <= 0 || config.BackoffCap < config.BackoffBase || config.PaymentWindow <= 0 {
		return nil, errors.New("invalid webhook processor configuration")
	}
	host, _ := os.Hostname()
	return &WebhookProcessor{
		db: db, config: config, now: time.Now,
		workerID: fmt.Sprintf("%s-%d-%s", host, os.Getpid(), uuid.New().String()[:8]),
	}, nil
}

const webhookIdleDelay = 100 * time.Millisecond

// Run drains consecutive batches without sleeping while events are available.
// When the inbox is empty it polls at a modest rate; pass-level failures use an
// exponential backoff capped by the configured interval. Event-level failures
// retain their existing durable retry schedule in reschedule.
func (p *WebhookProcessor) Run(ctx context.Context) {
	slog.Info("webhook processor started",
		"idle_delay", webhookIdleDelay,
		"error_backoff_cap", p.config.Interval,
		"batch", p.config.Batch,
		"max_attempts", p.config.MaxAttempts,
	)
	defer slog.Info("webhook processor stopped")
	runWebhookProcessor(ctx, webhookIdleDelay, p.config.Interval, p.processOnce)
}

func runWebhookProcessor(ctx context.Context, idleDelay, errorBackoffCap time.Duration, process func(context.Context) (int, error)) {
	errorDelay := idleDelay
	for {
		claimed, err := process(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			slog.Error("webhook processor pass failed", "error", err)
			if !waitForWebhookPass(ctx, errorDelay) {
				return
			}
			errorDelay = nextWebhookErrorDelay(errorDelay, errorBackoffCap)
			continue
		}

		errorDelay = idleDelay
		if claimed > 0 {
			// Do not impose the idle polling delay on an inbox that may still have
			// due rows. The next claim also catches events received while this batch
			// was being applied.
			continue
		}
		if !waitForWebhookPass(ctx, idleDelay) {
			return
		}
	}
}

func waitForWebhookPass(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func nextWebhookErrorDelay(current, cap time.Duration) time.Duration {
	if cap < webhookIdleDelay {
		cap = webhookIdleDelay
	}
	if current >= cap {
		return cap
	}
	next := current * 2
	if next > cap {
		return cap
	}
	return next
}

const claimWebhookQuery = `
WITH due AS (
	SELECT id
	FROM payments.webhook_events
	WHERE status = 'PENDING'
		AND next_attempt_at <= now()
		AND (locked_until IS NULL OR locked_until < now())
	ORDER BY next_attempt_at, received_at
	FOR UPDATE SKIP LOCKED
	LIMIT ?
)
UPDATE payments.webhook_events AS events
SET locked_by = ?,
	locked_until = now() + (? * interval '1 microsecond'),
	attempts = events.attempts + 1
FROM due
WHERE events.id = due.id
RETURNING events.*`

// parkNoMapping marks an event parked only because no payment knew its gateway
// order yet; those are replayed automatically once the mapping appears.
const parkNoMapping = "NO_MAPPING"

// ReplayParked puts events that were parked for want of a gateway-order mapping
// back in the queue once a payment knows that gateway order. It is idempotent:
// applying an event twice changes nothing (guarded decisions), and with no parked
// events it does nothing. Events parked for any other reason (malformed,
// unsupported) are left for a person.
func (p *WebhookProcessor) ReplayParked(ctx context.Context) (int64, error) {
	result := p.db.WithContext(ctx).Exec(`
UPDATE payments.webhook_events AS e
SET status = 'PENDING', attempts = 0, next_attempt_at = now(), park_reason = NULL,
	locked_by = NULL, locked_until = NULL, processed_at = NULL,
	last_error = 'replayed: gateway order mapping appeared'
WHERE e.status = 'PARKED'
	AND e.park_reason = ?
	AND e.gateway_order_id IS NOT NULL
	AND EXISTS (
		SELECT 1 FROM payments.payment_attempts AS a WHERE a.gateway_order_id = e.gateway_order_id
	)`, parkNoMapping)
	if result.Error != nil {
		return 0, fmt.Errorf("replay parked webhook events: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		slog.Warn("replayed parked webhook events", "count", result.RowsAffected)
	}
	return result.RowsAffected, nil
}

// ProcessOnce replays parked events whose mapping now exists, then claims and
// applies one batch of due events.
func (p *WebhookProcessor) ProcessOnce(ctx context.Context) error {
	_, err := p.processOnce(ctx)
	return err
}

// processOnce returns the number of events claimed so Run can distinguish an
// idle inbox from one that should be drained immediately. ProcessOnce remains
// the stable one-pass entry point used by tests and administrative callers.
func (p *WebhookProcessor) processOnce(ctx context.Context) (int, error) {
	if _, err := p.ReplayParked(ctx); err != nil {
		return 0, err
	}
	var claimed []models.WebhookEvent
	if err := p.db.WithContext(ctx).
		Raw(claimWebhookQuery, p.config.Batch, p.workerID, p.config.Lease.Microseconds()).
		Scan(&claimed).Error; err != nil {
		return 0, fmt.Errorf("claim webhook events: %w", err)
	}
	for _, event := range claimed {
		if ctx.Err() != nil {
			return len(claimed), ctx.Err()
		}
		p.handle(ctx, event)
	}
	return len(claimed), nil
}

type webhookOutcome int

const (
	outcomeProcessed webhookOutcome = iota
	outcomeRetry                    // the payment is not known (yet): try again later
	outcomePark                     // cannot be applied: keep for review
)

type webhookResult struct {
	outcome        webhookOutcome
	reason         string
	parkReason     string // set with outcomePark and with a retry that may end parked
	gatewayOrderID string
}

var errLeaseLost = errors.New("webhook event lease was lost")

// handle applies one event in a transaction together with its PROCESSED mark, or parks or retries it.
func (p *WebhookProcessor) handle(ctx context.Context, event models.WebhookEvent) {
	var result webhookResult
	err := p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		result, err = p.apply(tx, event)
		if err != nil {
			return err
		}
		if result.outcome != outcomeProcessed {
			return nil
		}
		// Effects and the PROCESSED mark commit together or not at all.
		return p.finish(tx, event, "PROCESSED", "")
	})
	switch {
	case errors.Is(err, errLeaseLost):
		slog.Warn("webhook lease lost; another worker owns the event", "event_id", event.EventID)
		return
	case err != nil:
		// Transient (database) failure: nothing committed. Retry after a backoff.
		slog.Error("apply webhook event", "event_id", event.EventID, "error", err)
		p.reschedule(ctx, event, "apply failed: "+err.Error())
		return
	}

	switch result.outcome {
	case outcomeProcessed:
		slog.Info("webhook event applied", "event_id", event.EventID, "event", event.EventType, "note", result.reason)
	case outcomePark:
		p.parkOrRetry(ctx, event, result, true)
	case outcomeRetry:
		p.parkOrRetry(ctx, event, result, false)
	}
}

type paymentEntity struct {
	ID        string  `json:"id"`
	OrderID   string  `json:"order_id"`
	Amount    int64   `json:"amount"`
	Currency  string  `json:"currency"`
	Status    string  `json:"status"`
	Captured  bool    `json:"captured"`
	ErrorCode *string `json:"error_code"`
}

type webhookEnvelope struct {
	Event     string `json:"event"`
	CreatedAt int64  `json:"created_at"`
	Payload   struct {
		Refund *struct {
			Entity struct {
				ID        string          `json:"id"`
				PaymentID string          `json:"payment_id"`
				Status    string          `json:"status"`
				Amount    int64           `json:"amount"`
				Receipt   *string         `json:"receipt"`
				Notes     json.RawMessage `json:"notes"`
			} `json:"entity"`
		} `json:"refund"`
		Payment *struct {
			Entity paymentEntity `json:"entity"`
		} `json:"payment"`
		Order *struct {
			Entity struct {
				ID     string `json:"id"`
				Amount int64  `json:"amount"`
			} `json:"entity"`
		} `json:"order"`
	} `json:"payload"`
}

// apply decodes an event and applies its effect to the payment or refund; a malformed event is parked.
func (p *WebhookProcessor) apply(tx *gorm.DB, event models.WebhookEvent) (webhookResult, error) {
	var envelope webhookEnvelope
	if err := json.Unmarshal(event.Body, &envelope); err != nil {
		return webhookResult{outcome: outcomePark, reason: "malformed payload: " + err.Error(), parkReason: "MALFORMED"}, nil
	}
	if strings.HasPrefix(event.EventType, "refund.") {
		return p.applyRefundEvent(tx, event, envelope)
	}
	if envelope.Payload.Payment == nil || envelope.Payload.Payment.Entity.ID == "" ||
		envelope.Payload.Payment.Entity.OrderID == "" {
		return webhookResult{outcome: outcomePark, reason: "payload has no payment entity with an order id", parkReason: "MALFORMED"}, nil
	}
	payment := envelope.Payload.Payment.Entity

	// Lock the attempt: a concurrent poll or webhook for the same payment waits
	// here, then sees the committed state and decides on current data.
	var attempt models.PaymentAttempt
	lookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("gateway_order_id = ?", payment.OrderID).
		Take(&attempt)
	if errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
		// The event can arrive before our row learns the gateway order id (the
		// create call's result is committed afterwards, or was lost and polling
		// has not found it yet). Never discard it as unknown.
		return webhookResult{
			outcome: outcomeRetry, reason: "no payment for gateway order " + payment.OrderID,
			parkReason: parkNoMapping, gatewayOrderID: payment.OrderID,
		}, nil
	}
	if lookup.Error != nil {
		return webhookResult{}, fmt.Errorf("load payment for webhook: %w", lookup.Error)
	}

	if attempt.Gateway != p.config.GatewayName {
		// Signed for this stack, but about an attempt that belongs to another gateway
		// environment (for example simulator money while running against the real
		// API): never apply it here.
		return webhookResult{
			outcome: outcomePark, parkReason: "ENVIRONMENT_MISMATCH",
			reason: fmt.Sprintf("attempt belongs to gateway %q, this process serves %q", attempt.Gateway, p.config.GatewayName),
		}, nil
	}

	switch event.EventType {
	case "payment.authorized", "payment.failed":
		// Neither changes the attempt: an authorized payment is still awaiting
		// capture (and an older authorized event must never regress a captured
		// payment), and a failed attempt does not fail the checkout, because the
		// customer may retry on the same gateway order until the window ends.
		return webhookResult{outcome: outcomeProcessed, reason: "no state change for " + event.EventType}, nil
	case "payment.captured", "order.paid":
		if payment.Status != "captured" && !payment.Captured {
			return webhookResult{outcome: outcomeProcessed, reason: "event carries no capture"}, nil
		}
	default:
		return webhookResult{outcome: outcomePark, reason: "unsupported event type " + event.EventType, parkReason: "UNSUPPORTED"}, nil
	}

	// The same payment already settled or recorded: nothing to do.
	if attempt.GatewayPaymentID != nil && *attempt.GatewayPaymentID == payment.ID && attempt.Status == "SUCCEEDED" {
		return webhookResult{outcome: outcomeProcessed, reason: "capture already applied"}, nil
	}

	// The capture time is OUR receipt time for this event, not anything the
	// payload claims: provider timestamps are not capture-time evidence (see
	// decideReconcile). The receipt time is durable, so a delayed or replayed
	// processing does not move it.
	state := paymentgateway.OrderState{
		GatewayOrderID:      payment.OrderID,
		CapturedPaymentID:   payment.ID,
		CapturedAmountPaise: payment.Amount,
		CapturedCurrency:    payment.Currency,
		ObservedAt:          event.ReceivedAt,
		Source:              "webhook",
	}
	if envelope.Payload.Order != nil {
		state.AmountPaise = envelope.Payload.Order.Entity.Amount
	}
	decision := decideReconcile(attempt, state, true, p.now(), p.config.PaymentWindow)
	changed, err := applyDecision(tx, attempt, decision)
	if err != nil {
		return webhookResult{}, err
	}
	note := "no change"
	if changed {
		note = decision.reason
	}
	if envelope.CreatedAt > 0 {
		// Recorded for audit only; it never decides anything.
		note += fmt.Sprintf(" (provider event created_at=%d, received %s)", envelope.CreatedAt, event.ReceivedAt.UTC().Format(time.RFC3339))
	}
	return webhookResult{outcome: outcomeProcessed, reason: note}, nil
}

// applyRefundEvent applies refund.created / refund.processed / refund.failed to the
// compensation record of the payment. Webhooks only accelerate this: the compensation
// worker reconciles every refund by fetching it, so a missing event is not fatal.
func (p *WebhookProcessor) applyRefundEvent(tx *gorm.DB, event models.WebhookEvent, envelope webhookEnvelope) (webhookResult, error) {
	if p.compensation == nil {
		return webhookResult{outcome: outcomePark, parkReason: "NO_COMPENSATION_WORKER", reason: "this gateway has no refund worker"}, nil
	}
	if envelope.Payload.Refund == nil || envelope.Payload.Refund.Entity.ID == "" || envelope.Payload.Refund.Entity.PaymentID == "" {
		return webhookResult{outcome: outcomePark, parkReason: "MALFORMED", reason: "refund event has no refund entity with a payment id"}, nil
	}
	entity := envelope.Payload.Refund.Entity
	refund := paymentgateway.RefundResult{ID: entity.ID, Status: entity.Status, PaymentID: entity.PaymentID, AmountPaise: entity.Amount}
	if entity.Receipt != nil {
		refund.Receipt = *entity.Receipt
	}
	var notes map[string]any
	if json.Unmarshal(entity.Notes, &notes) == nil && len(notes) > 0 {
		refund.Notes = make(map[string]string, len(notes))
		for key, value := range notes {
			refund.Notes[key] = fmt.Sprint(value)
		}
	}
	note, err := p.compensation.ApplyProviderRefund(tx, refund)
	if err != nil {
		return webhookResult{}, err
	}
	if strings.HasPrefix(note, "no compensation record") {
		// The record may simply not exist yet; retry with the usual bounded backoff.
		return webhookResult{outcome: outcomeRetry, parkReason: "NO_RECORD", reason: note}, nil
	}
	return webhookResult{outcome: outcomeProcessed, reason: note}, nil
}

// finish moves the event out of PENDING. The update is guarded by the lease, so a
// worker whose lease expired and was taken over cannot commit stale effects.
func (p *WebhookProcessor) finish(tx *gorm.DB, event models.WebhookEvent, status, lastError string, extra ...map[string]any) error {
	updates := map[string]any{
		"status": status, "locked_by": nil, "locked_until": nil,
		"processed_at": p.now(),
	}
	for _, more := range extra {
		for key, value := range more {
			updates[key] = value
		}
	}
	if lastError != "" {
		updates["last_error"] = lastError
	} else {
		updates["last_error"] = nil
	}
	result := tx.Table("payments.webhook_events").
		Where("id = ? AND locked_by = ? AND status = 'PENDING'", event.ID, p.workerID).
		Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("mark webhook event %s: %w", status, result.Error)
	}
	if result.RowsAffected != 1 {
		return errLeaseLost
	}
	return nil
}

// parkOrRetry keeps an event the processor could not apply. Retry-able events get
// bounded exponential backoff; after MaxAttempts (or when forcePark) the event is
// PARKED: durable, visible, never deleted. An event parked for want of a mapping
// (NO_MAPPING) is replayed automatically when the mapping appears (ReplayParked).
func (p *WebhookProcessor) parkOrRetry(ctx context.Context, event models.WebhookEvent, result webhookResult, forcePark bool) {
	if forcePark || event.Attempts >= p.config.MaxAttempts {
		slog.Error("webhook event parked", "event_id", event.EventID, "event", event.EventType,
			"attempts", event.Attempts, "park_reason", result.parkReason, "reason", result.reason)
		extra := map[string]any{"park_reason": result.parkReason}
		if result.gatewayOrderID != "" {
			extra["gateway_order_id"] = result.gatewayOrderID
		}
		err := p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return p.finish(tx, event, "PARKED", result.reason, extra)
		})
		if err != nil {
			slog.Error("park webhook event", "event_id", event.EventID, "error", err)
		}
		return
	}
	p.reschedule(ctx, event, result.reason)
}

// reschedule retries an event later, with a delay that doubles up to a cap.
func (p *WebhookProcessor) reschedule(ctx context.Context, event models.WebhookEvent, reason string) {
	delay := p.config.BackoffBase
	for i := 1; i < event.Attempts && delay < p.config.BackoffCap; i++ {
		delay *= 2
	}
	if delay > p.config.BackoffCap {
		delay = p.config.BackoffCap
	}
	err := p.db.WithContext(ctx).Table("payments.webhook_events").
		Where("id = ? AND locked_by = ? AND status = 'PENDING'", event.ID, p.workerID).
		Updates(map[string]any{
			"next_attempt_at": p.now().Add(delay),
			"locked_by":       nil,
			"locked_until":    nil,
			"last_error":      reason,
		}).Error
	if err != nil {
		slog.Error("reschedule webhook event", "event_id", event.EventID, "error", err)
	}
}
