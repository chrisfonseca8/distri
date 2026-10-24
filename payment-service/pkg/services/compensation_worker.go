package services

import (
	"context"
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
)

// CompensationConfig tunes the refund worker. Every duration is a policy choice, not a
// provider guarantee: Razorpay documents none of them.
type CompensationConfig struct {
	Interval       time.Duration
	Batch          int
	Lease          time.Duration // how long a claimed submission may run before it is treated as lost
	GatewayTimeout time.Duration
	PendingPoll    time.Duration // gap between provider-status fetches of a PENDING refund
	// AbsenceWindow: an empty lookup earlier than this after an unknown submission
	// proves nothing (lookups may lag), so the worker only waits.
	AbsenceWindow time.Duration
	// IdempotentRetryWindow and MaxIdempotentRetries bound resubmission of an unknown
	// operation with the SAME idempotency key and body (documented as safe to retry);
	// key retention is undocumented, so this is limited to a short window and the
	// provider's own "already fully refunded" refusal remains the backstop.
	IdempotentRetryWindow time.Duration
	MaxIdempotentRetries  int
	// UnknownMaxAge: an operation still unresolved after this long goes to review.
	UnknownMaxAge time.Duration
	// MaxFailedRetries and RetryDelay: a definitive rejection (nothing was created) is
	// retried with a new operation this many times, then goes to review. A refund the
	// provider created and then failed goes to review immediately.
	MaxFailedRetries int
	RetryDelay       time.Duration
}

func (c CompensationConfig) validate() error {
	switch {
	case c.Interval <= 0 || c.Batch <= 0 || c.Lease <= 0 || c.GatewayTimeout <= 0 || c.PendingPoll <= 0 || c.RetryDelay <= 0:
		return errors.New("compensation intervals, batch, lease and timeouts must be positive")
	case c.Lease <= c.GatewayTimeout:
		return errors.New("compensation lease must be longer than the gateway timeout")
	case c.AbsenceWindow < 0 || c.IdempotentRetryWindow < c.AbsenceWindow || c.UnknownMaxAge < c.IdempotentRetryWindow:
		return errors.New("compensation windows must satisfy absence <= idempotent retry <= unknown max age")
	case c.MaxIdempotentRetries < 0 || c.MaxFailedRetries < 0:
		return errors.New("compensation retry counts must not be negative")
	}
	return nil
}

// CompensationWorker submits and reconciles refunds for captured money that did not
// pay for a completed checkout. It is a state machine over compensation_records:
//
//	REQUIRED -> SUBMITTING -> PENDING -> PROCESSED
//	                 |            \-> REVIEW (provider failed / refund id unknown)
//	                 +-> UNKNOWN -> (adopt by lookup) PENDING | PROCESSED
//	                 |        \-> (same-key retry, bounded) SUBMITTING
//	                 |        \-> REVIEW (nothing established)
//	                 \-> FAILED (rejected, nothing created) -> REQUIRED (bounded) | REVIEW
//
// Rules: claims are short transactions with a lease and never span an HTTP call; the
// operation id is stored before the request is sent; a timeout, reset or ambiguous
// 5xx is UNKNOWN and is never read as "no refund exists"; local deduplication is not
// provider exactly-once, so every unknown operation is reconciled against the
// provider's own data (refund list by receipt, and payment.amount_refunded).
type CompensationWorker struct {
	db          *gorm.DB
	gatewayName string
	refunder    paymentgateway.Refunder
	lookup      paymentgateway.RefundLookup
	payments    paymentgateway.PaymentFetcher
	config      CompensationConfig
	workerID    string
	now         func() time.Time
}

