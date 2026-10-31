package controllers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/services/checkout"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/statusstream"
)

type sseCheckoutService struct {
	mu      sync.Mutex
	results []checkout.CheckoutStatusResult
	calls   int
}

func (s *sseCheckoutService) CreateCheckout(context.Context, checkout.CreateCheckoutInput) (checkout.Response, error) {
	return checkout.Response{}, nil
}

func (s *sseCheckoutService) GetCheckout(context.Context, uuid.UUID) (checkout.GetCheckoutResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return checkout.GetCheckoutResult{}, nil
}

func (s *sseCheckoutService) GetCheckoutStatus(context.Context, uuid.UUID) (checkout.CheckoutStatusResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.calls
	if index >= len(s.results) {
		index = len(s.results) - 1
	}
	s.calls++
	return s.results[index], nil
}

func TestStreamCheckoutSendsSnapshotThenCommittedChange(t *testing.T) {
	orderID := uuid.New()
	now := time.Now().UTC()
	service := &sseCheckoutService{results: []checkout.CheckoutStatusResult{
		{OrderID: orderID, Status: "RUNNING", CurrentStep: "TAKE_PAYMENT", Version: 2, UpdatedAt: now},
		{OrderID: orderID, Status: "COMPLETED", CurrentStep: "CONFIRM_STOCK", Version: 3, UpdatedAt: now.Add(time.Second)},
	}}
	hub := statusstream.NewHub(10)
	controller := NewCheckoutController(service, hub)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/checkouts/{orderID}/events", controller.StreamCheckout)
	server := httptest.NewServer(mux)
	defer server.Close()

	body := make(chan string, 1)
	errors := make(chan error, 1)
	go func() {
		response, err := http.Get(server.URL + "/v1/checkouts/" + orderID.String() + "/events")
		if err != nil {
			errors <- err
			return
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			errors <- err
			return
		}
		body <- string(raw)
	}()

	deadline := time.Now().Add(time.Second)
	for hub.Subscribers() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if hub.Subscribers() != 1 {
		t.Fatal("SSE subscriber did not register")
	}
	hub.Notify(orderID.String())

	select {
	case err := <-errors:
		t.Fatal(err)
	case raw := <-body:
		if !strings.Contains(raw, "id: 2\nevent: checkout.status") || !strings.Contains(raw, `"status":"RUNNING"`) {
			t.Fatalf("missing initial event in %q", raw)
		}
		if !strings.Contains(raw, "id: 3\nevent: checkout.status") || !strings.Contains(raw, `"status":"COMPLETED"`) {
			t.Fatalf("missing terminal event in %q", raw)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SSE stream to close")
	}
	if hub.Subscribers() != 0 {
		t.Fatalf("subscriber leaked after terminal event: %d", hub.Subscribers())
	}
}
