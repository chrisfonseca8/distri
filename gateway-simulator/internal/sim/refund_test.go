package sim

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// paidOrder returns a store with one captured payment of 25000 paise and its ids.
func paidOrder(t *testing.T, cfg Config, plan *Plan) (*Store, *manualClock, string, string) {
	t.Helper()
	store, clock := newTestStore(cfg)
	if plan == nil {
		plan = &Plan{}
	}
	plan.Attempts = []PlanAttempt{{At: dur("1s"), Outcome: "capture", CaptureAfter: ptr(dur("1s"))}}
	store.SetPlan("rcpt", *plan)
	o, _ := store.CreateOrder(newOrder("25000", "rcpt"))
	clock.Advance(2 * time.Second)
	clock.Advance(2 * time.Second)
	payments, _ := store.OrderPayments(o.ID)
	if len(payments) != 1 || payments[0].Status != "captured" {
		t.Fatalf("setup: payments = %+v", payments)
	}
	return store, clock, o.ID, payments[0].ID
}

func TestRefundOutcomesFollowTheScript(t *testing.T) {
	cases := []struct {
		name, outcome string
		wantFirst     string
		wantFinal     string
		wantRefunded  int64 // payment.amount_refunded after the final state
	}{
		{"immediately processed", "processed", "processed", "processed", 25000},
		{"pending then processed", "pending_processed", "pending", "processed", 25000},
		{"pending then failed", "pending_failed", "pending", "failed", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store, clock, _, paymentID := paidOrder(t, testConfig(), &Plan{Refunds: []RefundRule{{Outcome: c.outcome, After: ptr(dur("5s"))}}})
			refund, apiErr := store.CreateRefund(paymentID, RefundRequest{Receipt: "op-1", Notes: map[string]any{"operation_id": "op-1"}})
			if apiErr != nil || refund.Status != c.wantFirst {
				t.Fatalf("first state = %q (%v), want %q", refund.Status, apiErr, c.wantFirst)
			}
			if refund.Notes["operation_id"] != "op-1" || *refund.Receipt != "op-1" {
				t.Fatalf("correlation fields must be echoed: %+v", refund)
			}
			if p, _ := store.GetPayment(paymentID); p.AmountRefunded != 25000 && c.wantFirst == "pending" {
				t.Fatalf("a pending refund already counts in amount_refunded, got %d", p.AmountRefunded)
			}
			clock.Advance(3 * time.Second)
			if got, _ := store.GetRefund(refund.ID); got.Status != c.wantFirst {
				t.Fatalf("before the scripted time the refund is still %q, got %q", c.wantFirst, got.Status)
			}
			clock.Advance(3 * time.Second)
			got, _ := store.GetRefund(refund.ID)
			if got.Status != c.wantFinal {
				t.Fatalf("final = %q, want %q", got.Status, c.wantFinal)
			}
			p, _ := store.GetPayment(paymentID)
			if p.AmountRefunded != c.wantRefunded {
				t.Fatalf("amount_refunded = %d, want %d (a failed refund gives the money back)", p.AmountRefunded, c.wantRefunded)
			}
		})
	}
}

