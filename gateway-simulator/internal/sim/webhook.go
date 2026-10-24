package sim

import (
	"bytes"
	"container/heap"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Event is one webhook as created from a gateway state transition: the payload is
// a snapshot taken when the transition committed and is never regenerated, so a
// retry or redelivery carries the original bytes and event id.
type Event struct {
	Name      string
	Key       string // stable per event, e.g. the payment id
	PaymentID string
	OrderID   string
	CreatedAt time.Time
	Body      []byte
	// Rule is an optional scripted delivery rule (delay, omit, duplicates).
	Rule *DeliveryRule
}

// Notifier receives events from the store. It must not call back into the store.
type Notifier interface {
	Notify(Event)
}

type eventEnvelope struct {
	Entity    string         `json:"entity"`
	AccountID string         `json:"account_id"`
	Event     string         `json:"event"`
	Contains  []string       `json:"contains"`
	Payload   map[string]any `json:"payload"`
	CreatedAt int64          `json:"created_at"`
}

// buildEvent renders the documented envelope. order is nil for payment events.
func buildEvent(accountID, name string, at time.Time, payment *PaymentView, order *OrderView) []byte {
	payload := map[string]any{"payment": map[string]any{"entity": payment}}
	contains := []string{"payment"}
	if order != nil {
		payload["order"] = map[string]any{"entity": order}
		contains = append(contains, "order")
	}
	body, _ := json.Marshal(eventEnvelope{
		Entity: "event", AccountID: accountID, Event: name, Contains: contains,
		Payload: payload, CreatedAt: at.Unix(),
	})
	return body
}

// buildRefundEvent renders a refund event: the refund entity plus its payment.
func buildRefundEvent(accountID, name string, at time.Time, refund *RefundView, payment *PaymentView) []byte {
	body, _ := json.Marshal(eventEnvelope{
		Entity: "event", AccountID: accountID, Event: name, Contains: []string{"refund", "payment"},
		Payload: map[string]any{
			"refund":  map[string]any{"entity": refund},
			"payment": map[string]any{"entity": payment},
		},
		CreatedAt: at.Unix(),
	})
	return body
}

// sign is the hex HMAC-SHA256 of the exact outgoing bytes under the simulation
// webhook secret.
func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

const (
	deliveryPending   = "pending"
	deliveryDelivered = "delivered"
	deliveryExpired   = "expired"
	deliveryOmitted   = "omitted"
)

// Delivery is the delivery state of one event.
type Delivery struct {
	ID          string     `json:"event_id"`
	Event       string     `json:"event"`
	PaymentID   string     `json:"payment_id"`
	OrderID     string     `json:"order_id"`
	State       string     `json:"state"`
	Attempts    int        `json:"attempts"`
	Redelivery  bool       `json:"redelivery"`
	LastStatus  int        `json:"last_status"`
	LastError   string     `json:"last_error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	NextAt      time.Time  `json:"next_attempt_at"`
	DeliveredAt *time.Time `json:"delivered_at,omitempty"`

	body []byte
	seq  uint64
}

type deliveryHeap []*Delivery

func (h deliveryHeap) Len() int { return len(h) }
func (h deliveryHeap) Less(i, j int) bool {
	if h[i].NextAt.Equal(h[j].NextAt) {
		return h[i].seq < h[j].seq
	}
	return h[i].NextAt.Before(h[j].NextAt)
}
func (h deliveryHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *deliveryHeap) Push(x any)   { *h = append(*h, x.(*Delivery)) }
func (h *deliveryHeap) Pop() any {
	old := *h
	last := old[len(old)-1]
	*h = old[:len(old)-1]
	return last
}

// Webhooks delivers events over real HTTP with a bounded worker pool. A
// dispatcher moves due deliveries to the workers through a small channel, so a
// slow receiver applies backpressure (deliveries wait in the heap) instead of
// spawning goroutines. State lives in memory: a simulator restart loses pending
// deliveries (documented limitation; no persistence in this stage).
type Webhooks struct {
	cfg    Config
	client *http.Client
	now    func() time.Time

	mu    sync.Mutex
	heap  deliveryHeap
	byID  map[string]*Delivery
	order []string // creation order, for listing
	seq   uint64
	wake  chan struct{}
	work  chan *Delivery

	handoff        atomic.Int64 // popped by the dispatcher, not yet taken by a worker
	inFlight       atomic.Int64
	attempts       atomic.Int64
	failedAttempts atomic.Int64
	delivered      atomic.Int64
	expired        atomic.Int64
	redeliveries   atomic.Int64
	omitted        atomic.Int64
}

func NewWebhooks(cfg Config) *Webhooks {
	transport := &http.Transport{MaxIdleConnsPerHost: cfg.WebhookWorkers, IdleConnTimeout: 30 * time.Second}
	return &Webhooks{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.WebhookTimeout, Transport: transport},
		now:    time.Now,
		byID:   map[string]*Delivery{},
		wake:   make(chan struct{}, 1),
		work:   make(chan *Delivery, cfg.WebhookWorkers),
	}
}

func (w *Webhooks) Enabled() bool { return w != nil && w.cfg.WebhookURL != "" }

// Notify queues an event for delivery after a seeded delay. The event id is a
// pure function of the seed, event name and key, so it is stable across runs.
func (w *Webhooks) Notify(event Event) {
	if !w.Enabled() {
		return
	}
	id := entityID("evt_", w.cfg.Seed, "event", event.Name, event.Key)
	delay := lognormal(w.cfg.WebhookDelayMedian, 0.3, w.cfg.Seed, "webhook-delay", id)
	if event.Rule != nil && event.Rule.Delay != nil {
		delay = event.Rule.Delay.Duration // scripted: exact, no jitter
	}
	now := w.now()
	w.mu.Lock()
	if _, exists := w.byID[id]; exists {
		w.mu.Unlock()
		return
	}
	w.seq++
	d := &Delivery{
		ID: id, Event: event.Name, PaymentID: event.PaymentID, OrderID: event.OrderID,
		State: deliveryPending, CreatedAt: event.CreatedAt, NextAt: now.Add(delay),
		body: event.Body, seq: w.seq,
	}
	w.byID[id] = d
	w.order = append(w.order, id)
	if event.Rule != nil && event.Rule.Omit {
		// A lost webhook: the event exists and is listed, but is never sent.
		d.State = deliveryOmitted
		w.mu.Unlock()
		w.omitted.Add(1)
		return
	}
	heap.Push(&w.heap, d)
	if event.Rule != nil {
		for _, offset := range event.Rule.Duplicates {
			w.seq++
			clone := *d
			clone.Redelivery, clone.NextAt, clone.seq = true, d.NextAt.Add(offset.Duration), w.seq
			heap.Push(&w.heap, &clone)
			w.redeliveries.Add(1)
		}
	}
	w.mu.Unlock()
	w.poke()
}

// Redeliver sends the stored event again with the same id and original bytes, as
// Razorpay's at-least-once delivery may. It reports false for an unknown event.
func (w *Webhooks) Redeliver(id string) bool {
	w.mu.Lock()
	original, ok := w.byID[id]
	if !ok {
		w.mu.Unlock()
		return false
	}
	w.seq++
	clone := *original
	clone.State, clone.Attempts, clone.Redelivery = deliveryPending, 0, true
	clone.NextAt, clone.seq, clone.DeliveredAt = w.now(), w.seq, nil
	heap.Push(&w.heap, &clone)
	w.mu.Unlock()
	w.redeliveries.Add(1)
	w.poke()
	return true
}

func (w *Webhooks) poke() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Run starts the workers and the dispatcher; it returns when ctx ends.
func (w *Webhooks) Run(ctx context.Context) {
	if !w.Enabled() {
		return
	}
	for i := 0; i < w.cfg.WebhookWorkers; i++ {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case d := <-w.work:
					w.deliver(ctx, d)
				}
			}
		}()
	}
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		w.mu.Lock()
		var next *Delivery
		if len(w.heap) > 0 {
			next = w.heap[0]
		}
		var wait time.Duration
		if next != nil {
			wait = next.NextAt.Sub(w.now())
		}
		w.mu.Unlock()

		if next == nil {
			select {
			case <-ctx.Done():
				return
			case <-w.wake:
			}
			continue
		}
		if wait > 0 {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(wait)
			select {
			case <-ctx.Done():
				return
			case <-w.wake:
			case <-timer.C:
			}
			continue
		}

		w.mu.Lock()
		if len(w.heap) == 0 || w.heap[0] != next {
			w.mu.Unlock()
			continue
		}
		due := heap.Pop(&w.heap).(*Delivery)
		w.mu.Unlock()
		w.handoff.Add(1)
		select {
		case w.work <- due: // blocks while every worker is busy: backpressure
			w.handoff.Add(-1)
		case <-ctx.Done():
			return
		}
	}
}

// deliver makes one HTTP attempt. Only 2xx acknowledges the event; every other
// outcome (non-2xx, transport error, timeout) is retried on the schedule until
// the event is older than the max age.
func (w *Webhooks) deliver(ctx context.Context, d *Delivery) {
	w.inFlight.Add(1)
	defer w.inFlight.Add(-1)
	w.attempts.Add(1)

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, w.cfg.WebhookURL, bytes.NewReader(d.body))
	status, failure := 0, ""
	if err != nil {
		failure = err.Error()
	} else {
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Razorpay-Signature", sign(w.cfg.WebhookSecret, d.body))
		request.Header.Set("x-razorpay-event-id", d.ID)
		response, err := w.client.Do(request)
		if err != nil {
			failure = err.Error()
		} else {
			status = response.StatusCode
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<16))
			response.Body.Close()
		}
	}

	acknowledged := failure == "" && status >= 200 && status < 300
	now := w.now()

	w.mu.Lock()
	d.Attempts++
	d.LastStatus, d.LastError = status, failure
	if acknowledged {
		d.State = deliveryDelivered
		d.DeliveredAt = &now
		w.mu.Unlock()
		w.delivered.Add(1)
		return
	}
	w.failedAttempts.Add(1)
	index := d.Attempts - 1
	if index >= len(w.cfg.WebhookSchedule) {
		index = len(w.cfg.WebhookSchedule) - 1
	}
	retryAt := now.Add(w.cfg.WebhookSchedule[index])
	if retryAt.Sub(d.CreatedAt) > w.cfg.WebhookMaxAge {
		d.State = deliveryExpired
		w.mu.Unlock()
		w.expired.Add(1)
		return
	}
	d.NextAt = retryAt
	w.seq++
	d.seq = w.seq
	heap.Push(&w.heap, d)
	w.mu.Unlock()
	w.poke()
}

type WebhookStats struct {
	Enabled        bool  `json:"enabled"`
	Pending        int   `json:"pending"`
	InFlight       int64 `json:"in_flight"`
	Attempts       int64 `json:"attempts"`
	FailedAttempts int64 `json:"failed_attempts"`
	Delivered      int64 `json:"delivered"`
	Expired        int64 `json:"expired"`
	Redeliveries   int64 `json:"redeliveries"`
	Omitted        int64 `json:"omitted"`
	Events         int   `json:"events"`
	Workers        int   `json:"workers"`
}

func (w *Webhooks) Stats() WebhookStats {
	if w == nil {
		return WebhookStats{}
	}
	w.mu.Lock()
	pending, events := len(w.heap), len(w.byID)
	w.mu.Unlock()
	// the backlog is everything not yet in a worker: waiting in the heap, being
	// handed over, or queued for a worker
	pending += int(w.handoff.Load()) + len(w.work)
	return WebhookStats{
		Enabled: w.Enabled(), Pending: pending, InFlight: w.inFlight.Load(), Attempts: w.attempts.Load(),
		FailedAttempts: w.failedAttempts.Load(), Delivered: w.delivered.Load(), Expired: w.expired.Load(),
		Redeliveries: w.redeliveries.Load(), Omitted: w.omitted.Load(), Events: events, Workers: w.cfg.WebhookWorkers,
	}
}

// List returns the most recent events, newest first, optionally for one payment.
func (w *Webhooks) List(limit int, paymentID string) []Delivery {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	var items []Delivery
	for i := len(w.order) - 1; i >= 0 && len(items) < limit; i-- {
		d := w.byID[w.order[i]]
		if paymentID != "" && d.PaymentID != paymentID {
			continue
		}
		items = append(items, *d)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items
}
