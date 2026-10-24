package paymentgateway

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"uuid"
)

const (
	ProviderSimulator = "simulator"
	ProviderRazorpay  = "razorpay"

	// GatewayNameSimulator and GatewayNameRazorpay are the values stored in
	// payment_attempts.gateway.
	GatewayNameSimulator = "sim"
	GatewayNameRazorpay  = "razorpay"
	// GatewayNameRazorpaySim labels attempts created through the Razorpay adapter
	// against any endpoint other than the real API (the gateway simulator). The
	// reconciler only touches attempts carrying its own gateway name, so a stack
	// pointed at the real API never polls or refunds simulator payments, and a
	// stack pointed at the simulator never acts on real ones.
	GatewayNameRazorpaySim = "razorpay-sim"
)

// ErrAwaitingPayment is returned by a gateway whose CreateCharge only creates
// the provider's order. Money has not moved: the customer still has to pay, and
// the outcome is learned later by looking the order up. It is not a failure.
type ErrAwaitingPayment struct {
	Ref string
}

// Error describes the order that is still waiting for the customer to pay.
func (e *ErrAwaitingPayment) Error() string {
	return fmt.Sprintf("gateway order %s is awaiting payment", e.Ref)
}

// ErrNotSent means the request provably never left this process: the connection to
// the provider could not be opened (connection refused, DNS failure, dial timeout).
// Unlike a timeout after sending, nothing can have been created at the provider, so
// the call is safe to try again.
type ErrNotSent struct {
	Err error
}

// Error describes why the request could not be sent.
func (e *ErrNotSent) Error() string { return "request was not sent: " + e.Err.Error() }

// Unwrap returns the underlying network error.
func (e *ErrNotSent) Unwrap() error { return e.Err }

// ErrRejected is a confirmed refusal by the provider of the request itself (for
// example invalid input): nothing was created. It is different from a payment
// that was declined, and from an ambiguous failure such as a timeout.
type ErrRejected struct {
	Code        string
	Description string
}

// Error describes why the provider refused the request.
func (e *ErrRejected) Error() string {
	return fmt.Sprintf("gateway rejected the request: %s: %s", e.Code, e.Description)
}

// ErrOrderNotFound means the provider has no order for the checkout.
var ErrOrderNotFound = errors.New("gateway order not found")

// ErrFullyRefunded means the provider says the payment has been fully refunded
// already. After a refund call of unknown outcome this is a strong hint that the
// earlier call succeeded, so it is reconciled by lookup, never treated as failure.
var ErrFullyRefunded = errors.New("payment is already fully refunded")

// ErrRefundNotFound means the provider has no refund with that id.
var ErrRefundNotFound = errors.New("gateway refund not found")

// ErrPaymentNotFound means the provider has no payment with that id.
var ErrPaymentNotFound = errors.New("gateway payment not found")

// ErrRefundConflict is HTTP 409 on a refund create: another request with the same
// idempotency key is still in progress, or the key was used with a different
// request. The outcome of the original request is not known from this response.
type ErrRefundConflict struct{ Description string }

// Error describes the refund conflict reported by the provider.
func (e *ErrRefundConflict) Error() string { return "refund conflict: " + e.Description }

// OrderState is what a provider reports about the order of one checkout.
type OrderState struct {
	GatewayOrderID      string
	AmountPaise         int64
	CapturedPaymentID   string // empty until a payment is captured
	CapturedAmountPaise int64
	CapturedCurrency    string // empty when the source did not report it
	// ObservedAt is when WE first saw the payment captured, on our clock: the
	// webhook's local receipt time, or the moment a lookup returned. It is the only
	// timestamp the deadline decision uses. A provider timestamp (event created_at,
	// payment created_at) is deliberately not carried here: Razorpay does not define
	// the event's created_at as capture time, and the payment has no captured_at.
	ObservedAt time.Time
	// Source says where the capture was observed: "webhook" or "poll".
	Source string
	// ExtraCaptured lists further captured payments on the same checkout (for
	// example on a duplicate gateway order). Each is distinct money to compensate.
	ExtraCaptured   []CapturedPayment
	LastFailureCode string // most recent failed payment attempt, if any
}

// CapturedPayment is one captured gateway payment.
type CapturedPayment struct {
	PaymentID   string
	AmountPaise int64
	Currency    string
}

// OrderLookup is implemented by gateways that can be asked about an order. The
// lookup is keyed by the checkout's order ID, which the adapter sent as the
// provider-side idempotency handle.
type OrderLookup interface {
	LookupOrder(ctx context.Context, orderID uuid.UUID) (OrderState, error)
}

// RefundRequest is one refund operation. A full refund has AmountPaise equal to
// the captured amount. IdempotencyKey is sent in Razorpay's documented
// X-Refund-Idempotency header (at least 10 characters of letters, digits, '-', '_');
// Receipt and Notes are correlation data echoed back by the provider.
type RefundRequest struct {
	PaymentID      string
	AmountPaise    int64
	Receipt        string
	Notes          map[string]string
	IdempotencyKey string
}

// RefundResult is a provider refund. Status is the provider's own status
// (pending, processed, failed), not our workflow state.
type RefundResult struct {
	ID          string
	Status      string
	PaymentID   string
	AmountPaise int64
	Receipt     string
	Notes       map[string]string
}

// PaymentInfo is what the provider reports about one payment.
type PaymentInfo struct {
	ID                  string
	OrderID             string
	Status              string
	Currency            string
	AmountPaise         int64
	AmountRefundedPaise int64
}

// Refunder is implemented by gateways that can refund a captured payment.
type Refunder interface {
	Refund(ctx context.Context, request RefundRequest) (RefundResult, error)
}

// RefundLookup is implemented by gateways whose refunds can be looked up. These
// are the documented fetch endpoints (by refund id, and the refunds of a payment).
type RefundLookup interface {
	FetchRefund(ctx context.Context, refundID string) (RefundResult, error)
	ListPaymentRefunds(ctx context.Context, paymentID string) ([]RefundResult, error)
}

// PaymentFetcher is implemented by gateways that can report a payment, including
// how much of it has been refunded by anyone.
type PaymentFetcher interface {
	FetchPayment(ctx context.Context, paymentID string) (PaymentInfo, error)
}

// NewFromEnv builds the gateway selected by GATEWAY_PROVIDER (default
// simulator) and returns the name stored on payment attempts.
func NewFromEnv() (Gateway, string, error) {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("GATEWAY_PROVIDER")))
	switch provider {
	case "", ProviderSimulator:
		gateway, err := NewSimulatorFromEnv()
		if err != nil {
			return nil, "", err
		}
		return gateway, GatewayNameSimulator, nil
	case ProviderRazorpay:
		gateway, err := NewRazorpayFromEnv()
		if err != nil {
			return nil, "", err
		}
		if gateway.baseURL != defaultRazorpayBaseURL {
			return gateway, GatewayNameRazorpaySim, nil
		}
		return gateway, GatewayNameRazorpay, nil
	default:
		return nil, "", fmt.Errorf("unsupported GATEWAY_PROVIDER %q", provider)
	}
}