func TestRefundIdempotencyKeyAndDuplicates(t *testing.T) {
	t.Run("same key and request replays the original; a different request conflicts", func(t *testing.T) {
		store, _, _, paymentID := paidOrder(t, testConfig(), nil)
		first, apiErr := store.CreateRefund(paymentID, RefundRequest{Receipt: "r", IdempotencyKey: "cmp-key-0000001"})
		if apiErr != nil {
			t.Fatal(apiErr)
		}
		again, apiErr := store.CreateRefund(paymentID, RefundRequest{Receipt: "r", IdempotencyKey: "cmp-key-0000001"})
		if apiErr != nil || again.ID != first.ID || store.Stats().RefundCount != 1 {
			t.Fatalf("replay = %+v %v, refunds=%d, want the original and no second refund", again, apiErr, store.Stats().RefundCount)
		}
		if _, apiErr := store.CreateRefund(paymentID, RefundRequest{Receipt: "other", IdempotencyKey: "cmp-key-0000001"}); apiErr == nil || apiErr.Status != 409 {
			t.Fatalf("a different request with the same key must be 409, got %v", apiErr)
		}
		if _, apiErr := store.CreateRefund(paymentID, RefundRequest{IdempotencyKey: "short"}); apiErr == nil || apiErr.Status != 400 {
			t.Fatalf("a key under 10 characters must be 400, got %v", apiErr)
		}
	})
	t.Run("a forgotten key (injected) cannot over-refund: the full-refund cap still answers", func(t *testing.T) {
		cfg := testConfig()
		cfg.RefundKeyRetention = time.Second
		store, clock, _, paymentID := paidOrder(t, cfg, nil)
		store.CreateRefund(paymentID, RefundRequest{IdempotencyKey: "cmp-key-0000002"})
		clock.Advance(time.Minute) // the key is forgotten
		if _, apiErr := store.CreateRefund(paymentID, RefundRequest{IdempotencyKey: "cmp-key-0000002"}); apiErr == nil || apiErr.Description != "The payment has been fully refunded already." {
			t.Fatalf("err = %v, want 'fully refunded already'", apiErr)
		}
		if store.Stats().RefundCount != 1 {
			t.Fatalf("refunds = %d, want 1", store.Stats().RefundCount)
		}
	})
	t.Run("duplicate receipts are accepted by default (not made convenient) and capped by the amount", func(t *testing.T) {
		store, _, _, paymentID := paidOrder(t, testConfig(), nil)
		if _, apiErr := store.CreateRefund(paymentID, RefundRequest{Amount: "10000", Receipt: "same"}); apiErr != nil {
			t.Fatal(apiErr)
		}
		if _, apiErr := store.CreateRefund(paymentID, RefundRequest{Amount: "10000", Receipt: "same"}); apiErr != nil {
			t.Fatalf("a duplicate receipt must be accepted by default, got %v", apiErr)
		}
		if _, apiErr := store.CreateRefund(paymentID, RefundRequest{Amount: "10000", Receipt: "same"}); apiErr == nil {
			t.Fatal("the third 10000 exceeds the 25000 captured and must be refused")
		}
	})
}

func TestRefundResponseFaultsNeverUndoTheCommit(t *testing.T) {
	check := func(response string, committed bool, wantStatus int) {
		t.Run(response, func(t *testing.T) {
			store, _, _, paymentID := paidOrder(t, testConfig(), &Plan{Refunds: []RefundRule{{Outcome: "pending_processed", Response: response, ResponseDelay: ptr(dur("2s"))}}})
			_, behavior, apiErr := store.CreateRefundWithBehavior(paymentID, RefundRequest{Receipt: "x"})
			count := store.Stats().RefundCount
			switch response {
			case "reject", "error_before":
				if apiErr == nil || apiErr.Status != wantStatus || count != 0 {
					t.Fatalf("err=%v refunds=%d, want %d and nothing created", apiErr, count, wantStatus)
				}
			default:
				if apiErr != nil || count != 1 || !committed {
					t.Fatalf("err=%v refunds=%d: the refund must be committed", apiErr, count)
				}
				if response == "lost" && !behavior.Lost || response == "error_after" && !behavior.Error500 || response == "slow" && behavior.Delay != 2*time.Second {
					t.Fatalf("behavior = %+v", behavior)
				}
			}
		})
	}
	check("reject", false, 400)
	check("error_before", false, 500)
	check("lost", true, 0)
	check("slow", true, 0)
	check("error_after", true, 500)
}

