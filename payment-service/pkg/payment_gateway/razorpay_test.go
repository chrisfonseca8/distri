package paymentgateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
	"uuid"
)

const testOrderID = "70000000-0000-4000-8000-000000000001"

func newTestRazorpay(t *testing.T, handler http.Handler) *Razorpay {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	gateway, err := NewRazorpay(RazorpayConfig{
		KeyID:      "rzp_test_key",
		KeySecret:  "secret",
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("new razorpay: %v", err)
	}
	return gateway
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func TestNewRazorpayValidatesCredentials(t *testing.T) {
	for name, config := range map[string]RazorpayConfig{
		"missing key id": {KeySecret: "s"},
		"missing secret": {KeyID: "rzp_test_a"},
		"live key":       {KeyID: "rzp_live_a", KeySecret: "s"},
	} {
		if _, err := NewRazorpay(config); err == nil {
			t.Errorf("%s: NewRazorpay accepted the configuration", name)
		}
	}
	if _, err := NewRazorpay(RazorpayConfig{KeyID: "rzp_live_a", KeySecret: "s", AllowLive: true}); err != nil {
		t.Errorf("live key with AllowLive: %v", err)
	}
}

func TestCreateChargeCreatesAnOrderThatAwaitsPayment(t *testing.T) {
	var request struct {
		Amount   int64
		Currency string
		Receipt  string
	}
	var user, password string
	gateway := newTestRazorpay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/orders" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		user, password, _ = r.BasicAuth()
		_ = json.NewDecoder(r.Body).Decode(&request)
		writeJSON(w, 200, `{"id":"order_ABC","amount":149900,"receipt":"`+testOrderID+`","status":"created"}`)
	}))

	ref, err := gateway.CreateCharge(context.Background(), uuid.MustParse(testOrderID), 149900)
	var awaiting *ErrAwaitingPayment
	if !errors.As(err, &awaiting) || awaiting.Ref != "order_ABC" || ref != "" {
		t.Fatalf("ref=%q err=%v, want ErrAwaitingPayment{order_ABC}", ref, err)
	}
	if user != "rzp_test_key" || password != "secret" {
		t.Errorf("basic auth = %q/%q", user, password)
	}
	if request.Amount != 149900 || request.Currency != "INR" || request.Receipt != testOrderID {
		t.Errorf("request body = %+v", request)
	}
}

func TestCreateChargeClassifiesFailures(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		body         string
		wantRejected bool
	}{
		{"validation error is a confirmed rejection", 400, `{"error":{"code":"BAD_REQUEST_ERROR","description":"The amount must be atleast INR 1.00"}}`, true},
		{"server error is ambiguous", 500, `{"error":{"description":"boom"}}`, false},
		{"rate limit is ambiguous", 429, `{"error":{"description":"slow down"}}`, false},
		{"bad credentials are ambiguous, not a rejection", 401, `{"error":{"description":"Authentication failed"}}`, false},
		{"bad credentials reported as 400 are ambiguous", 400, `{"error":{"code":"BAD_REQUEST_ERROR","description":"Authentication failed"}}`, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gateway := newTestRazorpay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, test.status, test.body)
			}))
			_, err := gateway.CreateCharge(context.Background(), uuid.MustParse(testOrderID), 100)
			var rejected *ErrRejected
			var awaiting *ErrAwaitingPayment
			if err == nil || errors.As(err, &awaiting) {
				t.Fatalf("err = %v, want a failure", err)
			}
			if errors.As(err, &rejected) != test.wantRejected {
				t.Fatalf("rejected = %v, want %v (err %v)", !test.wantRejected, test.wantRejected, err)
			}
		})
	}
}

func TestCreateChargeTimeoutIsAmbiguous(t *testing.T) {
	gateway := newTestRazorpay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := gateway.CreateCharge(ctx, uuid.MustParse(testOrderID), 100)
	var rejected *ErrRejected
	var awaiting *ErrAwaitingPayment
	if err == nil || errors.As(err, &rejected) || errors.As(err, &awaiting) {
		t.Fatalf("err = %v, want a plain ambiguous error", err)
	}
}

