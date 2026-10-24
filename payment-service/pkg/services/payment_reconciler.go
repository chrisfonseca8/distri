package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/payment-service/database/models"
	"github.com/chrisfonseca08/distri/payment-service/pkg/payment_gateway"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// failure codes that mean "we gave up waiting", as opposed to a decline. A
// capture that shows up later on such an attempt is a late success.
var giveUpFailureCodes = []string{"PAYMENT_EXPIRED", "NO_GATEWAY_ORDER", "AMOUNT_MISMATCH", "UNCONFIRMED"}

type ReconcilerConfig struct {
	Interval time.Duration
	// MinPoll is the minimum gap between two lookups of an unsettled attempt
	// (AWAITING_PAYMENT or UNKNOWN).
	MinPoll       time.Duration
	Batch         int
	PaymentWindow time.Duration
	// AuditWindow and AuditInterval bound the search for captured money on SETTLED
	// attempts (SUCCEEDED or FAILED): an attempt is looked up again every
	// AuditInterval for AuditWindow after it last changed, then never again. Outside
	// the window a late or additional capture is found only if its webhook arrives.
	AuditWindow    time.Duration
	AuditInterval  time.Duration
	GatewayTimeout time.Duration
	// UnconfirmedGrace: an attempt still AWAITING_PAYMENT or UNKNOWN this long after its
	// window ended, because every lookup failed, is decided FAILED (UNCONFIRMED) and the
	// saga is told. The payment service alone decides the payment, so the saga never has
	// to give up on it. A capture found afterwards is refunded like any late capture.
	// Zero disables the rule.
	UnconfirmedGrace time.Duration
}

// PaymentReconciler polls the gateway for payments that finish outside the
// request that created them: it completes AWAITING_PAYMENT and UNKNOWN attempts,
// fails them when the payment window passes, and refunds a payment that was
// captured after its attempt had already failed. Gateway calls never run inside
// a database transaction.
type PaymentReconciler struct {
	db          *gorm.DB
	lookup      paymentgateway.OrderLookup
	gatewayName string
	config      ReconcilerConfig
	now         func() time.Time
}

// NewPaymentReconciler creates the reconciler for one gateway.
func NewPaymentReconciler(
	db *gorm.DB,
	gateway paymentgateway.Gateway,
	gatewayName string,
	config ReconcilerConfig,
) (*PaymentReconciler, error) {
	if db == nil {
		return nil, errors.New("payment reconciler database is required")
	}
	lookup, ok := gateway.(paymentgateway.OrderLookup)
	if !ok {
		return nil, fmt.Errorf("gateway %q cannot look orders up", gatewayName)
	}
	if config.Interval <= 0 || config.Batch <= 0 || config.PaymentWindow <= 0 ||
		config.GatewayTimeout <= 0 || config.MinPoll < 0 || config.AuditWindow <= 0 || config.AuditInterval <= 0 {
		return nil, errors.New("invalid payment reconciler configuration")
	}
	return &PaymentReconciler{
		db:          db,
		lookup:      lookup,
		gatewayName: gatewayName,
		config:      config,
		now:         time.Now,
	}, nil
}

