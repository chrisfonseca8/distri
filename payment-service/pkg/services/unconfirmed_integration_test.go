package services

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/payment-service/pkg/payment_gateway"
)

// unreachableGateway looks like a gateway that cannot be asked: every lookup fails, and
// the failure is NOT "order not found".
type unreachableGateway struct{ lookups int }

func (g *unreachableGateway) CreateCharge(context.Context, uuid.UUID, int64) (string, error) {
	return "", errors.New("not used")
}

func (g *unreachableGateway) LookupOrder(context.Context, uuid.UUID) (paymentgateway.OrderState, error) {
	g.lookups++
	return paymentgateway.OrderState{}, errors.New("gateway unreachable")
}

func reconcilerFor(t *testing.T, f *webhookFixture, gateway paymentgateway.Gateway, grace time.Duration) *PaymentReconciler {
	t.Helper()
	reconciler, err := NewPaymentReconciler(f.db, gateway, "razorpay", ReconcilerConfig{
		Interval: time.Second, MinPoll: 0, Batch: 10, PaymentWindow: 5 * time.Minute, GatewayTimeout: time.Second,
		AuditWindow: time.Hour, AuditInterval: time.Minute, UnconfirmedGrace: grace,
	})
	if err != nil {
		t.Fatalf("reconciler: %v", err)
	}
	return reconciler
}

// IMP-6: when the gateway cannot be asked for longer than the window plus the grace,
// the payment service decides FAILED (UNCONFIRMED) and tells the saga.
func TestUnreachableGatewayPastWindowPlusGraceFailsTheAttempt(t *testing.T) {
	f := newWebhookFixture(t, "AWAITING_PAYMENT", -10*time.Minute) // window ended 10 min ago
	gateway := &unreachableGateway{}
	if err := reconcilerFor(t, f, gateway, 5*time.Minute).ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	attempt := f.attempt(t)
	if attempt.Status != "FAILED" || attempt.FailureCode == nil || *attempt.FailureCode != "UNCONFIRMED" {
		t.Fatalf("attempt = %s / %v, want FAILED / UNCONFIRMED", attempt.Status, attempt.FailureCode)
	}
	if got := f.replies(t); len(got) != 1 || got[0] != "Failed" {
		t.Fatalf("replies = %v, want one Failed so the saga compensates", got)
	}
	if gateway.lookups == 0 {
		t.Fatal("the gateway must have been asked first")
	}
}

func TestUnreachableGatewayInsideTheGraceKeepsAskingAndNeverGuesses(t *testing.T) {
	f := newWebhookFixture(t, "AWAITING_PAYMENT", -2*time.Minute) // 2 min past the window, grace is 5
	if err := reconcilerFor(t, f, &unreachableGateway{}, 5*time.Minute).ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.attempt(t).Status; got != "AWAITING_PAYMENT" {
		t.Fatalf("status = %s, want unchanged inside the grace period", got)
	}
	if len(f.replies(t)) != 0 {
		t.Fatal("no reply may be sent yet")
	}
}

func TestUnconfirmedRuleCanBeSwitchedOff(t *testing.T) {
	f := newWebhookFixture(t, "UNKNOWN", -time.Hour)
	if err := reconcilerFor(t, f, &unreachableGateway{}, 0).ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.attempt(t).Status; got != "UNKNOWN" {
		t.Fatalf("status = %s, want UNKNOWN with the rule off", got)
	}
}