// razorpayStub serves the duplicate-receipt recovery path: create is rejected as
// a duplicate, then the order is found by receipt.
func razorpayStub(orderAmount int64, payments string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/orders":
			writeJSON(w, 400, `{"error":{"code":"BAD_REQUEST_ERROR","description":"Duplicate request. This request has already been processed."}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/orders":
			if r.URL.Query().Get("receipt") != testOrderID {
				writeJSON(w, 200, `{"entity":"collection","count":0,"items":[]}`)
				return
			}
			writeJSON(w, 200, `{"entity":"collection","count":1,"items":[{"id":"order_EXIST","amount":`+itoa(orderAmount)+`,"receipt":"`+testOrderID+`","status":"created"}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/orders/order_EXIST/payments":
			writeJSON(w, 200, payments)
		default:
			http.NotFound(w, r)
		}
	})
}

func itoa(value int64) string {
	return strconv.FormatInt(value, 10)
}

func TestCreateChargeDuplicateReceiptReturnsTheExistingOrder(t *testing.T) {
	gateway := newTestRazorpay(t, razorpayStub(149900, `{"items":[]}`))
	_, err := gateway.CreateCharge(context.Background(), uuid.MustParse(testOrderID), 149900)
	var awaiting *ErrAwaitingPayment
	if !errors.As(err, &awaiting) || awaiting.Ref != "order_EXIST" {
		t.Fatalf("err = %v, want ErrAwaitingPayment{order_EXIST}", err)
	}
}

func TestCreateChargeDuplicateReceiptWithAnotherAmountIsRejected(t *testing.T) {
	gateway := newTestRazorpay(t, razorpayStub(999, `{"items":[]}`))
	_, err := gateway.CreateCharge(context.Background(), uuid.MustParse(testOrderID), 149900)
	var rejected *ErrRejected
	if !errors.As(err, &rejected) || rejected.Code != "AMOUNT_MISMATCH" {
		t.Fatalf("err = %v, want ErrRejected AMOUNT_MISMATCH", err)
	}
}

func TestLookupOrder(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		gateway := newTestRazorpay(t, razorpayStub(100, `{"items":[]}`))
		_, err := gateway.LookupOrder(context.Background(), uuid.MustParse("70000000-0000-4000-8000-0000000000ff"))
		if !errors.Is(err, ErrOrderNotFound) {
			t.Fatalf("err = %v, want ErrOrderNotFound", err)
		}
	})
	t.Run("unpaid order with a failed attempt", func(t *testing.T) {
		gateway := newTestRazorpay(t, razorpayStub(100, `{"items":[{"id":"pay_1","amount":100,"status":"failed","error_code":"BAD_REQUEST_ERROR"}]}`))
		state, err := gateway.LookupOrder(context.Background(), uuid.MustParse(testOrderID))
		if err != nil || state.CapturedPaymentID != "" || state.LastFailureCode != "BAD_REQUEST_ERROR" || state.GatewayOrderID != "order_EXIST" {
			t.Fatalf("state=%+v err=%v", state, err)
		}
	})
	t.Run("captured payment after a failed one", func(t *testing.T) {
		gateway := newTestRazorpay(t, razorpayStub(100, `{"items":[{"id":"pay_1","amount":100,"status":"failed"},{"id":"pay_2","amount":100,"status":"captured"}]}`))
		state, err := gateway.LookupOrder(context.Background(), uuid.MustParse(testOrderID))
		if err != nil || state.CapturedPaymentID != "pay_2" || state.CapturedAmountPaise != 100 {
			t.Fatalf("state=%+v err=%v", state, err)
		}
	})
	t.Run("authorized but uncaptured does not count as paid", func(t *testing.T) {
		gateway := newTestRazorpay(t, razorpayStub(100, `{"items":[{"id":"pay_1","amount":100,"status":"authorized"}]}`))
		state, err := gateway.LookupOrder(context.Background(), uuid.MustParse(testOrderID))
		if err != nil || state.CapturedPaymentID != "" {
			t.Fatalf("state=%+v err=%v", state, err)
		}
	})
}