// Run reconciles payments on every interval until ctx is cancelled.
func (r *PaymentReconciler) Run(ctx context.Context) {
	slog.Info(
		"payment reconciler started",
		"gateway", r.gatewayName,
		"interval", r.config.Interval,
		"payment_window", r.config.PaymentWindow,
	)
	defer slog.Info("payment reconciler stopped")

	ticker := time.NewTicker(r.config.Interval)
	defer ticker.Stop()
	for {
		if err := r.ReconcileOnce(ctx); err != nil && ctx.Err() == nil {
			slog.Error("payment reconciler pass failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ReconcileOnce handles one batch of attempts that are due for a lookup.
func (r *PaymentReconciler) ReconcileOnce(ctx context.Context) error {
	now := r.now()
	var attempts []models.PaymentAttempt
	// Only attempts of THIS gateway environment are touched: an attempt labelled for
	// another environment (for example simulator work while this process talks to the
	// real API) is invisible to this worker.
	query := r.db.WithContext(ctx).
		Where("gateway = ?", r.gatewayName).
		Where(`(status IN ('AWAITING_PAYMENT', 'UNKNOWN') AND (reconciled_at IS NULL OR reconciled_at < ?))
			OR (status IN ('SUCCEEDED', 'FAILED') AND updated_at > ? AND (reconciled_at IS NULL OR reconciled_at < ?))`,
			now.Add(-r.config.MinPoll), now.Add(-r.config.AuditWindow), now.Add(-r.config.AuditInterval)).
		Order("reconciled_at NULLS FIRST, created_at").
		Limit(r.config.Batch).
		Find(&attempts)
	if query.Error != nil {
		return fmt.Errorf("load payments to reconcile: %w", query.Error)
	}
	for _, attempt := range attempts {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r.reconcileOne(ctx, attempt)
	}
	return nil
}

// reconcileOne looks up one payment's order at the gateway and applies the result; if the lookup fails it changes nothing.
func (r *PaymentReconciler) reconcileOne(ctx context.Context, attempt models.PaymentAttempt) {
	lookupContext, cancel := context.WithTimeout(ctx, r.config.GatewayTimeout)
	state, err := r.lookup.LookupOrder(lookupContext, attempt.OrderID)
	cancel()
	found := err == nil
	if err != nil && !errors.Is(err, paymentgateway.ErrOrderNotFound) {
		// The lookup itself failed: the outcome is still unknown, so change
		// nothing and ask again on a later pass.
		slog.Warn("payment lookup failed", "payment_id", attempt.ID, "order_id", attempt.OrderID, "error", err)
		r.failUnconfirmed(ctx, attempt)
		return
	}

	// The lookup has just returned: this is the earliest moment we can say the
	// capture existed, and the only capture time the policy trusts.
	observed := r.now()
	state.ObservedAt, state.Source = observed, "poll"
	decision := decideReconcile(attempt, state, found, observed, r.config.PaymentWindow)
	if decision.expiry && r.captureWaitingInInbox(ctx, attempt) {
		// An on-time capture is durably received but not yet applied. Let the
		// webhook worker decide it (it will see the receipt time) instead of
		// expiring the attempt underneath it.
		slog.Info("expiry deferred: an on-time capture event is waiting in the inbox", "payment_id", attempt.ID)
		r.touch(ctx, attempt.ID)
		return
	}
	changed, err := r.apply(ctx, attempt, decision)
	if err != nil {
		slog.Error("apply payment reconciliation", "payment_id", attempt.ID, "error", err)
		return
	}
	if changed {
		slog.Warn(
			"reconciled payment",
			"payment_id", attempt.ID,
			"order_id", attempt.OrderID,
			"from_status", attempt.Status,
			"reason", decision.reason,
		)
	}
}

// failUnconfirmed decides an attempt FAILED when the gateway could not be asked for so
// long after the payment window that waiting further helps nobody. Only the absence of
// evidence is read as failure: a capture seen later is refunded, never accepted, because
// the checkout is cancelled by then.
func (r *PaymentReconciler) failUnconfirmed(ctx context.Context, attempt models.PaymentAttempt) {
	if r.config.UnconfirmedGrace <= 0 {
		return
	}
	if attempt.Status != "AWAITING_PAYMENT" && attempt.Status != "UNKNOWN" {
		return
	}
	now := r.now()
	if now.Before(windowEnd(attempt, r.config.PaymentWindow).Add(r.config.UnconfirmedGrace)) {
		return
	}
	if r.captureWaitingInInbox(ctx, attempt) {
		return
	}
	decision := reconcileDecision{
		updates: map[string]any{"status": "FAILED", "failure_code": "UNCONFIRMED"},
		expiry:  true,
		reason:  "no answer from the gateway until the grace period after the payment window ended",
	}
	changed, err := r.apply(ctx, attempt, decision)
	if err != nil {
		slog.Error("apply unconfirmed payment decision", "payment_id", attempt.ID, "error", err)
		return
	}
	if changed {
		slog.Warn("payment failed as unconfirmed", "payment_id", attempt.ID, "order_id", attempt.OrderID,
			"from_status", attempt.Status, "grace", r.config.UnconfirmedGrace)
	}
}

// captureWaitingInInbox reports whether a capture event for this attempt's gateway
// order was received before the deadline and is still waiting to be applied. A
// parked or processed event no longer waits, so this cannot defer expiry forever.
func (r *PaymentReconciler) captureWaitingInInbox(ctx context.Context, attempt models.PaymentAttempt) bool {
	if attempt.GatewayOrderID == nil {
		return false
	}
	var count int64
	err := r.db.WithContext(ctx).Table("payments.webhook_events").
		Where("gateway_order_id = ? AND status = 'PENDING' AND event_type IN ('payment.captured','order.paid') AND received_at < ?",
			*attempt.GatewayOrderID, windowEnd(attempt, r.config.PaymentWindow)).
		Count(&count).Error
	if err != nil {
		slog.Warn("inbox check before expiry failed", "payment_id", attempt.ID, "error", err)
		return false
	}
	return count > 0
}

type reconcileDecision struct {
	// updates is nil when the attempt itself does not change.
	updates map[string]any
	// expiry marks a decision that fails an attempt only because its window ended,
	// with no capture seen. It may be deferred when an on-time capture is waiting
	// unprocessed in the webhook inbox.
	expiry bool
	// compensations are captured payments that did not pay for a completed
	// checkout. They are recorded durably, one row per gateway payment id.
	compensations []compensationDraft
	// capture is the evidence behind a decision that settles on a capture.
	capture *captureEvidence
	reason  string
}

type compensationDraft struct {
	PaymentID    string
	AmountPaise  int64
	Currency     string
	Reason       string
	Status       string // REQUIRED when the payment's identity and amount are established, else REVIEW
	ReviewReason string
}

type captureEvidence struct {
	ObservedAt time.Time
	Source     string
}

const (
	reasonLateCapture    = "LATE_CAPTURE"
	reasonAmountMismatch = "AMOUNT_MISMATCH"
	reasonAfterCancel    = "CAPTURE_AFTER_CANCELLATION"
	reasonExtraCapture   = "EXTRA_CAPTURE"
	// reasonCheckoutFailedAfterPayment is recorded only on the saga's request: the customer
	// paid, but the checkout could not be completed within the bounded wait.
	reasonCheckoutFailedAfterPayment = "CHECKOUT_FAILED_AFTER_PAYMENT"
)

// decideReconcile maps what the gateway reports onto the attempt's state
// machine. It is pure: no I/O, so every case is unit-testable. Polling, the
// expiry pass and the webhook worker all call it, so there is one set of rules.
//
// The deadline is fixed: expires_at (or created_at + window). The ONLY timestamp
// that decides whether a capture met it is OrderState.ObservedAt, when we first
// saw the capture on our own clock. A capture seen before the deadline was
// necessarily made before it, whatever the provider's clocks say. Provider
// timestamps are not used: Razorpay does not define an event's created_at as the
// capture time (its sample authorized and captured events share one value), and
// a payment has only a creation time, no captured_at. Missing evidence is never
// read as "on time": a capture seen after the deadline is treated as late even if
// it may have happened earlier, so the cancelled checkout stays cancelled and the
// money is compensated (a customer who paid just in time may be refunded
// unnecessarily). A cancelled or expired attempt is never resurrected.
func decideReconcile(
	attempt models.PaymentAttempt,
	state paymentgateway.OrderState,
	found bool,
	now time.Time,
	window time.Duration,
) reconcileDecision {
	captured := found && state.CapturedPaymentID != ""
	observedAt := state.ObservedAt
	if observedAt.IsZero() {
		observedAt = now
	}
	evidence := &captureEvidence{ObservedAt: observedAt, Source: state.Source}

	// Every captured payment we know of. The first is the primary one (the capture
	// on the oldest gateway order); the rest are additional captures.
	var captures []paymentgateway.CapturedPayment
	if captured {
		captures = append(captures, paymentgateway.CapturedPayment{
			PaymentID: state.CapturedPaymentID, AmountPaise: state.CapturedAmountPaise, Currency: state.CapturedCurrency,
		})
		captures = append(captures, state.ExtraCaptured...)
	}
	// draft turns a captured payment into a compensation to record. Only a payment
	// whose identity, amount and currency are established is eligible for an
	// automatic refund; anything else stays in review with an actionable reason.
	draft := func(c paymentgateway.CapturedPayment, reason string) compensationDraft {
		d := compensationDraft{
			PaymentID: c.PaymentID, AmountPaise: c.AmountPaise, Currency: c.Currency,
			Reason: reason, Status: models.CompensationRequired,
		}
		switch {
		case c.PaymentID == "":
			d.Status, d.ReviewReason = models.CompensationReview, "captured payment has no gateway payment id"
		case c.AmountPaise <= 0:
			d.Status, d.ReviewReason = models.CompensationReview, "captured amount is not established"
		case c.Currency == "":
			d.Status, d.ReviewReason = models.CompensationReview, "captured currency is not established"
		}
		return d
	}
	// compensate lists a draft for every capture except the one that funded
	// fulfilment (funding may be empty). The funding payment is never refunded.
	compensate := func(funding string, primaryReason string) []compensationDraft {
		var drafts []compensationDraft
		for i, c := range captures {
			if c.PaymentID == funding && funding != "" {
				continue
			}
			reason := reasonExtraCapture
			if i == 0 {
				reason = primaryReason
			}
			drafts = append(drafts, draft(c, reason))
		}
		return drafts
	}

	switch attempt.Status {
	case "AWAITING_PAYMENT", "UNKNOWN":
		deadline := windowEnd(attempt, window)
		switch {
		case captured && (state.CapturedAmountPaise != attempt.AmountPaise ||
			(state.CapturedCurrency != "" && state.CapturedCurrency != attempt.Currency)):
			return reconcileDecision{
				updates: map[string]any{
					"status":             "FAILED",
					"failure_code":       "AMOUNT_MISMATCH",
					"gateway_order_id":   state.GatewayOrderID,
					"gateway_payment_id": state.CapturedPaymentID,
				},
				compensations: compensate("", reasonAmountMismatch),
				capture:       evidence,
				reason:        "captured amount or currency differs from the attempt",
			}
		case captured && observedAt.Before(deadline):
			return reconcileDecision{
				updates: map[string]any{
					"status":             "SUCCEEDED",
					"failure_code":       nil,
					"gateway_order_id":   state.GatewayOrderID,
					"gateway_payment_id": state.CapturedPaymentID,
				},
				compensations: compensate(state.CapturedPaymentID, reasonExtraCapture),
				capture:       evidence,
				reason:        "payment captured before the deadline",
			}
		case captured:
			return reconcileDecision{
				updates: map[string]any{
					"status":             "FAILED",
					"failure_code":       "PAYMENT_EXPIRED",
					"gateway_order_id":   state.GatewayOrderID,
					"gateway_payment_id": state.CapturedPaymentID,
				},
				compensations: compensate("", reasonLateCapture),
				capture:       evidence,
				reason:        "payment first seen captured after the business deadline",
			}
		case !found:
			if now.Before(deadline) {
				return reconcileDecision{}
			}
			return reconcileDecision{
				updates: map[string]any{"status": "FAILED", "failure_code": "NO_GATEWAY_ORDER"},
				expiry:  true,
				reason:  "no gateway order after the payment window",
			}
		case attempt.Status == "UNKNOWN":
			return reconcileDecision{
				updates: map[string]any{
					"status":           "AWAITING_PAYMENT",
					"gateway_order_id": state.GatewayOrderID,
					"expires_at":       deadline,
				},
				reason: "gateway order exists, awaiting payment",
			}
		case !now.Before(deadline):
			return reconcileDecision{
				updates: map[string]any{"status": "FAILED", "failure_code": "PAYMENT_EXPIRED"},
				expiry:  true,
				reason:  "payment window passed",
			}
		default:
			return reconcileDecision{}
		}

	case "FAILED":
		// Captured money on a cancelled or expired checkout: every captured payment
		// is owed back and recorded, each under its own payment id. The checkout is
		// never revived and the saga is not told again.
		if drafts := compensate("", reasonAfterCancel); len(drafts) > 0 {
			return reconcileDecision{compensations: drafts, capture: evidence, reason: "payment captured after the attempt failed"}
		}

	case "SUCCEEDED":
		// Anything captured besides the payment that settled this attempt is extra
		// money, never a second success and never the funding payment.
		funding := ""
		if attempt.GatewayPaymentID != nil {
			funding = *attempt.GatewayPaymentID
		}
		if drafts := compensate(funding, reasonExtraCapture); len(drafts) > 0 {
			return reconcileDecision{compensations: drafts, capture: evidence, reason: "additional capture on a settled attempt"}
		}
	}
	return reconcileDecision{}
}

// windowEnd returns when the payment window for an attempt ends.
func windowEnd(attempt models.PaymentAttempt, window time.Duration) time.Time {
	if attempt.ExpiresAt != nil {
		return *attempt.ExpiresAt
	}
	return attempt.CreatedAt.Add(window)
}

// apply writes a decision in one short transaction and stamps reconciled_at. It
// reports whether the attempt's state changed.
func (r *PaymentReconciler) apply(
	ctx context.Context,
	attempt models.PaymentAttempt,
	decision reconcileDecision,
) (bool, error) {
	changed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		changed, err = applyDecision(tx, attempt, decision)
		if err != nil {
			return err
		}
		return tx.Table("payments.payment_attempts").
			Where("id = ?", attempt.ID).
			UpdateColumn("reconciled_at", r.now()).Error
	})
	return changed, err
}

// applyDecision commits one decision inside the caller's transaction. It is the
// single place polling, the expiry pass, the audit pass and the webhook worker
// change an attempt: every update is guarded by the status the decision was made
// from, so a decision made on stale data, or the same discovery made twice, changes
// nothing and sends no second reply to the saga. Compensation records are inserted
// idempotently (unique per gateway environment and payment id) in the same
// transaction as the change that created the obligation. It reports whether
// anything new was committed.
func applyDecision(tx *gorm.DB, attempt models.PaymentAttempt, decision reconcileDecision) (bool, error) {
	changed := false
	if decision.updates != nil {
		updates := decision.updates
		if decision.capture != nil {
			updates = copyUpdates(updates)
			updates["capture_observed_at"] = decision.capture.ObservedAt
			updates["capture_source"] = decision.capture.Source
		}
		var err error
		changed, err = writePaymentResult(tx, attempt.ID, attempt.Status, updates)
		if err != nil {
			return false, err
		}
		if !changed {
			// Someone else moved the attempt first; they own the outcome and the
			// next pass or event recomputes from the new state.
			return false, nil
		}
	}
	for _, draft := range decision.compensations {
		inserted, err := recordCompensation(tx, attempt, draft, decision.capture)
		if err != nil {
			return false, err
		}
		changed = changed || inserted
	}
	return changed, nil
}

// copyUpdates returns a copy of the update map so it can be extended safely.
func copyUpdates(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+2)
	for key, value := range in {
		out[key] = value
	}
	return out
}

// recordCompensation durably records one captured payment that needs a refund or a
// human decision, and its first history entry. It is idempotent per (gateway
// environment, gateway payment id) and reports whether it inserted a new record.
// The payment that funded a successful checkout is never recorded.
func recordCompensation(
	tx *gorm.DB,
	attempt models.PaymentAttempt,
	draft compensationDraft,
	capture *captureEvidence,
) (bool, error) {
	if draft.PaymentID == "" {
		return false, errors.New("compensation record needs a gateway payment id")
	}
	if attempt.Status == "SUCCEEDED" && attempt.GatewayPaymentID != nil && *attempt.GatewayPaymentID == draft.PaymentID {
		return false, nil
	}
	observedAt, source := time.Now(), "unknown"
	if capture != nil {
		observedAt, source = capture.ObservedAt, capture.Source
	}
	record := models.CompensationRecord{
		PaymentAttemptID: attempt.ID, OrderID: attempt.OrderID, Gateway: attempt.Gateway,
		GatewayPaymentID: draft.PaymentID, AmountPaise: draft.AmountPaise, Currency: draft.Currency,
		Reason: draft.Reason, Status: draft.Status, ObservedAt: observedAt, Source: source,
	}
	if draft.ReviewReason != "" {
		reason := draft.ReviewReason
		record.ReviewReason = &reason
	}
	result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&record)
	if result.Error != nil {
		return false, fmt.Errorf("record compensation for %s: %w", draft.PaymentID, result.Error)
	}
	if result.RowsAffected == 0 {
		return false, nil
	}
	return true, addCompensationEvent(tx, record.ID, "", draft.Status,
		fmt.Sprintf("recorded: %s, %d %s, observed by %s", draft.Reason, draft.AmountPaise, draft.Currency, source))
}

// addCompensationEvent appends a history entry. History is never rewritten.
func addCompensationEvent(tx *gorm.DB, recordID uuid.UUID, from, to, detail string) error {
	event := models.CompensationEvent{RecordID: recordID, FromStatus: from, ToStatus: to, Detail: detail}
	if err := tx.Create(&event).Error; err != nil {
		return fmt.Errorf("record compensation history: %w", err)
	}
	return nil
}

// touch records the time a payment was last reconciled.
func (r *PaymentReconciler) touch(ctx context.Context, id uuid.UUID) {
	if err := r.db.WithContext(ctx).
		Table("payments.payment_attempts").
		Where("id = ?", id).
		UpdateColumn("reconciled_at", r.now()).Error; err != nil {
		slog.Error("stamp reconciled_at", "payment_id", id, "error", err)
	}
}