// A capture that turns up after the attempt was failed as UNCONFIRMED is refunded, never accepted.
func TestCaptureFoundAfterAnUnconfirmedFailureIsRefundedNotAccepted(t *testing.T) {
	f := newWebhookFixture(t, "AWAITING_PAYMENT", -10*time.Minute)
	if err := reconcilerFor(t, f, &unreachableGateway{}, 5*time.Minute).ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.attempt(t).Status; got != "FAILED" {
		t.Fatalf("setup: status = %s, want FAILED", got)
	}

	// the gateway comes back and reports the customer's payment as captured
	f.eventAt(t, "payment.captured", time.Now(), time.Now(), 25000, "INR", f.paymentID)
	processor, err := NewWebhookProcessor(f.db, WebhookProcessorConfig{
		Interval: time.Second, Batch: 10, Lease: time.Minute, MaxAttempts: 3, BackoffBase: time.Second,
		BackoffCap: time.Minute, PaymentWindow: 5 * time.Minute, GatewayName: "razorpay",
	})
	if err != nil {
		t.Fatalf("processor: %v", err)
	}
	if err := processor.ProcessOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.attempt(t).Status; got != "FAILED" {
		t.Fatalf("status = %s: a failed checkout must never be revived by a late capture", got)
	}
	var owed int64
	f.db.Raw(`SELECT count(*) FROM payments.compensation_records WHERE order_id = ? AND status = 'REQUIRED'`, f.orderID.String()).Scan(&owed)
	if owed != 1 {
		t.Fatalf("refund records owed = %d, want 1", owed)
	}
	if got := f.replies(t); len(got) != 1 {
		t.Fatalf("replies = %v, want only the original Failed (the saga is not told again)", got)
	}
}

// ---- IMP-13: a request that never left this process is told apart from an unknown outcome ----

func TestConnectionRefusedIsNotSentButATimeoutAfterSendingIsNot(t *testing.T) {
	// a server we close: nothing listens, so the dial fails and the request cannot have been sent
	closed := httptest.NewServer(http.NotFoundHandler())
	url := closed.URL
	closed.Close()
	refused, err := paymentgateway.NewRazorpay(paymentgateway.RazorpayConfig{BaseURL: url, KeyID: "rzp_test_x", KeySecret: "s", HTTPClient: &http.Client{Timeout: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	_, chargeErr := refused.CreateCharge(context.Background(), uuid.New(), 1000)
	var notSent *paymentgateway.ErrNotSent
	if !errors.As(chargeErr, &notSent) {
		t.Fatalf("error = %v, want ErrNotSent for a refused connection", chargeErr)
	}

	// a server that accepts the connection and never answers: the request WAS sent
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
		}
	}()
	silent, err := paymentgateway.NewRazorpay(paymentgateway.RazorpayConfig{BaseURL: "http://" + listener.Addr().String(), KeyID: "rzp_test_x", KeySecret: "s", HTTPClient: &http.Client{Timeout: 300 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	_, chargeErr = silent.CreateCharge(context.Background(), uuid.New(), 1000)
	if chargeErr == nil || errors.As(chargeErr, &notSent) {
		t.Fatalf("error = %v: a timeout after the connection was made is ambiguous, never 'not sent'", chargeErr)
	}
}

type notSentGateway struct{ calls int }

func (g *notSentGateway) CreateCharge(context.Context, uuid.UUID, int64) (string, error) {
	g.calls++
	return "", &paymentgateway.ErrNotSent{Err: errors.New("dial tcp: connection refused")}
}

func TestNotSentRequestIsRetriedAndLeavesTheAttemptCreated(t *testing.T) {
	db := openTestDB(t)
	orderID := uuid.New()
	cleanupOrder(t, db, orderID)
	created, err := NewPaymentService(db).CreatePayment(context.Background(), CreatePaymentInput{OrderID: orderID, AmountPaise: 1000})
	if err != nil {
		t.Fatal(err)
	}
	gateway := &notSentGateway{}
	handler := NewPaymentJobHandler(db, gateway, time.Second)

	outcome, err := handler.ProcessForJob(context.Background(), created.Attempt.ID, 1)
	if outcome != ProcessRetry || err == nil {
		t.Fatalf("outcome = %q, %v; want a retry", outcome, err)
	}
	var status string
	db.Raw(`SELECT status FROM payments.payment_attempts WHERE id = ?`, created.Attempt.ID.String()).Scan(&status)
	if status != "CREATED" {
		t.Fatalf("status = %s, want CREATED: nothing reached the gateway, so the claim is undone and the next try is a normal first try", status)
	}
	var replies int64
	db.Raw(`SELECT count(*) FROM payments.outbox WHERE payload->>'order_id' = ?`, orderID.String()).Scan(&replies)
	if replies != 0 {
		t.Fatalf("no reply may be sent for a retry, found %d", replies)
	}
}