func TestRefund(t *testing.T) {
	var body map[string]any
	var header string
	handler := func(status int, response string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/v1/payments/pay_2/refund" {
				t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			}
			header = r.Header.Get("X-Refund-Idempotency")
			body = map[string]any{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			writeJSON(w, status, response)
		})
	}
	request := RefundRequest{
		PaymentID: "pay_2", AmountPaise: 25000, Receipt: "op-1",
		Notes: map[string]string{"operation_id": "op-1"}, IdempotencyKey: "cmp-op-1-long-enough",
	}

	t.Run("sends the documented idempotency header, receipt, notes and amount", func(t *testing.T) {
		gateway := newTestRazorpay(t, handler(200, `{"id":"rfnd_1","status":"pending","payment_id":"pay_2","amount":25000,"receipt":"op-1","notes":{"operation_id":"op-1"}}`))
		result, err := gateway.Refund(context.Background(), request)
		if err != nil || result.ID != "rfnd_1" || result.Status != "pending" || result.Receipt != "op-1" || result.Notes["operation_id"] != "op-1" {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if header != "cmp-op-1-long-enough" || body["receipt"] != "op-1" || body["amount"] != float64(25000) {
			t.Fatalf("header=%q body=%v", header, body)
		}
	})
	t.Run("no key means no header, and empty notes as an array are accepted", func(t *testing.T) {
		gateway := newTestRazorpay(t, handler(200, `{"id":"rfnd_2","status":"processed","notes":[]}`))
		request := request
		request.IdempotencyKey = ""
		result, err := gateway.Refund(context.Background(), request)
		if err != nil || result.Status != "processed" || header != "" || len(result.Notes) != 0 {
			t.Fatalf("result=%+v err=%v header=%q", result, err, header)
		}
	})
	t.Run("already fully refunded is reconciled, not treated as a failure", func(t *testing.T) {
		gateway := newTestRazorpay(t, handler(400, `{"error":{"code":"BAD_REQUEST_ERROR","description":"The payment has been fully refunded already."}}`))
		if _, err := gateway.Refund(context.Background(), request); !errors.Is(err, ErrFullyRefunded) {
			t.Fatalf("err = %v, want ErrFullyRefunded", err)
		}
	})
	t.Run("409 is an idempotency conflict whose outcome is unknown", func(t *testing.T) {
		gateway := newTestRazorpay(t, handler(409, `{"error":{"code":"BAD_REQUEST_ERROR","description":"Another request with the same idempotency key is still in progress."}}`))
		var conflict *ErrRefundConflict
		if _, err := gateway.Refund(context.Background(), request); !errors.As(err, &conflict) {
			t.Fatalf("err = %v, want ErrRefundConflict", err)
		}
	})
	t.Run("other 400 is a definitive rejection; 500, 429 and timeouts are ambiguous", func(t *testing.T) {
		var rejected *ErrRejected
		var conflict *ErrRefundConflict
		definitive := newTestRazorpay(t, handler(400, `{"error":{"code":"BAD_REQUEST_ERROR","description":"The payment status should be captured for action to be taken."}}`))
		if _, err := definitive.Refund(context.Background(), request); !errors.As(err, &rejected) {
			t.Fatalf("err = %v, want ErrRejected", err)
		}
		for _, status := range []int{500, 502, 429} {
			ambiguous := newTestRazorpay(t, handler(status, `{}`))
			_, err := ambiguous.Refund(context.Background(), request)
			if err == nil || errors.As(err, &rejected) || errors.As(err, &conflict) || errors.Is(err, ErrFullyRefunded) {
				t.Fatalf("HTTP %d: err = %v, want a plain ambiguous error", status, err)
			}
		}
		slow := newTestRazorpay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
		}))
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if _, err := slow.Refund(ctx, request); err == nil || errors.As(err, &rejected) {
			t.Fatalf("a timeout must stay ambiguous, got %v", err)
		}
	})
}

