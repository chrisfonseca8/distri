package sim

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func dur(s string) Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		panic(err)
	}
	return Duration{d}
}

func ptr(d Duration) *Duration { return &d }

func TestPlanValidation(t *testing.T) {
	bad := map[string]Plan{
		"unknown outcome":     {Attempts: []PlanAttempt{{At: dur("1s"), Outcome: "teleport"}}},
		"negative offset":     {Attempts: []PlanAttempt{{At: dur("-1s"), Outcome: "capture"}}},
		"unknown event":       {Delivery: map[string]DeliveryRule{"payment.exploded": {}}},
		"negative delay":      {Delivery: map[string]DeliveryRule{"payment.captured": {Delay: ptr(dur("-1s"))}}},
		"negative duplicate":  {Delivery: map[string]DeliveryRule{"payment.captured": {Duplicates: []Duration{dur("-1s")}}}},
		"negative capture_at": {Attempts: []PlanAttempt{{At: dur("1s"), Outcome: "capture", CaptureAfter: ptr(dur("-1s"))}}},
	}
	for name, plan := range bad {
		if err := plan.Validate(); err == nil {
			t.Errorf("%s: plan accepted", name)
		}
	}
	var parsed Plan
	body := `{"attempts":[{"at":"2s","outcome":"decline"},{"at":"6s","outcome":"capture","capture_after":"1s"}],
		"delivery":{"payment.authorized":{"delay":"400ms"},"payment.captured":{"omit":true,"duplicates":["1s"]}}}`
	if err := json.Unmarshal([]byte(body), &parsed); err != nil || parsed.Validate() != nil {
		t.Fatalf("a valid plan was rejected: %v %v", err, parsed.Validate())
	}
	if parsed.Attempts[1].CaptureAfter.Duration != time.Second || !parsed.Delivery["payment.captured"].Omit {
		t.Fatalf("parsed = %+v", parsed)
	}
}

func TestScriptedDeclineThenCaptureUsesExactTimes(t *testing.T) {
	cfg := testConfig()
	cfg.TimeScale = 10 // a plan is a script: it must NOT be sped up
	store, clock := newTestStore(cfg)
	notifier := &recordingNotifier{}
	store.SetNotifier(notifier)
	store.SetPlan("rcpt-b", Plan{Attempts: []PlanAttempt{
		{At: dur("2s"), Outcome: "decline"},
		{At: dur("6s"), Outcome: "capture", CaptureAfter: ptr(dur("1s"))},
	}})
	o, _ := store.CreateOrder(newOrder("25000", "rcpt-b"))

	clock.Advance(1500 * time.Millisecond)
	if p, _ := store.OrderPayments(o.ID); len(p) != 0 {
		t.Fatal("nothing may happen before the scripted offset (no time scaling)")
	}
	clock.Advance(time.Second) // t = 2.5 s: the decline
	if p, _ := store.OrderPayments(o.ID); len(p) != 1 || p[0].Status != "failed" {
		t.Fatalf("payments after the decline = %+v", p)
	}
	if got, _ := store.GetOrder(o.ID); got.Status != "attempted" {
		t.Fatalf("a failed attempt must leave the order payable, got %s", got.Status)
	}
	clock.Advance(4 * time.Second) // t = 6.5 s: second attempt authorized
	if p, _ := store.OrderPayments(o.ID); len(p) != 2 || p[1].Status != "authorized" {
		t.Fatalf("payments after the second attempt = %+v", p)
	}
	clock.Advance(time.Second) // t = 7.5 s: captured exactly 1 s after authorization
	if got, _ := store.GetOrder(o.ID); got.Status != "paid" {
		t.Fatalf("order = %s, want paid", got.Status)
	}
	want := []string{"payment.failed", "payment.authorized", "payment.captured", "order.paid"}
	if got := names(notifier.events); len(got) != 4 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] || got[3] != want[3] {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestOrderPlanCanPayAnExistingDuplicate(t *testing.T) {
	store, clock := newTestStore(testConfig())
	store.CreateOrder(newOrder("5000", "dup"))
	second, _ := store.CreateOrder(newOrder("5000", "dup"))
	if apiErr := store.SetOrderPlan(second.ID, Plan{Attempts: []PlanAttempt{{At: dur("1s"), Outcome: "capture", CaptureAfter: ptr(dur("1s"))}}}); apiErr != nil {
		t.Fatal(apiErr)
	}
	if apiErr := store.SetOrderPlan("order_missing", Plan{}); apiErr == nil {
		t.Fatal("an unknown order must be rejected")
	}
	clock.Advance(3 * time.Second) // the scripted attempt
	clock.Advance(3 * time.Second) // its capture
	if got, _ := store.GetOrder(second.ID); got.Status != "paid" {
		t.Fatalf("the scripted duplicate must be paid, got %s", got.Status)
	}
}

// capturePlanEvents emits the four events of one successful payment into a real
// Webhooks through a store driven by the manual clock.
func capturePlanEvents(t *testing.T, webhooks *Webhooks, plan Plan) {
	t.Helper()
	cfg := testConfig()
	store, clock := newTestStore(cfg)
	store.SetNotifier(webhooks)
	store.SetPlan("rcpt", plan)
	store.CreateOrder(newOrder("25000", "rcpt"))
	clock.Advance(10 * time.Second) // attempt: payment.authorized
	clock.Advance(10 * time.Second) // capture: payment.captured + order.paid
}

func capturePlan(delivery map[string]DeliveryRule) Plan {
	return Plan{
		Attempts: []PlanAttempt{{At: dur("1s"), Outcome: "capture", CaptureAfter: ptr(dur("1s"))}},
		Delivery: delivery,
	}
}

func TestDeliveryOrderIsIndependentOfTransitionOrder(t *testing.T) {
	var mu sync.Mutex
	var order []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var envelope struct {
			Event string `json:"event"`
		}
		json.Unmarshal(body, &envelope)
		mu.Lock()
		order = append(order, envelope.Event)
		mu.Unlock()
	}))
	defer server.Close()
	webhooks := startWebhooks(t, webhookConfig(server.URL))
	// authorized is created first but scripted to arrive after captured
	capturePlanEvents(t, webhooks, capturePlan(map[string]DeliveryRule{
		"payment.authorized": {Delay: ptr(dur("400ms"))},
		"payment.captured":   {Delay: ptr(dur("0s"))},
		"order.paid":         {Delay: ptr(dur("100ms"))},
	}))
	waitFor(t, "all three events", func() bool { return webhooks.Stats().Delivered == 3 })
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 3 || order[0] != "payment.captured" || order[1] != "order.paid" || order[2] != "payment.authorized" {
		t.Fatalf("delivery order = %v, want captured, order.paid, then the older authorized", order)
	}
}

