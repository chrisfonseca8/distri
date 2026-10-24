package sim

import (
	"encoding/json"
	"fmt"
	"time"
)

// Plan scripts one checkout deterministically: when the simulated customer
// attempts payment and what happens, and, independently, when (or whether) each
// webhook event is delivered. It replaces the seeded randomness for that checkout,
// so a scenario does not depend on sleeps or on goroutine scheduling.
//
// All durations in a plan are exact and are NOT divided by SIM_TIME_SCALE: a plan
// is a script, not a distribution. Attempt offsets count from the creation of the
// gateway order; delivery delays count from the moment the event is created.
type Plan struct {
	Attempts []PlanAttempt           `json:"attempts"`
	Delivery map[string]DeliveryRule `json:"delivery"`
	// Refunds scripts the refunds created on this order's payments, by the order
	// they are created in (the first refund uses Refunds[0], and so on). A refund
	// without a rule behaves as the simulator default: pending, then processed after
	// SIM_REFUND_DELAY (seeded).
	Refunds []RefundRule `json:"refunds"`
}

// RefundRule scripts one refund. The refund's own state transitions are committed
// by the simulator regardless of what the caller sees or when webhooks arrive.
type RefundRule struct {
	// Outcome: processed (immediately), pending_processed (pending, then processed
	// After later) or pending_failed (pending, then failed After later).
	Outcome string `json:"outcome"`
	// After is the exact time a pending refund takes to reach its final state.
	After *Duration `json:"after,omitempty"`
	// Response shapes what the caller sees:
	//   ok            the normal response (default)
	//   lost          the refund is committed, then the connection is closed: no response
	//   slow          committed, then answered ResponseDelay later (longer than the client timeout)
	//   reject        HTTP 400 before any refund exists (injected fault: nothing is created)
	//   error_before  HTTP 500 before any refund exists (injected fault: nothing is created)
	//   error_after   the refund is committed, then HTTP 500 (injected fault)
	Response      string    `json:"response,omitempty"`
	ResponseDelay *Duration `json:"response_delay,omitempty"`
	// VisibleAfter hides the refund from the fetch and list endpoints for this long
	// after it is accepted (delayed visibility; payment.amount_refunded is immediate).
	VisibleAfter *Duration `json:"visible_after,omitempty"`
}

// PlanAttempt is one customer payment attempt.
type PlanAttempt struct {
	// At is the offset from order creation.
	At Duration `json:"at"`
	// Outcome is decline (a failed attempt; the order stays payable), authorize
	// (authorized, waiting for a manual capture) or capture (authorized, then
	// captured CaptureAfter later when the order's capture mode is automatic).
	Outcome string `json:"outcome"`
	// CaptureAfter is the time from authorization to capture; zero-length means
	// the simulator's default (seeded) capture delay.
	CaptureAfter *Duration `json:"capture_after,omitempty"`
}

// DeliveryRule shapes the delivery of every event of one name for this checkout.
type DeliveryRule struct {
	// Delay is the exact time from event creation to the first delivery attempt;
	// nil keeps the seeded default.
	Delay *Duration `json:"delay,omitempty"`
	// Omit records the event but never delivers it (a lost webhook).
	Omit bool `json:"omit,omitempty"`
	// Duplicates are extra deliveries of the same event (same id, same bytes) at
	// these offsets after the first attempt's due time.
	Duplicates []Duration `json:"duplicates,omitempty"`
}

// Duration is a time.Duration that reads and writes as "1500ms" / "2s".
type Duration struct{ time.Duration }

func (d *Duration) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("duration must be a string like \"2s\": %w", err)
	}
	parsed, err := time.ParseDuration(text)
	if err != nil {
		return err
	}
	d.Duration = parsed
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(d.Duration.String()) }

var knownEvents = map[string]bool{
	"payment.authorized": true, "payment.captured": true, "payment.failed": true, "order.paid": true,
	"refund.created": true, "refund.processed": true, "refund.failed": true,
}

func (p Plan) Validate() error {
	for i, attempt := range p.Attempts {
		if attempt.At.Duration < 0 {
			return fmt.Errorf("attempt %d: at must not be negative", i)
		}
		switch attempt.Outcome {
		case "decline", "authorize", "capture":
		default:
			return fmt.Errorf("attempt %d: outcome must be decline, authorize or capture, got %q", i, attempt.Outcome)
		}
		if attempt.CaptureAfter != nil && attempt.CaptureAfter.Duration < 0 {
			return fmt.Errorf("attempt %d: capture_after must not be negative", i)
		}
	}
	for i, rule := range p.Refunds {
		switch rule.Outcome {
		case "processed", "pending_processed", "pending_failed":
		default:
			return fmt.Errorf("refund rule %d: outcome must be processed, pending_processed or pending_failed, got %q", i, rule.Outcome)
		}
		switch rule.Response {
		case "", "ok", "lost", "slow", "reject", "error_before", "error_after":
		default:
			return fmt.Errorf("refund rule %d: unknown response %q", i, rule.Response)
		}
		for _, d := range []*Duration{rule.After, rule.ResponseDelay, rule.VisibleAfter} {
			if d != nil && d.Duration < 0 {
				return fmt.Errorf("refund rule %d: durations must not be negative", i)
			}
		}
	}
	for name, rule := range p.Delivery {
		if !knownEvents[name] {
			return fmt.Errorf("delivery rule for unknown event %q", name)
		}
		if rule.Delay != nil && rule.Delay.Duration < 0 {
			return fmt.Errorf("delivery rule %q: delay must not be negative", name)
		}
		for _, d := range rule.Duplicates {
			if d.Duration < 0 {
				return fmt.Errorf("delivery rule %q: duplicate offsets must not be negative", name)
			}
		}
	}
	return nil
}