func TestDelayedVisibilityHidesTheRefundFromLookupsButNotFromTheAmount(t *testing.T) {
	store, clock, _, paymentID := paidOrder(t, testConfig(), &Plan{Refunds: []RefundRule{{Outcome: "pending_processed", After: ptr(dur("20s")), VisibleAfter: ptr(dur("10s"))}}})
	refund, _ := store.CreateRefund(paymentID, RefundRequest{Receipt: "op"})
	if _, apiErr := store.GetRefund(refund.ID); apiErr == nil {
		t.Fatal("a refund inside its visibility delay must not be fetchable")
	}
	if list, _ := store.ListPaymentRefunds(paymentID); len(list) != 0 {
		t.Fatalf("list = %d, the refund must be hidden", len(list))
	}
	if all, _ := store.ListRefunds(0, 0, 0, 0); len(all) != 0 {
		t.Fatal("the global list must hide it too")
	}
	if p, _ := store.GetPayment(paymentID); p.AmountRefunded != 25000 {
		t.Fatalf("amount_refunded = %d: the refunded money is visible immediately, which is how an unseen refund is noticed", p.AmountRefunded)
	}
	clock.Advance(11 * time.Second)
	if got, apiErr := store.GetRefund(refund.ID); apiErr != nil || got.ID != refund.ID {
		t.Fatalf("after the delay the refund is visible: %v", apiErr)
	}
	if list, _ := store.ListPaymentRefunds(paymentID); len(list) != 1 {
		t.Fatalf("list = %d", len(list))
	}
}

func TestRefundEventsComeFromTheStoredRefund(t *testing.T) {
	store, clock := newTestStore(testConfig())
	notifier := &recordingNotifier{}
	store.SetNotifier(notifier)
	store.SetPlan("rcpt", Plan{
		Attempts: []PlanAttempt{{At: dur("1s"), Outcome: "capture", CaptureAfter: ptr(dur("1s"))}},
		Refunds:  []RefundRule{{Outcome: "pending_processed", After: ptr(dur("5s"))}},
	})
	o, _ := store.CreateOrder(newOrder("25000", "rcpt"))
	clock.Advance(2 * time.Second)
	clock.Advance(2 * time.Second)
	payments, _ := store.OrderPayments(o.ID)
	notifier.events = nil
	refund, _ := store.CreateRefund(payments[0].ID, RefundRequest{Receipt: "op-9", Notes: map[string]any{"operation_id": "op-9"}})
	clock.Advance(6 * time.Second)
	if got := names(notifier.events); len(got) != 2 || got[0] != "refund.created" || got[1] != "refund.processed" {
		t.Fatalf("events = %v", got)
	}
	var created, processed struct {
		Contains []string `json:"contains"`
		Payload  struct {
			Refund struct {
				Entity RefundView `json:"entity"`
			} `json:"refund"`
			Payment struct {
				Entity PaymentView `json:"entity"`
			} `json:"payment"`
		} `json:"payload"`
	}
	json.Unmarshal(notifier.events[0].Body, &created)
	json.Unmarshal(notifier.events[1].Body, &processed)
	if created.Payload.Refund.Entity.Status != "pending" || processed.Payload.Refund.Entity.Status != "processed" {
		t.Fatalf("snapshots: created=%s processed=%s (each event keeps the state at its creation)", created.Payload.Refund.Entity.Status, processed.Payload.Refund.Entity.Status)
	}
	if created.Payload.Refund.Entity.ID != refund.ID || created.Payload.Refund.Entity.Notes["operation_id"] != "op-9" || *created.Payload.Refund.Entity.Receipt != "op-9" {
		t.Fatalf("correlation fields missing from the event: %+v", created.Payload.Refund.Entity)
	}
	if len(created.Contains) != 2 || created.Payload.Payment.Entity.ID != payments[0].ID {
		t.Fatalf("a refund event carries its payment: %+v", created.Contains)
	}
}

