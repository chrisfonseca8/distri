package sim

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type received struct {
	body      []byte
	signature string
	eventID   string
}

func webhookConfig(url string) Config {
	cfg := testConfig()
	cfg.WebhookURL = url
	cfg.WebhookSecret = "whsec_sim"
	cfg.WebhookAccountID = "acc_test"
	cfg.WebhookWorkers = 2
	cfg.WebhookTimeout = 200 * time.Millisecond
	cfg.WebhookSchedule = []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}
	cfg.WebhookMaxAge = time.Hour
	cfg.WebhookDelayMedian = time.Millisecond
	return cfg
}

func startWebhooks(t *testing.T, cfg Config) *Webhooks {
	t.Helper()
	w := NewWebhooks(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go w.Run(ctx)
	return w
}

func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func sampleEvent(name, key string) Event {
	now := time.Now() // the max-age check runs against the event's creation time
	body := buildEvent("acc_test", name, now,
		&PaymentView{ID: "pay_1", Entity: "payment", Status: "captured", OrderID: "order_1"}, nil)
	return Event{Name: name, Key: key, PaymentID: "pay_1", OrderID: "order_1", CreatedAt: now, Body: body}
}

func TestDeliverySignsTheExactBytesAndSendsTheEventID(t *testing.T) {
	var mu sync.Mutex
	var got []received
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, received{body, r.Header.Get("X-Razorpay-Signature"), r.Header.Get("x-razorpay-event-id")})
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	webhooks := startWebhooks(t, webhookConfig(server.URL))
	event := sampleEvent("payment.captured", "pay_1")
	webhooks.Notify(event)
	waitFor(t, "delivery", func() bool { return webhooks.Stats().Delivered == 1 })

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("deliveries = %d", len(got))
	}
	if string(got[0].body) != string(event.Body) {
		t.Fatal("the delivered bytes differ from the stored snapshot")
	}
	if got[0].signature != sign("whsec_sim", got[0].body) || got[0].signature == sign("another-secret", got[0].body) {
		t.Fatal("the signature must be HMAC-SHA256 of the exact received bytes under the webhook secret")
	}
	if got[0].eventID == "" || got[0].eventID != entityID("evt_", 1, "event", "payment.captured", "pay_1") && got[0].eventID[:4] != "evt_" {
		t.Fatalf("event id = %q", got[0].eventID)
	}
}

func TestRetriesKeepTheEventIdentityAndPayload(t *testing.T) {
	var calls atomic.Int64
	var mu sync.Mutex
	var seen []received
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, received{body, r.Header.Get("X-Razorpay-Signature"), r.Header.Get("x-razorpay-event-id")})
		mu.Unlock()
		if calls.Add(1) <= 2 {
			w.WriteHeader(http.StatusInternalServerError) // non-2xx: not acknowledged
			return
		}
		w.WriteHeader(http.StatusNoContent) // any 2xx acknowledges
	}))
	defer server.Close()
	webhooks := startWebhooks(t, webhookConfig(server.URL))
	webhooks.Notify(sampleEvent("payment.captured", "pay_1"))
	waitFor(t, "acknowledgement after retries", func() bool { return webhooks.Stats().Delivered == 1 })

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 3 {
		t.Fatalf("attempts = %d, want 3 (two failures then an acknowledgement)", len(seen))
	}
	for _, attempt := range seen[1:] {
		if attempt.eventID != seen[0].eventID || string(attempt.body) != string(seen[0].body) || attempt.signature != seen[0].signature {
			t.Fatal("a retry must keep the original event id, payload bytes and signature")
		}
	}
	if stats := webhooks.Stats(); stats.FailedAttempts != 2 || stats.Pending != 0 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestAcknowledgementTimeoutIsRetried(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			time.Sleep(600 * time.Millisecond) // longer than the 200 ms test timeout
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	webhooks := startWebhooks(t, webhookConfig(server.URL))
	webhooks.Notify(sampleEvent("payment.captured", "pay_1"))
	waitFor(t, "acknowledgement after a timed-out attempt", func() bool { return webhooks.Stats().Delivered == 1 })
	if stats := webhooks.Stats(); stats.FailedAttempts < 1 {
		t.Fatalf("the timed-out attempt must count as a failure: %+v", stats)
	}
}