func NewCompensationWorker(
	db *gorm.DB,
	gateway paymentgateway.Gateway,
	gatewayName string,
	config CompensationConfig,
) (*CompensationWorker, error) {
	if db == nil {
		return nil, errors.New("compensation worker database is required")
	}
	refunder, ok1 := gateway.(paymentgateway.Refunder)
	lookup, ok2 := gateway.(paymentgateway.RefundLookup)
	payments, ok3 := gateway.(paymentgateway.PaymentFetcher)
	if !ok1 || !ok2 || !ok3 {
		return nil, fmt.Errorf("gateway %q cannot refund, look refunds up and fetch payments", gatewayName)
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	host, _ := os.Hostname()
	return &CompensationWorker{
		db: db, gatewayName: gatewayName, refunder: refunder, lookup: lookup, payments: payments,
		config: config, now: time.Now,
		workerID: fmt.Sprintf("%s-%d-%s", host, os.Getpid(), uuid.New().String()[:8]),
	}, nil
}

func (w *CompensationWorker) Run(ctx context.Context) {
	slog.Info("compensation worker started", "gateway", w.gatewayName, "interval", w.config.Interval)
	defer slog.Info("compensation worker stopped")
	ticker := time.NewTicker(w.config.Interval)
	defer ticker.Stop()
	for {
		w.RunOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunOnce makes one pass over every phase, in a fixed order: recovery of work a
// stopped worker left behind comes first, so what it releases is handled in the same
// pass. A failing phase is logged and the rest still run.
func (w *CompensationWorker) RunOnce(ctx context.Context) {
	phases := []struct {
		name string
		run  func(context.Context) error
	}{
		{"recover stale submissions", w.recoverStale},
		{"classify review records", w.classifyReview},
		{"recover failed", w.recoverFailed},
		{"submit", w.submitRequired},
		{"reconcile unknown", w.reconcileUnknown},
		{"reconcile pending", w.reconcilePending},
	}
	for _, phase := range phases {
		if err := phase.run(ctx); err != nil && ctx.Err() == nil {
			slog.Error("compensation phase failed", "phase", phase.name, "error", err)
		}
	}
}

// ---- state transitions -------------------------------------------------------

type recordGuard struct {
	status      string
	operationID *uuid.UUID
	lockedBy    string
}

// transition applies updates to one record and appends a history entry, in one
// short transaction, only if the record still has the status (and, when given, the
// operation id and lease owner) the caller decided from. It reports whether it
// applied; false means someone else moved the record and the caller must stop.
func (w *CompensationWorker) transition(
	ctx context.Context,
	record models.CompensationRecord,
	guard recordGuard,
	to string,
	updates map[string]any,
	detail string,
) (bool, error) {
	applied := false
	err := w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Table("payments.compensation_records").Where("id = ? AND status = ?", record.ID, guard.status)
		if guard.operationID != nil {
			query = query.Where("operation_id = ?", *guard.operationID)
		}
		if guard.lockedBy != "" {
			query = query.Where("locked_by = ?", guard.lockedBy)
		}
		set := map[string]any{"status": to, "updated_at": w.now()}
		for key, value := range updates {
			set[key] = value
		}
		result := query.Updates(set)
		if result.Error != nil {
			return fmt.Errorf("move compensation %s to %s: %w", record.ID, to, result.Error)
		}
		if result.RowsAffected != 1 {
			return nil
		}
		applied = true
		return addCompensationEvent(tx, record.ID, guard.status, to, detail)
	})
	return applied, err
}

func unlocked(updates map[string]any) map[string]any {
	updates["locked_by"], updates["locked_until"] = nil, nil
	return updates
}

func truncateText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit]
}

func (w *CompensationWorker) toReview(ctx context.Context, record models.CompensationRecord, guard recordGuard, reason string) {
	slog.Error("compensation needs review", "record_id", record.ID, "payment_id", record.GatewayPaymentID, "reason", reason)
	if _, err := w.transition(ctx, record, guard, models.CompensationReview,
		unlocked(map[string]any{"review_reason": reason}), "review: "+reason); err != nil {
		slog.Error("park compensation for review", "record_id", record.ID, "error", err)
	}
}

// ---- recovery of work a stopped worker left behind -------------------------------