func TestOmittedEventIsListedButNeverSent(t *testing.T) {
	var mu sync.Mutex
	var sent []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var envelope struct {
			Event string `json:"event"`
		}
		json.Unmarshal(body, &envelope)
		mu.Lock()
		sent = append(sent, envelope.Event)
		mu.Unlock()
	}))
	defer server.Close()
	webhooks := startWebhooks(t, webhookConfig(server.URL))
	capturePlanEvents(t, webhooks, capturePlan(map[string]DeliveryRule{
		"payment.captured": {Omit: true},
		"order.paid":       {Omit: true},
	}))
	waitFor(t, "the authorized event", func() bool { return webhooks.Stats().Delivered == 1 })
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 || sent[0] != "payment.authorized" {
		t.Fatalf("sent = %v, only the un-omitted event may be delivered", sent)
	}
	omitted := 0
	for _, d := range webhooks.List(10, "") {
		if d.State == deliveryOmitted {
			omitted++
		}
	}
	if omitted != 2 || webhooks.Stats().Omitted != 2 {
		t.Fatalf("omitted listed=%d stats=%+v", omitted, webhooks.Stats())
	}
}

func TestScriptedDuplicatesAreByteIdentical(t *testing.T) {
	var mu sync.Mutex
	var seen []received
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var envelope struct {
			Event string `json:"event"`
		}
		json.Unmarshal(body, &envelope)
		if envelope.Event != "payment.captured" {
			return
		}
		mu.Lock()
		seen = append(seen, received{body, r.Header.Get("X-Razorpay-Signature"), r.Header.Get("x-razorpay-event-id")})
		mu.Unlock()
	}))
	defer server.Close()
	webhooks := startWebhooks(t, webhookConfig(server.URL))
	capturePlanEvents(t, webhooks, capturePlan(map[string]DeliveryRule{
		"payment.captured": {Delay: ptr(dur("0s")), Duplicates: []Duration{dur("50ms"), dur("120ms")}},
	}))
	waitFor(t, "the original and two duplicates", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(seen) == 3
	})
	mu.Lock()
	defer mu.Unlock()
	for _, delivery := range seen[1:] {
		if delivery.eventID != seen[0].eventID || string(delivery.body) != string(seen[0].body) || delivery.signature != seen[0].signature {
			t.Fatal("a duplicate must keep the event id, payload bytes and signature")
		}
	}
}

func TestDifferentEventsAboutOnePaymentCanArriveConcurrently(t *testing.T) {
	var arrived sync.WaitGroup
	arrived.Add(2) // payment.captured and order.paid must both be in flight together
	both := make(chan struct{})
	go func() { arrived.Wait(); close(both) }()
	var overlap sync.Map
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var envelope struct {
			Event string `json:"event"`
		}
		json.Unmarshal(body, &envelope)
		if envelope.Event == "payment.authorized" {
			return
		}
		arrived.Done()
		select {
		case <-both: // held until the other event's request has arrived too
			overlap.Store(envelope.Event, true)
		case <-time.After(3 * time.Second):
		}
	}))
	defer server.Close()
	cfg := webhookConfig(server.URL)
	cfg.WebhookTimeout = 5 * time.Second
	webhooks := startWebhooks(t, cfg)
	capturePlanEvents(t, webhooks, capturePlan(map[string]DeliveryRule{
		"payment.captured": {Delay: ptr(dur("0s"))},
		"order.paid":       {Delay: ptr(dur("0s"))},
	}))
	waitFor(t, "both events delivered", func() bool { return webhooks.Stats().Delivered == 3 })
	_, a := overlap.Load("payment.captured")
	_, b := overlap.Load("order.paid")
	if !a || !b {
		t.Fatal("payment.captured and order.paid must have been in flight at the same time")
	}
}