func TestUnreachableReceiverIsRetriedUntilTheMaxAge(t *testing.T) {
	cfg := webhookConfig("http://127.0.0.1:1") // nothing listens here
	cfg.WebhookMaxAge = 60 * time.Millisecond
	webhooks := startWebhooks(t, cfg)
	webhooks.Notify(sampleEvent("payment.captured", "pay_1"))
	waitFor(t, "expiry", func() bool { return webhooks.Stats().Expired == 1 })
	deliveries := webhooks.List(10, "")
	if len(deliveries) != 1 || deliveries[0].State != deliveryExpired || deliveries[0].Attempts < 2 {
		t.Fatalf("deliveries = %+v", deliveries)
	}
}

func TestDeliveryConcurrencyIsBounded(t *testing.T) {
	var current, peak atomic.Int64
	release := make(chan struct{})
	var releaseOnce sync.Once
	open := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(open) // never leave handlers blocked if the test fails early
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := current.Add(1)
		for {
			old := peak.Load()
			if now <= old || peak.CompareAndSwap(old, now) {
				break
			}
		}
		<-release
		current.Add(-1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	defer open() // runs before server.Close: Close waits for blocked handlers
	cfg := webhookConfig(server.URL)
	cfg.WebhookTimeout = 5 * time.Second
	webhooks := startWebhooks(t, cfg)
	for i := 0; i < 10; i++ {
		webhooks.Notify(sampleEvent("payment.captured", "pay_"+itoa(i)))
	}
	waitFor(t, "workers to be busy", func() bool { return webhooks.Stats().InFlight == 2 })
	time.Sleep(50 * time.Millisecond)
	if stats := webhooks.Stats(); stats.InFlight != 2 || stats.Pending < 7 {
		t.Fatalf("stats = %+v, want 2 in flight and the rest waiting", stats)
	}
	open()
	waitFor(t, "all events delivered", func() bool { return webhooks.Stats().Delivered == 10 })
	if peak.Load() > 2 {
		t.Fatalf("peak concurrent requests = %d, want at most the 2 workers", peak.Load())
	}
}

func TestRedeliverySendsTheOriginalBytesAndEventID(t *testing.T) {
	var mu sync.Mutex
	var seen []received
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, received{body, r.Header.Get("X-Razorpay-Signature"), r.Header.Get("x-razorpay-event-id")})
		mu.Unlock()
	}))
	defer server.Close()
	webhooks := startWebhooks(t, webhookConfig(server.URL))
	webhooks.Notify(sampleEvent("payment.authorized", "pay_1"))
	waitFor(t, "first delivery", func() bool { return webhooks.Stats().Delivered == 1 })
	id := webhooks.List(1, "")[0].ID
	if !webhooks.Redeliver(id) || webhooks.Redeliver("evt_unknown") {
		t.Fatal("redeliver must accept a known event and refuse an unknown one")
	}
	waitFor(t, "redelivery", func() bool { return webhooks.Stats().Delivered == 2 })
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen[0].eventID != seen[1].eventID || string(seen[0].body) != string(seen[1].body) {
		t.Fatal("a redelivery must be byte-identical with the same event id")
	}
	if webhooks.Stats().Redeliveries != 1 {
		t.Fatalf("stats = %+v", webhooks.Stats())
	}
}

// recordingNotifier collects events synchronously, in creation order.
type recordingNotifier struct {
	mu     sync.Mutex
	events []Event
}

func (n *recordingNotifier) Notify(e Event) {
	n.mu.Lock()
	n.events = append(n.events, e)
	n.mu.Unlock()
}

func TestEventsComeFromCommittedTransitionsAndKeepTheirSnapshot(t *testing.T) {
	store, clock := newTestStore(testConfig())
	notifier := &recordingNotifier{}
	store.SetNotifier(notifier)
	o, _ := store.CreateOrder(newOrder("25000", "rcpt-ev"))
	clock.Advance(time.Minute) // attempt: payment.authorized
	if len(notifier.events) != 1 || notifier.events[0].Name != "payment.authorized" {
		t.Fatalf("events after the attempt = %+v", names(notifier.events))
	}
	clock.Advance(time.Minute) // auto-capture: payment.captured then order.paid
	if got := names(notifier.events); len(got) != 3 || got[1] != "payment.captured" || got[2] != "order.paid" {
		t.Fatalf("events = %v", got)
	}

	// The authorized event keeps the state at its creation even though the payment
	// is captured by now.
	var authorized, captured, orderPaid eventEnvelopeForTest
	json.Unmarshal(notifier.events[0].Body, &authorized)
	json.Unmarshal(notifier.events[1].Body, &captured)
	json.Unmarshal(notifier.events[2].Body, &orderPaid)
	if p := authorized.Payload.Payment.Entity; p.Status != "authorized" || p.Captured {
		t.Fatalf("authorized snapshot = %+v", p)
	}
	if p := captured.Payload.Payment.Entity; p.Status != "captured" || !p.Captured || p.OrderID != o.ID {
		t.Fatalf("captured snapshot = %+v", p)
	}
	if orderPaid.Payload.Order == nil || orderPaid.Payload.Order.Entity.Status != "paid" || len(orderPaid.Contains) != 2 {
		t.Fatalf("order.paid payload = %+v", orderPaid)
	}
	if authorized.Entity != "event" || authorized.AccountID != "" && authorized.AccountID != "acc_test" {
		t.Fatalf("envelope = %+v", authorized)
	}
	// The same state read through the API agrees with the events.
	if payment, _ := store.GetPayment(captured.Payload.Payment.Entity.ID); payment.Status != "captured" {
		t.Fatalf("read endpoint disagrees with the event: %+v", payment)
	}
}