// recoverStale handles SUBMITTING records whose lease expired (the worker stopped).
// If the request was never sent (submitted_at is empty) it is safe to start over;
// if it may have been sent the outcome is UNKNOWN and is reconciled, not repeated.
func (w *CompensationWorker) recoverStale(ctx context.Context) error {
	var stale []models.CompensationRecord
	if err := w.db.WithContext(ctx).
		Where("gateway = ? AND status = ? AND locked_until < ?", w.gatewayName, models.CompensationSubmitting, w.now()).
		Limit(w.config.Batch).Find(&stale).Error; err != nil {
		return err
	}
	for _, record := range stale {
		guard := recordGuard{status: models.CompensationSubmitting, operationID: record.OperationID}
		if record.SubmittedAt == nil {
			_, err := w.transition(ctx, record, guard, models.CompensationRequired,
				unlocked(map[string]any{"next_attempt_at": w.now(), "last_error": "worker stopped before sending the refund request"}),
				"worker stopped before the request was sent: safe to start again")
			if err != nil {
				return err
			}
			continue
		}
		_, err := w.transition(ctx, record, guard, models.CompensationUnknown,
			unlocked(map[string]any{"unknown_since": w.now(), "next_attempt_at": w.now(),
				"last_error": "worker stopped after sending the refund request"}),
			"worker stopped after sending the request: the gateway may have created the refund; reconcile, do not resend blindly")
		if err != nil {
			return err
		}
	}
	return nil
}

// classifyReview looks at REVIEW records that carry no reason (older records) and
// either makes them eligible, because payment identity, ownership, amount and
// reason are established, or records exactly what is missing. It never touches a
// record that already has a review reason: those wait for a person.
func (w *CompensationWorker) classifyReview(ctx context.Context) error {
	var records []models.CompensationRecord
	if err := w.db.WithContext(ctx).
		Where("gateway = ? AND status = ? AND review_reason IS NULL", w.gatewayName, models.CompensationReview).
		Limit(w.config.Batch).Find(&records).Error; err != nil {
		return err
	}
	for _, record := range records {
		problem := w.eligibilityProblem(ctx, record)
		guard := recordGuard{status: models.CompensationReview}
		if problem != "" {
			if _, err := w.transition(ctx, record, guard, models.CompensationReview,
				map[string]any{"review_reason": problem}, "review classified: "+problem); err != nil {
				return err
			}
			continue
		}
		if _, err := w.transition(ctx, record, guard, models.CompensationRequired,
			map[string]any{"next_attempt_at": w.now()}, "review classified: payment identity, ownership, amount and reason established; eligible for refund"); err != nil {
			return err
		}
	}
	return nil
}

// eligibilityProblem returns why a record must not be refunded automatically, or "".
func (w *CompensationWorker) eligibilityProblem(ctx context.Context, record models.CompensationRecord) string {
	var attempt models.PaymentAttempt
	if err := w.db.WithContext(ctx).Where("id = ?", record.PaymentAttemptID).Take(&attempt).Error; err != nil {
		return "owning payment attempt not found"
	}
	switch {
	case record.GatewayPaymentID == "":
		return "no gateway payment id"
	case record.AmountPaise <= 0:
		return "amount not established"
	case record.Currency == "":
		return "currency not established"
	case attempt.Gateway != record.Gateway:
		return fmt.Sprintf("gateway mismatch: record %q, attempt %q", record.Gateway, attempt.Gateway)
	case attempt.OrderID != record.OrderID:
		return "order mismatch between the record and its attempt"
	case attempt.Status == "SUCCEEDED" && attempt.GatewayPaymentID != nil && *attempt.GatewayPaymentID == record.GatewayPaymentID:
		return "this payment funded a successful checkout and must not be refunded"
	case record.Reason == "":
		return "compensation reason not established"
	}
	return ""
}

// ---- submission ------------------------------------------------------------------

