package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStreamTerminalReadsCompletedEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/checkouts/order-1/events" || r.Header.Get("Accept") != "text/event-stream" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "id: 1\nevent: checkout.status\ndata: {\"status\":\"RUNNING\"}\n\n")
		fmt.Fprint(w, "id: 2\nevent: checkout.status\ndata: {\"status\":\"COMPLETED\"}\n\n")
	}))
	defer server.Close()

	client := &loadClient{checkoutURL: server.URL, http: server.Client(), metrics: newCollector()}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, terminal := client.streamTerminal(ctx, "order-1")
	if !terminal || result.Status != "PAID" || result.ID != "order-1" {
		t.Fatalf("unexpected SSE result: %#v terminal=%v", result, terminal)
	}
	metrics := client.metrics.snapshot(time.Second)
	if metrics.CompletionObservation.SSEAttempts != 1 || metrics.CompletionObservation.SSETerminalEvents != 1 {
		t.Fatalf("unexpected SSE observation metrics: %#v", metrics.CompletionObservation)
	}
}