func TestDeclineEmitsAFailedEventWithErrorFields(t *testing.T) {
	store, clock := newTestStore(testConfig())
	notifier := &recordingNotifier{}
	store.SetNotifier(notifier)
	store.SetScenario("rcpt", ScenarioDecline)
	store.CreateOrder(newOrder("5000", "rcpt"))
	clock.Advance(time.Minute)
	if got := names(notifier.events); len(got) != 1 || got[0] != "payment.failed" {
		t.Fatalf("events = %v", got)
	}
	var failed eventEnvelopeForTest
	json.Unmarshal(notifier.events[0].Body, &failed)
	if p := failed.Payload.Payment.Entity; p.Status != "failed" || p.ErrorCode == nil {
		t.Fatalf("failed snapshot = %+v", p)
	}
}

func TestDuplicateOrderPolicyIsSeparateFromTheObservedReceiptBehaviour(t *testing.T) {
	scenarioOf := func(store *Store, id string) string {
		store.mu.Lock()
		defer store.mu.Unlock()
		return store.orders[id].scenario
	}
	t.Run("default policy pays only the first order", func(t *testing.T) {
		store, _ := newTestStore(testConfig())
		first, _ := store.CreateOrder(newOrder("5000", "dup"))
		second, apiErr := store.CreateOrder(newOrder("5000", "dup"))
		if apiErr != nil || scenarioOf(store, first.ID) != ScenarioCapture || scenarioOf(store, second.ID) != ScenarioNone {
			t.Fatalf("accepting the duplicate is the observed behaviour; paying it is policy: %s / %s",
				scenarioOf(store, first.ID), scenarioOf(store, second.ID))
		}
	})
	t.Run("policy all pays every duplicate", func(t *testing.T) {
		cfg := testConfig()
		cfg.DuplicatePays = "all"
		store, _ := newTestStore(cfg)
		store.CreateOrder(newOrder("5000", "dup"))
		second, _ := store.CreateOrder(newOrder("5000", "dup"))
		if scenarioOf(store, second.ID) != ScenarioCapture {
			t.Fatal("DuplicatePays=all must give the duplicate the checkout's scenario")
		}
	})
	t.Run("a scenario can pay either order", func(t *testing.T) {
		store, clock := newTestStore(testConfig())
		notifier := &recordingNotifier{}
		store.SetNotifier(notifier)
		store.CreateOrder(newOrder("5000", "dup"))
		second, _ := store.CreateOrder(newOrder("5000", "dup"))
		if apiErr := store.SetOrderScenario(second.ID, ScenarioCapture); apiErr != nil {
			t.Fatal(apiErr)
		}
		if apiErr := store.SetOrderScenario("order_missing", ScenarioCapture); apiErr == nil {
			t.Fatal("an unknown order must be rejected")
		}
		clock.Advance(time.Minute)
		clock.Advance(time.Minute)
		payments, _ := store.OrderPayments(second.ID)
		if len(payments) != 1 || payments[0].Status != "captured" {
			t.Fatalf("the pinned duplicate must be paid: %+v", payments)
		}
	})
}

type eventEnvelopeForTest struct {
	Entity    string   `json:"entity"`
	AccountID string   `json:"account_id"`
	Event     string   `json:"event"`
	Contains  []string `json:"contains"`
	Payload   struct {
		Payment struct {
			Entity PaymentView `json:"entity"`
		} `json:"payment"`
		Order *struct {
			Entity OrderView `json:"entity"`
		} `json:"order"`
	} `json:"payload"`
}

func names(events []Event) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.Name
	}
	return out
}