const claimCompensationQuery = `
WITH due AS (
	SELECT id
	FROM payments.compensation_records
	WHERE gateway = ? AND status = 'REQUIRED' AND next_attempt_at <= now()
	ORDER BY next_attempt_at, created_at
	FOR UPDATE SKIP LOCKED
	LIMIT ?
)
UPDATE payments.compensation_records AS c
SET status = 'SUBMITTING',
	operation_id = gen_random_uuid(),
	attempts = c.attempts + 1,
	retry_count = 0,
	locked_by = ?,
	locked_until = now() + (? * interval '1 microsecond'),
	submitted_at = NULL,
	unknown_since = NULL,
	last_error = NULL,
	updated_at = now()
FROM due
WHERE c.id = due.id
RETURNING c.*`

// submitRequired claims due REQUIRED records and submits each. The claim is one short
// transaction; the operation id it mints is committed before any request is sent.
func (w *CompensationWorker) submitRequired(ctx context.Context) error {
	var claimed []models.CompensationRecord
	err := w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Raw(claimCompensationQuery, w.gatewayName, w.config.Batch, w.workerID, w.config.Lease.Microseconds()).
			Scan(&claimed).Error; err != nil {
			return fmt.Errorf("claim compensation work: %w", err)
		}
		for _, record := range claimed {
			if err := addCompensationEvent(tx, record.ID, models.CompensationRequired, models.CompensationSubmitting,
				fmt.Sprintf("claimed by %s as operation %s", w.workerID, record.OperationID)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, record := range claimed {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		w.submit(ctx, record, true)
	}
	return nil
}

func (w *CompensationWorker) mine(record models.CompensationRecord) recordGuard {
	return recordGuard{status: models.CompensationSubmitting, operationID: record.OperationID, lockedBy: w.workerID}
}

// submit sends one claimed operation. precheck is false for a same-key resubmission
// of an unknown operation (the amount_refunded baseline is already stored).
func (w *CompensationWorker) submit(ctx context.Context, record models.CompensationRecord, precheck bool) {
	guard := w.mine(record)

	// Never refund the payment that funded fulfilment, whatever the record says.
	if problem := w.eligibilityProblem(ctx, record); problem != "" {
		w.toReview(ctx, record, guard, problem)
		return
	}

	if precheck {
		info, err := w.callPayment(ctx, record.GatewayPaymentID)
		if err != nil {
			if errors.Is(err, paymentgateway.ErrPaymentNotFound) {
				w.toReview(ctx, record, guard, "the gateway does not know this payment id")
				return
			}
			// Nothing has been sent: start again later.
			_, _ = w.transition(ctx, record, guard, models.CompensationRequired,
				unlocked(map[string]any{"next_attempt_at": w.now().Add(w.config.RetryDelay), "last_error": truncateText(err.Error(), 300)}),
				"payment lookup failed before submission; nothing was sent: "+truncateText(err.Error(), 120))
			return
		}
		switch {
		case info.Status != "captured":
			w.toReview(ctx, record, guard, fmt.Sprintf("the payment is %q at the gateway, not captured", info.Status))
			return
		case info.AmountPaise != record.AmountPaise || (info.Currency != "" && info.Currency != record.Currency):
			w.toReview(ctx, record, guard, fmt.Sprintf("amount mismatch: record %d %s, gateway %d %s",
				record.AmountPaise, record.Currency, info.AmountPaise, info.Currency))
			return
		case info.AmountRefundedPaise > 0:
			w.toReview(ctx, record, guard, fmt.Sprintf(
				"the gateway already shows %d of this payment refunded before our submission (a refund we did not create); confirm and close",
				info.AmountRefundedPaise))
			return
		}
		before := info.AmountRefundedPaise
		record.AmountRefundedBefore = &before
	}

	// From here the request may reach the gateway: record that first, so a worker that
	// stops from now on is recovered as UNKNOWN rather than as "never sent".
	sentAt := w.now()
	updates := map[string]any{"submitted_at": sentAt}
	if record.AmountRefundedBefore != nil {
		updates["amount_refunded_before"] = *record.AmountRefundedBefore
	}
	if applied, err := w.touch(ctx, record, guard, updates); err != nil || !applied {
		return
	}

	operation := record.OperationID.String()
	refundCtx, cancel := context.WithTimeout(ctx, w.config.GatewayTimeout)
	result, err := w.refunder.Refund(refundCtx, paymentgateway.RefundRequest{
		PaymentID:      record.GatewayPaymentID,
		AmountPaise:    record.AmountPaise,
		Receipt:        operation,
		Notes:          map[string]string{"operation_id": operation, "order_id": record.OrderID.String(), "record_id": record.ID.String()},
		IdempotencyKey: "cmp-" + operation,
	})
	cancel()
	w.afterSubmit(ctx, record, guard, result, err)
}

// touch updates a SUBMITTING record without changing its status.
func (w *CompensationWorker) touch(ctx context.Context, record models.CompensationRecord, guard recordGuard, updates map[string]any) (bool, error) {
	query := w.db.WithContext(ctx).Table("payments.compensation_records").
		Where("id = ? AND status = ? AND operation_id = ? AND locked_by = ?", record.ID, guard.status, *guard.operationID, guard.lockedBy)
	set := map[string]any{"updated_at": w.now()}
	for key, value := range updates {
		set[key] = value
	}
	result := query.Updates(set)
	return result.RowsAffected == 1, result.Error
}

// afterSubmit records the outcome of one submission. A transport failure, timeout or
// 5xx is UNKNOWN: it is never converted into proof that no refund exists.
func (w *CompensationWorker) afterSubmit(
	ctx context.Context,
	record models.CompensationRecord,
	guard recordGuard,
	result paymentgateway.RefundResult,
	err error,
) {
	var rejected *paymentgateway.ErrRejected
	var conflict *paymentgateway.ErrRefundConflict
	switch {
	case err == nil:
		w.adopt(ctx, record, guard, result, "accepted by the gateway")
	case errors.As(err, &rejected):
		// Definitive refusal: the gateway created nothing.
		_, _ = w.transition(ctx, record, guard, models.CompensationFailed,
			unlocked(map[string]any{
				"failure_count": record.FailureCount + 1, "last_error": truncateText(err.Error(), 300),
				"next_attempt_at": w.now().Add(w.config.RetryDelay),
			}),
			"rejected before any refund was created: "+truncateText(err.Error(), 160))
	case errors.Is(err, paymentgateway.ErrFullyRefunded):
		w.markUnknown(ctx, record, guard, "the gateway says the payment is already fully refunded: an earlier request may have succeeded")
	case errors.As(err, &conflict):
		w.markUnknown(ctx, record, guard, "idempotency conflict: "+conflict.Description)
	default:
		w.markUnknown(ctx, record, guard, "ambiguous outcome: "+truncateText(err.Error(), 200))
	}
}

func (w *CompensationWorker) markUnknown(ctx context.Context, record models.CompensationRecord, guard recordGuard, why string) {
	since := w.now()
	if record.UnknownSince != nil {
		since = *record.UnknownSince // a resubmission keeps the original clock
	}
	if _, err := w.transition(ctx, record, guard, models.CompensationUnknown,
		unlocked(map[string]any{"unknown_since": since, "next_attempt_at": w.now().Add(w.config.PendingPoll), "last_error": truncateText(why, 300)}),
		"outcome unknown: "+truncateText(why, 200)); err != nil {
		slog.Error("mark compensation unknown", "record_id", record.ID, "error", err)
	}
}

// adopt stores a provider refund against the record, mapping the provider's status
// onto our workflow state.
func (w *CompensationWorker) adopt(
	ctx context.Context,
	record models.CompensationRecord,
	guard recordGuard,
	refund paymentgateway.RefundResult,
	how string,
) {
	to, updates, detail := providerOutcome(w, record, refund)
	updates["refund_id"] = refund.ID
	updates["provider_status"] = refund.Status
	updates["last_error"] = nil
	if _, err := w.transition(ctx, record, guard, to, unlocked(updates), how+"; "+detail); err != nil {
		slog.Error("record refund outcome", "record_id", record.ID, "error", err)
	}
}

// providerOutcome maps a provider refund status to a workflow state.
func providerOutcome(w *CompensationWorker, record models.CompensationRecord, refund paymentgateway.RefundResult) (string, map[string]any, string) {
	switch strings.ToLower(refund.Status) {
	case "processed":
		return models.CompensationProcessed, map[string]any{}, "provider status processed: refund " + refund.ID + " is complete"
	case "failed":
		return models.CompensationReview, map[string]any{
			"review_reason": "the provider reported refund " + refund.ID + " as failed; money was not returned",
		}, "provider status failed: refund " + refund.ID + " did not complete"
	default:
		return models.CompensationPending, map[string]any{"next_attempt_at": w.now().Add(w.config.PendingPoll)},
			"provider status " + refund.Status + ": refund " + refund.ID + " is not complete yet"
	}
}

func (w *CompensationWorker) callPayment(ctx context.Context, id string) (paymentgateway.PaymentInfo, error) {
	callCtx, cancel := context.WithTimeout(ctx, w.config.GatewayTimeout)
	defer cancel()
	return w.payments.FetchPayment(callCtx, id)
}

// ---- reconciliation ------------------------------------------------------------------

// recoverFailed applies the recovery policy to definitively failed (rejected, nothing
// created) operations: retry with a NEW operation a bounded number of times, then
// review.
func (w *CompensationWorker) recoverFailed(ctx context.Context) error {
	var failed []models.CompensationRecord
	if err := w.db.WithContext(ctx).
		Where("gateway = ? AND status = ? AND next_attempt_at <= ?", w.gatewayName, models.CompensationFailed, w.now()).
		Limit(w.config.Batch).Find(&failed).Error; err != nil {
		return err
	}
	for _, record := range failed {
		guard := recordGuard{status: models.CompensationFailed}
		if record.FailureCount > w.config.MaxFailedRetries {
			w.toReview(ctx, record, guard, fmt.Sprintf("refund rejected %d times: %s", record.FailureCount, deref(record.LastError)))
			continue
		}
		if _, err := w.transition(ctx, record, guard, models.CompensationRequired,
			map[string]any{"next_attempt_at": w.now()},
			fmt.Sprintf("retry %d of %d with a new operation after a rejection", record.FailureCount, w.config.MaxFailedRetries)); err != nil {
			return err
		}
	}
	return nil
}

func deref(text *string) string {
	if text == nil {
		return ""
	}
	return *text
}

// reconcileUnknown resolves operations whose outcome is unknown. The order matters:
// look for a refund carrying our operation id; then look at payment.amount_refunded;
// only then, inside a short window, resubmit with the SAME key and body; and after the
// window escalate. An empty lookup shortly after a timeout proves nothing.
func (w *CompensationWorker) reconcileUnknown(ctx context.Context) error {
	var unknown []models.CompensationRecord
	if err := w.db.WithContext(ctx).
		Where("gateway = ? AND status = ? AND next_attempt_at <= ?", w.gatewayName, models.CompensationUnknown, w.now()).
		Limit(w.config.Batch).Find(&unknown).Error; err != nil {
		return err
	}
	for _, record := range unknown {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		w.reconcileOneUnknown(ctx, record)
	}
	return nil
}

func (w *CompensationWorker) reconcileOneUnknown(ctx context.Context, record models.CompensationRecord) {
	guard := recordGuard{status: models.CompensationUnknown, operationID: record.OperationID}
	wait := func(why string) {
		_, _ = w.touchUnknown(ctx, record, map[string]any{"next_attempt_at": w.now().Add(w.config.PendingPoll), "last_error": truncateText(why, 300)})
	}

	listCtx, cancel := context.WithTimeout(ctx, w.config.GatewayTimeout)
	refunds, err := w.lookup.ListPaymentRefunds(listCtx, record.GatewayPaymentID)
	cancel()
	if err != nil {
		wait("refund list failed: " + err.Error())
		return
	}
	operation := ""
	if record.OperationID != nil {
		operation = record.OperationID.String()
	}
	for _, refund := range refunds {
		if refund.Receipt == operation || refund.Notes["operation_id"] == operation {
			w.adopt(ctx, record, guard, refund, "found at the gateway by operation id")
			return
		}
	}

	info, err := w.callPayment(ctx, record.GatewayPaymentID)
	if err != nil {
		wait("payment lookup failed: " + err.Error())
		return
	}
	before := int64(0)
	if record.AmountRefundedBefore != nil {
		before = *record.AmountRefundedBefore
	}
	age := w.now().Sub(w.unknownSince(record))

	// The gateway shows refunded money that no visible refund explains: a refund exists
	// (ours with delayed visibility, or one we did not create). Keep looking; escalate
	// if it never becomes explainable.
	if info.AmountRefundedPaise > before {
		if age >= w.config.UnknownMaxAge {
			w.toReview(ctx, record, guard, fmt.Sprintf(
				"the gateway shows %d refunded (was %d) but no refund carrying operation %s is visible after %s",
				info.AmountRefundedPaise, before, operation, age.Round(time.Second)))
			return
		}
		wait(fmt.Sprintf("payment.amount_refunded rose to %d but the refund is not visible yet", info.AmountRefundedPaise))
		return
	}

	// Nothing found and nothing refunded. That is not proof of absence: lookups may lag
	// and the request may still be in flight.
	switch {
	case age < w.config.AbsenceWindow:
		wait("no refund visible yet; within the absence window, so this proves nothing")
	case age < w.config.IdempotentRetryWindow && record.RetryCount < w.config.MaxIdempotentRetries:
		w.resubmit(ctx, record)
	default:
		w.toReview(ctx, record, guard, fmt.Sprintf(
			"no refund carrying operation %s found after %s and payment.amount_refunded is unchanged (%d); the request may or may not have reached the gateway; check the gateway before retrying",
			operation, age.Round(time.Second), info.AmountRefundedPaise))
	}
}

func (w *CompensationWorker) unknownSince(record models.CompensationRecord) time.Time {
	if record.UnknownSince != nil {
		return *record.UnknownSince
	}
	return record.UpdatedAt
}

func (w *CompensationWorker) touchUnknown(ctx context.Context, record models.CompensationRecord, updates map[string]any) (bool, error) {
	set := map[string]any{"updated_at": w.now()}
	for key, value := range updates {
		set[key] = value
	}
	result := w.db.WithContext(ctx).Table("payments.compensation_records").
		Where("id = ? AND status = ? AND operation_id = ?", record.ID, models.CompensationUnknown, *record.OperationID).Updates(set)
	return result.RowsAffected == 1, result.Error
}

// resubmit sends the SAME operation again (same operation id, receipt, idempotency
// key and body), which Razorpay documents as safe to retry. It is bounded in number
// and time because key retention is undocumented.
func (w *CompensationWorker) resubmit(ctx context.Context, record models.CompensationRecord) {
	moved, err := w.transition(ctx, record, recordGuard{status: models.CompensationUnknown, operationID: record.OperationID},
		models.CompensationSubmitting, map[string]any{
			"retry_count": record.RetryCount + 1, "locked_by": w.workerID,
			"locked_until": w.now().Add(w.config.Lease), "submitted_at": nil,
		}, fmt.Sprintf("resubmitting operation %s with the same idempotency key (attempt %d)", record.OperationID, record.RetryCount+1))
	if err != nil || !moved {
		return
	}
	record.RetryCount++
	record.Status = models.CompensationSubmitting
	w.submit(ctx, record, false)
}

// reconcilePending follows an accepted refund until the provider says processed or
// failed.
func (w *CompensationWorker) reconcilePending(ctx context.Context) error {
	var pending []models.CompensationRecord
	if err := w.db.WithContext(ctx).
		Where("gateway = ? AND status = ? AND next_attempt_at <= ?", w.gatewayName, models.CompensationPending, w.now()).
		Limit(w.config.Batch).Find(&pending).Error; err != nil {
		return err
	}
	for _, record := range pending {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		guard := recordGuard{status: models.CompensationPending}
		if record.RefundID == nil || *record.RefundID == "" {
			w.toReview(ctx, record, guard, "pending record has no refund id")
			continue
		}
		fetchCtx, cancel := context.WithTimeout(ctx, w.config.GatewayTimeout)
		refund, err := w.lookup.FetchRefund(fetchCtx, *record.RefundID)
		cancel()
		switch {
		case errors.Is(err, paymentgateway.ErrRefundNotFound):
			w.toReview(ctx, record, guard, "the gateway does not know refund "+*record.RefundID+" that it earlier accepted")
		case err != nil:
			_, _ = w.touchPending(ctx, record, map[string]any{"next_attempt_at": w.now().Add(w.config.PendingPoll), "last_error": truncateText(err.Error(), 300)})
		default:
			to, updates, detail := providerOutcome(w, record, refund)
			updates["provider_status"] = refund.Status
			if to == models.CompensationPending {
				// unchanged: just schedule the next look, without a history entry per poll
				_, _ = w.touchPending(ctx, record, updates)
				continue
			}
			if _, err := w.transition(ctx, record, guard, to, unlocked(updates), "reconciled by fetch; "+detail); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *CompensationWorker) touchPending(ctx context.Context, record models.CompensationRecord, updates map[string]any) (bool, error) {
	set := map[string]any{"updated_at": w.now()}
	for key, value := range updates {
		set[key] = value
	}
	result := w.db.WithContext(ctx).Table("payments.compensation_records").
		Where("id = ? AND status = ?", record.ID, models.CompensationPending).Updates(set)
	return result.RowsAffected == 1, result.Error
}

// ---- provider events ---------------------------------------------------------------

// ApplyProviderRefund applies a refund reported by a webhook to the matching record,
// inside the caller's transaction. It only moves a record forward: a PROCESSED record
// never regresses, and a late "pending" event changes nothing. A refund that does not
// carry our operation id or refund id is one we did not create: while the record is
// still unsubmitted it is sent to review instead of being adopted.
func (w *CompensationWorker) ApplyProviderRefund(tx *gorm.DB, refund paymentgateway.RefundResult) (string, error) {
	var record models.CompensationRecord
	err := tx.Where("gateway = ? AND gateway_payment_id = ?", w.gatewayName, refund.PaymentID).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "no compensation record for payment " + refund.PaymentID, nil
	}
	if err != nil {
		return "", err
	}
	ours := (record.RefundID != nil && *record.RefundID == refund.ID) ||
		(record.OperationID != nil && (refund.Receipt == record.OperationID.String() || refund.Notes["operation_id"] == record.OperationID.String()))
	if !ours {
		switch record.Status {
		case models.CompensationRequired, models.CompensationSubmitting, models.CompensationUnknown:
			reason := "a refund (" + refund.ID + ") that this workflow did not create appeared for the payment; confirm and close"
			return "foreign refund: " + reason, w.eventGuardedMove(tx, record, models.CompensationReview, map[string]any{"review_reason": reason}, reason)
		}
		return "refund " + refund.ID + " is not this record's operation; ignored", nil
	}
	if record.Status == models.CompensationProcessed || record.Status == models.CompensationReview {
		return "record already " + record.Status, nil
	}
	to, updates, detail := providerOutcome(w, record, refund)
	if to == models.CompensationPending && record.Status == models.CompensationPending {
		return "still pending", nil
	}
	updates["refund_id"], updates["provider_status"] = refund.ID, refund.Status
	updates["locked_by"], updates["locked_until"] = nil, nil
	return "adopted from webhook: " + detail, w.eventGuardedMove(tx, record, to, updates, "webhook; "+detail)
}

// eventGuardedMove changes a record's state and logs it, guarded by its current status.
func (w *CompensationWorker) eventGuardedMove(tx *gorm.DB, record models.CompensationRecord, to string, updates map[string]any, detail string) error {
	set := map[string]any{"status": to, "updated_at": w.now()}
	for key, value := range updates {
		set[key] = value
	}
	result := tx.Table("payments.compensation_records").Where("id = ? AND status = ?", record.ID, record.Status).Updates(set)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return nil
	}
	return addCompensationEvent(tx, record.ID, record.Status, to, detail)
}
