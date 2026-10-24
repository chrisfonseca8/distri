package services

import (
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/payment-service/database/models"
	"github.com/chrisfonseca08/distri/payment-service/pkg/payment_gateway"
)

func TestPaymentResultUpdatesForAsyncGateway(t *testing.T) {
	awaiting := paymentResultUpdates("", &paymentgateway.ErrAwaitingPayment{Ref: "order_ABC"})
	if awaiting["status"] != "AWAITING_PAYMENT" || awaiting["gateway_order_id"] != "order_ABC" || awaiting["failure_code"] != nil {
		t.Fatalf("awaiting updates = %v", awaiting)
	}
	rejected := paymentResultUpdates("", &paymentgateway.ErrRejected{Code: "BAD_REQUEST_ERROR"})
	if rejected["status"] != "FAILED" || rejected["failure_code"] != "GATEWAY_REJECTED" {
		t.Fatalf("rejected updates = %v", rejected)
	}
}

func TestDecideReconcile(t *testing.T) {
	const window = 5 * time.Minute
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	deadline := now.Add(-time.Minute) // for "expired" attempts the deadline is a minute ago
	attempt := func(status string, age time.Duration) models.PaymentAttempt {
		return models.PaymentAttempt{
			ID: uuid.New(), OrderID: uuid.New(), AmountPaise: 10000, Currency: "INR", Status: status,
			CreatedAt: now.Add(-age),
		}
	}
	withExpiry := func(a models.PaymentAttempt, at time.Time) models.PaymentAttempt {
		a.ExpiresAt = &at
		return a
	}
	withPayment := func(a models.PaymentAttempt, paymentID string) models.PaymentAttempt {
		a.GatewayPaymentID = &paymentID
		return a
	}
	unpaid := paymentgateway.OrderState{GatewayOrderID: "order_1", AmountPaise: 10000}
	captured := func(observedAt time.Time) paymentgateway.OrderState {
		return paymentgateway.OrderState{
			GatewayOrderID: "order_1", AmountPaise: 10000, CapturedPaymentID: "pay_1", CapturedAmountPaise: 10000,
			CapturedCurrency: "INR", ObservedAt: observedAt, Source: "webhook",
		}
	}
	wrongAmount := captured(now)
	wrongAmount.CapturedAmountPaise = 9000
	wrongCurrency := captured(now)
	wrongCurrency.CapturedCurrency = "USD"
	extra := paymentgateway.CapturedPayment{PaymentID: "pay_extra", AmountPaise: 10000, Currency: "INR"}
	withExtra := captured(now.Add(-time.Hour))
	withExtra.ExtraCaptured = []paymentgateway.CapturedPayment{extra}
	lateWithExtra := captured(now)
	lateWithExtra.ExtraCaptured = []paymentgateway.CapturedPayment{extra}
	unestablished := captured(now.Add(-time.Hour))
	unestablished.ExtraCaptured = []paymentgateway.CapturedPayment{{PaymentID: "pay_nocurrency", AmountPaise: 10000}}

	awaitingOpen := withExpiry(attempt("AWAITING_PAYMENT", time.Minute), now.Add(time.Minute))
	awaitingExpired := withExpiry(attempt("AWAITING_PAYMENT", 9*time.Minute), deadline)

	tests := []struct {
		name         string
		attempt      models.PaymentAttempt
		state        paymentgateway.OrderState
		found        bool
		wantStatus   any // nil means the attempt row does not change
		wantFail     any
		wantExpiry   bool
		compensation []string // "paymentID:status" in order
	}{
		{"observed before the deadline succeeds", awaitingOpen, captured(now), true, "SUCCEEDED", nil, false, nil},
		{"observed before the deadline succeeds even if decided after it (processing lag is not penalised)", awaitingExpired, captured(deadline.Add(-time.Second)), true, "SUCCEEDED", nil, false, nil},
		{"first seen after the deadline is late: expired and compensated, never completed", awaitingExpired, captured(now), true, "FAILED", "PAYMENT_EXPIRED", false, []string{"pay_1:REQUIRED"}},
		{"observed exactly at the deadline is late (the window is half-open)", awaitingExpired, captured(deadline), true, "FAILED", "PAYMENT_EXPIRED", false, []string{"pay_1:REQUIRED"}},
		{"unset observation time falls back to the decision time", awaitingExpired, paymentgateway.OrderState{GatewayOrderID: "o", CapturedPaymentID: "pay_1", CapturedAmountPaise: 10000, CapturedCurrency: "INR"}, true, "FAILED", "PAYMENT_EXPIRED", false, []string{"pay_1:REQUIRED"}},
		{"still unpaid inside the window waits", awaitingOpen, unpaid, true, nil, nil, false, nil},
		{"still unpaid past the window expires (deferrable)", awaitingExpired, unpaid, true, "FAILED", "PAYMENT_EXPIRED", true, nil},
		{"amount mismatch fails and owes a refund", awaitingOpen, wrongAmount, true, "FAILED", "AMOUNT_MISMATCH", false, []string{"pay_1:REQUIRED"}},
		{"currency mismatch fails and owes a refund", awaitingOpen, wrongCurrency, true, "FAILED", "AMOUNT_MISMATCH", false, []string{"pay_1:REQUIRED"}},
		{"unknown with an existing order becomes awaiting", attempt("UNKNOWN", time.Minute), unpaid, true, "AWAITING_PAYMENT", nil, false, nil},
		{"unknown and captured in time succeeds", attempt("UNKNOWN", time.Minute), captured(now), true, "SUCCEEDED", nil, false, nil},
		{"unknown with no order inside the window waits", attempt("UNKNOWN", time.Minute), paymentgateway.OrderState{}, false, nil, nil, false, nil},
		{"unknown with no order past the window fails (deferrable)", attempt("UNKNOWN", 6*time.Minute), paymentgateway.OrderState{}, false, "FAILED", "NO_GATEWAY_ORDER", true, nil},
		{"an on-time capture keeps the funding payment and refunds the extra one", awaitingOpen, withExtra, true, "SUCCEEDED", nil, false, []string{"pay_extra:REQUIRED"}},
		{"a late capture plus an extra refunds both, each under its own id", awaitingExpired, lateWithExtra, true, "FAILED", "PAYMENT_EXPIRED", false, []string{"pay_1:REQUIRED", "pay_extra:REQUIRED"}},
		{"an extra capture without an established currency stays in review", awaitingOpen, unestablished, true, "SUCCEEDED", nil, false, []string{"pay_nocurrency:REVIEW"}},
		{"a cancelled attempt is never resurrected: a later capture is only compensated", attempt("FAILED", 20*time.Minute), captured(now.Add(-2 * time.Hour)), true, nil, nil, false, []string{"pay_1:REQUIRED"}},
		{"a cancelled attempt ignores an event that suggests an earlier capture", attempt("FAILED", 20*time.Minute), captured(now.Add(-24 * time.Hour)), true, nil, nil, false, []string{"pay_1:REQUIRED"}},
		{"every capture after cancellation is compensated separately", attempt("FAILED", time.Hour), withExtra, true, nil, nil, false, []string{"pay_1:REQUIRED", "pay_extra:REQUIRED"}},
		{"failed attempt without a capture stays failed", attempt("FAILED", 20*time.Minute), unpaid, true, nil, nil, false, nil},
		{"a different capture on a settled attempt is extra money to refund", withPayment(attempt("SUCCEEDED", time.Minute), "pay_first"), captured(now), true, nil, nil, false, []string{"pay_1:REQUIRED"}},
		{"the capture that funded the settled attempt is never refunded", withPayment(attempt("SUCCEEDED", time.Minute), "pay_1"), captured(now), true, nil, nil, false, nil},
		{"only the additional capture is refunded when the funding one is seen again", withPayment(attempt("SUCCEEDED", time.Minute), "pay_1"), withExtra, true, nil, nil, false, []string{"pay_extra:REQUIRED"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := decideReconcile(test.attempt, test.state, test.found, now, window)
			if got := compensationSummary(decision); !equalStrings(got, test.compensation) {
				t.Fatalf("compensations = %v, want %v", got, test.compensation)
			}
			if decision.expiry != test.wantExpiry {
				t.Fatalf("expiry = %v, want %v", decision.expiry, test.wantExpiry)
			}
			if _, slot := decision.updates["refund_status"]; slot {
				t.Fatal("the legacy refund_status column must no longer be written; compensation records are the source of truth")
			}
			if test.wantStatus == nil {
				if decision.updates != nil {
					t.Fatalf("updates = %v, want the attempt unchanged (no second saga reply)", decision.updates)
				}
				return
			}
			if decision.updates == nil {
				t.Fatal("no change, want a transition")
			}
			if decision.updates["status"] != test.wantStatus {
				t.Fatalf("status = %v, want %v", decision.updates["status"], test.wantStatus)
			}
			if test.wantFail != nil && decision.updates["failure_code"] != test.wantFail {
				t.Fatalf("failure_code = %v, want %v", decision.updates["failure_code"], test.wantFail)
			}
		})
	}
}

func compensationSummary(decision reconcileDecision) []string {
	var out []string
	for _, draft := range decision.compensations {
		out = append(out, draft.PaymentID+":"+draft.Status)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