func TestRefundFetchListAndPaymentLookup(t *testing.T) {
	gateway := newTestRazorpay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/refunds/rfnd_1":
			writeJSON(w, 200, `{"id":"rfnd_1","status":"processed","payment_id":"pay_2","amount":25000,"receipt":null,"notes":[]}`)
		case "/v1/refunds/rfnd_missing":
			writeJSON(w, 400, `{"error":{"code":"BAD_REQUEST_ERROR","description":"The id provided does not exist"}}`)
		case "/v1/payments/pay_2/refunds":
			if r.URL.Query().Get("count") != "100" {
				t.Errorf("list query = %v", r.URL.RawQuery)
			}
			writeJSON(w, 200, `{"entity":"collection","count":2,"items":[{"id":"rfnd_1","status":"pending","receipt":"op-1","notes":{"operation_id":"op-1"}},{"id":"rfnd_9","status":"processed","notes":[]}]}`)
		case "/v1/payments/pay_2":
			writeJSON(w, 200, `{"id":"pay_2","order_id":"order_1","status":"captured","currency":"INR","amount":25000,"amount_refunded":25000}`)
		case "/v1/payments/pay_missing", "/v1/payments/pay_missing/refunds":
			writeJSON(w, 400, `{"error":{"code":"BAD_REQUEST_ERROR","description":"The id provided does not exist"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	ctx := context.Background()

	refund, err := gateway.FetchRefund(ctx, "rfnd_1")
	if err != nil || refund.Status != "processed" || refund.PaymentID != "pay_2" || refund.Receipt != "" {
		t.Fatalf("FetchRefund = %+v %v", refund, err)
	}
	if _, err := gateway.FetchRefund(ctx, "rfnd_missing"); !errors.Is(err, ErrRefundNotFound) {
		t.Fatalf("an unknown refund id must be ErrRefundNotFound, got %v", err)
	}
	list, err := gateway.ListPaymentRefunds(ctx, "pay_2")
	if err != nil || len(list) != 2 || list[0].Receipt != "op-1" || list[0].Notes["operation_id"] != "op-1" || list[1].Receipt != "" {
		t.Fatalf("ListPaymentRefunds = %+v %v", list, err)
	}
	if _, err := gateway.ListPaymentRefunds(ctx, "pay_missing"); !errors.Is(err, ErrPaymentNotFound) {
		t.Fatalf("an unknown payment must be ErrPaymentNotFound, got %v", err)
	}
	info, err := gateway.FetchPayment(ctx, "pay_2")
	if err != nil || info.Status != "captured" || info.AmountPaise != 25000 || info.AmountRefundedPaise != 25000 || info.OrderID != "order_1" {
		t.Fatalf("FetchPayment = %+v %v", info, err)
	}
	if _, err := gateway.FetchPayment(ctx, "pay_missing"); !errors.Is(err, ErrPaymentNotFound) {
		t.Fatalf("an unknown payment must be ErrPaymentNotFound, got %v", err)
	}
}

func TestLookupOrderToleratesSeveralOrdersWithOneReceipt(t *testing.T) {
	gateway := newTestRazorpay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/orders":
			// newest first, as Razorpay lists them
			writeJSON(w, 200, `{"items":[{"id":"order_NEW","amount":100},{"id":"order_OLD","amount":100}]}`)
		case "/v1/orders/order_NEW/payments":
			writeJSON(w, 200, `{"items":[{"id":"pay_9","amount":100,"status":"captured"}]}`)
		case "/v1/orders/order_OLD/payments":
			writeJSON(w, 200, `{"items":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	state, err := gateway.LookupOrder(context.Background(), uuid.MustParse(testOrderID))
	if err != nil || state.CapturedPaymentID != "pay_9" || state.GatewayOrderID != "order_NEW" {
		t.Fatalf("a capture on any of the orders must be found: state=%+v err=%v", state, err)
	}

	unpaid := newTestRazorpay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/orders" {
			writeJSON(w, 200, `{"items":[{"id":"order_NEW","amount":100},{"id":"order_OLD","amount":100}]}`)
			return
		}
		writeJSON(w, 200, `{"items":[]}`)
	}))
	state, err = unpaid.LookupOrder(context.Background(), uuid.MustParse(testOrderID))
	if err != nil || state.CapturedPaymentID != "" || state.GatewayOrderID != "order_OLD" {
		t.Fatalf("without a capture the oldest order is reported: state=%+v err=%v", state, err)
	}
}

func TestGatewayNameSeparatesSimulatedFromRealEndpoints(t *testing.T) {
	t.Setenv("GATEWAY_PROVIDER", "razorpay")
	t.Setenv("RAZORPAY_KEY_ID", "rzp_test_x")
	t.Setenv("RAZORPAY_KEY_SECRET", "s")

	t.Setenv("RAZORPAY_BASE_URL", "")
	if _, name, err := NewFromEnv(); err != nil || name != GatewayNameRazorpay {
		t.Fatalf("default endpoint: name=%q err=%v, want %q", name, err, GatewayNameRazorpay)
	}
	t.Setenv("RAZORPAY_BASE_URL", "https://api.razorpay.com/")
	if _, name, _ := NewFromEnv(); name != GatewayNameRazorpay {
		t.Fatalf("the real endpoint (trailing slash) must keep the real label, got %q", name)
	}
	t.Setenv("RAZORPAY_BASE_URL", "http://gateway-simulator:9090")
	if _, name, err := NewFromEnv(); err != nil || name != GatewayNameRazorpaySim {
		t.Fatalf("simulator endpoint: name=%q err=%v, want %q", name, err, GatewayNameRazorpaySim)
	}
}