func TestRefundWebhookDeliveryCanBeDelayedOmittedAndReordered(t *testing.T) {
	var mu sync.Mutex
	var order []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var envelope struct{ Event string }
		json.Unmarshal(body, &envelope)
		mu.Lock()
		order = append(order, envelope.Event)
		mu.Unlock()
	}))
	defer server.Close()
	webhooks := startWebhooks(t, webhookConfig(server.URL))
	store, clock := newTestStore(testConfig())
	store.SetNotifier(webhooks)
	store.SetPlan("rcpt", Plan{
		Attempts: []PlanAttempt{{At: dur("1s"), Outcome: "capture", CaptureAfter: ptr(dur("1s"))}},
		Refunds:  []RefundRule{{Outcome: "processed"}},
		Delivery: map[string]DeliveryRule{
			"payment.authorized": {Omit: true}, "payment.captured": {Omit: true}, "order.paid": {Omit: true},
			"refund.created":   {Delay: ptr(dur("300ms"))}, // created is delivered AFTER processed
			"refund.processed": {Delay: ptr(dur("0s")), Duplicates: []Duration{dur("50ms")}},
		},
	})
	o, _ := store.CreateOrder(newOrder("25000", "rcpt"))
	clock.Advance(2 * time.Second)
	clock.Advance(2 * time.Second)
	payments, _ := store.OrderPayments(o.ID)
	store.CreateRefund(payments[0].ID, RefundRequest{Receipt: "x"})
	waitFor(t, "refund events", func() bool { mu.Lock(); defer mu.Unlock(); return len(order) == 3 })
	mu.Lock()
	defer mu.Unlock()
	if order[0] != "refund.processed" || order[1] != "refund.processed" || order[2] != "refund.created" {
		t.Fatalf("delivery order = %v, want processed (and its duplicate) before the older created", order)
	}
}

func TestRefundResponseFaultsOverHTTP(t *testing.T) {
	cfg := testConfig()
	scheduler := NewScheduler()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go scheduler.Run(ctx)
	store := NewStore(cfg, scheduler.At)
	plan := Plan{
		Attempts: []PlanAttempt{{At: dur("0s"), Outcome: "capture", CaptureAfter: ptr(dur("0s"))}},
		Refunds: []RefundRule{
			{Outcome: "pending_processed", After: ptr(dur("1s")), Response: "lost"},
			{Outcome: "pending_processed", After: ptr(dur("1s")), Response: "slow", ResponseDelay: ptr(dur("600ms"))},
		},
	}
	store.SetPlan("rcpt", plan)
	order, _ := store.CreateOrder(newOrder("25000", "rcpt"))
	var paymentID string
	waitFor(t, "capture", func() bool {
		payments, _ := store.OrderPayments(order.ID)
		if len(payments) == 1 && payments[0].Status == "captured" {
			paymentID = payments[0].ID
			return true
		}
		return false
	})
	api := httptest.NewServer(NewServer(cfg, store, scheduler, nil).Handler())
	defer api.Close()
	post := func(client *http.Client, key string, amount string) (*http.Response, error) {
		req, _ := http.NewRequest(http.MethodPost, api.URL+"/v1/payments/"+paymentID+"/refund", io.NopCloser(stringsReader(`{"amount":`+amount+`,"receipt":"op"}`)))
		req.SetBasicAuth(cfg.KeyID, cfg.KeySecret)
		req.Header.Set("X-Refund-Idempotency", key)
		return client.Do(req)
	}

	// lost: the refund is committed, the caller gets no response at all
	if _, err := post(&http.Client{Timeout: 2 * time.Second}, "cmp-lost-000001", "10000"); err == nil {
		t.Fatal("a lost response must surface as a transport error")
	}
	if store.Stats().RefundCount != 1 {
		t.Fatalf("refunds = %d, the committed refund must exist", store.Stats().RefundCount)
	}
	// slow: the client times out while the committed refund continues
	if _, err := post(&http.Client{Timeout: 100 * time.Millisecond}, "cmp-slow-000001", "10000"); err == nil {
		t.Fatal("the client must time out")
	}
	if store.Stats().RefundCount != 2 {
		t.Fatalf("refunds = %d, a client timeout must not undo the commit", store.Stats().RefundCount)
	}
	// while the slow response is still pending, the same key answers 409
	resp, err := post(&http.Client{Timeout: 2 * time.Second}, "cmp-slow-000001", "10000")
	if err != nil || resp.StatusCode != http.StatusConflict {
		t.Fatalf("a retry during the delayed response = %v / %v, want 409 in progress", resp, err)
	}
	time.Sleep(700 * time.Millisecond) // the delayed response completes
	resp, err = post(&http.Client{Timeout: 2 * time.Second}, "cmp-slow-000001", "10000")
	if err != nil || resp.StatusCode != http.StatusOK || store.Stats().RefundCount != 2 {
		t.Fatalf("a retry after that replays the original: %v %v refunds=%d", resp, err, store.Stats().RefundCount)
	}
}
