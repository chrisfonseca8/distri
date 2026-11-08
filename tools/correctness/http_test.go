package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitPaymentContinuesThroughAwaitingPayment(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		status := "AWAITING_PAYMENT"
		if requests.Add(1) >= 2 {
			status = "SUCCEEDED"
		}
		_ = json.NewEncoder(w).Encode(paymentResult{ID: "payment-1", Status: status})
	}))
	defer server.Close()

	p := &phaseContext{suite: &suite{
		ctx:        context.Background(),
		client:     server.Client(),
		paymentURL: server.URL,
	}}
	result := p.waitPayment("payment-1", time.Second)
	if result.Status != "SUCCEEDED" {
		t.Fatalf("status = %q, want SUCCEEDED", result.Status)
	}
	if requests.Load() < 2 {
		t.Fatalf("requests = %d, want at least 2", requests.Load())
	}
}
