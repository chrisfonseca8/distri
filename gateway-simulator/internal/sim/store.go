package sim

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// APIError is a documented-shape Razorpay error. Status is the HTTP status.
type APIError struct {
	Status      int
	Code        string
	Description string
	Source      string
	Step        string
	Reason      string
	Field       string
}

// Error returns the error's description.
func (e *APIError) Error() string { return e.Description }

// badRequest builds a 400 error in Razorpay's format.
func badRequest(description, field string) *APIError {
	return &APIError{
		Status: http.StatusBadRequest, Code: "BAD_REQUEST_ERROR", Description: description,
		Source: "business", Step: "payment_initiation", Reason: "input_validation_failed", Field: field,
	}
}

// OrderRequest is the body of POST /v1/orders.
type OrderRequest struct {
	Amount   json.Number    `json:"amount"`
	Currency string         `json:"currency"`
	Receipt  string         `json:"receipt"`
	Notes    map[string]any `json:"notes"`
	Payment  *struct {
		Capture string `json:"capture"`
	} `json:"payment"`
}

// RefundRequest is the body of POST /v1/payments/{id}/refund. IdempotencyKey is the
// X-Refund-Idempotency header (documented: at least 10 characters of letters,
// digits, '-' and '_').
type RefundRequest struct {
	Amount         json.Number    `json:"amount"`
	Speed          string         `json:"speed"`
	Receipt        string         `json:"receipt"`
	Notes          map[string]any `json:"notes"`
	IdempotencyKey string         `json:"-"`
}

type order struct {
	id         string
	amount     int64
	amountPaid int64
	currency   string
	receipt    string
	notes      map[string]string
	status     string // created, attempted, paid
	attempts   int
	createdAt  time.Time
	visibleAt  time.Time // when list-by-receipt starts to show it
	capture    string    // automatic or manual
	scenario   string
	plan       *Plan // optional script: attempt times and delivery rules
	paySeq     int
}

type payment struct {
	id             string
	orderID        string
	amount         int64
	currency       string
	status         string // created, authorized, captured, refunded, failed
	method         string
	captured       bool
	amountRefunded int64
	refundStatus   string // "", partial, full
	errorCode      string
	errorDesc      string
	errorSource    string
	errorStep      string
	errorReason    string
	createdAt      time.Time
}

type refund struct {
	id        string
	paymentID string
	amount    int64
	currency  string
	receipt   string
	notes     map[string]string
	status    string // pending, processed, failed
	speed     string
	createdAt time.Time
	visibleAt time.Time // fetch and list show the refund from this time
}

// refundKey remembers one X-Refund-Idempotency key.
type refundKey struct {
	refundID    string
	fingerprint string
	at          time.Time
	inFlight    bool // committed, response still being delayed (a plan's "slow" response)
}

// Store holds all gateway state. One mutex guards it: every operation is a few
// map lookups, so the lock is held for microseconds. All read endpoints derive
// from this single state, so an order, its payments and any event built from them
// can never disagree.
type Store struct {
	mu  sync.Mutex
	cfg Config
	now func() time.Time
	at  func(time.Time, func())

	orders          map[string]*order
	orderList       []*order // creation order
	byReceipt       map[string][]*order
	payments        map[string]*payment
	paymentsByOrder map[string][]*payment
	refunds         map[string]*refund
	refundList      []*refund
	refundsByPay    map[string][]*refund
	refundKeys      map[string]*refundKey

	scenarioByReceipt map[string]string
	planByReceipt     map[string]*Plan
	defaultScenario   string
	orderCounter      int

	notifier Notifier // optional; receives an event after each committed transition
}

// SetNotifier sets who receives webhook events. Events are built while the
// transition's lock is held, from the state it just committed, and only queued
// here; delivery happens elsewhere.
func (s *Store) SetNotifier(n Notifier) {
	s.mu.Lock()
	s.notifier = n
	s.mu.Unlock()
}

// notify builds the event snapshot from current state. The caller holds s.mu and
// has already applied the transition, so an event can never describe a state the
// gateway has not reached.
func (s *Store) notify(name, key string, p *payment, o *order, withOrder bool) {
	if s.notifier == nil {
		return
	}
	paymentSnapshot := paymentView(p)
	var orderSnapshot *OrderView
	if withOrder {
		view := orderView(o)
		orderSnapshot = &view
	}
	at := s.now()
	event := Event{
		Name: name, Key: key, PaymentID: p.id, OrderID: o.id, CreatedAt: at,
		Body: buildEvent(s.cfg.WebhookAccountID, name, at, &paymentSnapshot, orderSnapshot),
	}
	if o.plan != nil {
		if rule, ok := o.plan.Delivery[name]; ok {
			event.Rule = &rule
		}
	}
	s.notifier.Notify(event)
}

// NewStore creates an empty store whose delayed work is scheduled through at.
func NewStore(cfg Config, at func(time.Time, func())) *Store {
	return &Store{
		cfg:               cfg,
		now:               time.Now,
		at:                at,
		orders:            map[string]*order{},
		byReceipt:         map[string][]*order{},
		payments:          map[string]*payment{},
		paymentsByOrder:   map[string][]*payment{},
		refunds:           map[string]*refund{},
		refundsByPay:      map[string][]*refund{},
		refundKeys:        map[string]*refundKey{},
		scenarioByReceipt: map[string]string{},
		planByReceipt:     map[string]*Plan{},
	}
}

// SetPlan scripts the checkout with this receipt before its gateway order exists.
func (s *Store) SetPlan(receipt string, plan Plan) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	s.planByReceipt[receipt] = &plan
	s.mu.Unlock()
	return nil
}

// SetOrderPlan scripts one existing order; attempt offsets count from now.
func (s *Store) SetOrderPlan(orderID string, plan Plan) *APIError {
	if err := plan.Validate(); err != nil {
		return badRequest(err.Error(), "plan")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.orders[orderID]
	if o == nil {
		return unknownID()
	}
	o.plan, o.scenario = &plan, "plan"
	s.schedulePlanLocked(o, s.now())
	return nil
}

// schedulePlanLocked schedules every scripted attempt. The caller holds s.mu.
func (s *Store) schedulePlanLocked(o *order, from time.Time) {
	for _, attempt := range o.plan.Attempts {
		attempt, orderID := attempt, o.id
		s.at(from.Add(attempt.At.Duration), func() { s.attempt(orderID, attempt.Outcome, attempt.CaptureAfter) })
	}
}

// SetScenario pins the scenario of a checkout (by receipt) before its order is
// created. SetDefaultScenario pins every checkout without its own pin; an empty
// name clears it and returns to the configured mix.
func (s *Store) SetScenario(receipt, name string) error {
	if !ValidScenario(name) {
		return fmt.Errorf("unknown scenario %q", name)
	}
	s.mu.Lock()
	s.scenarioByReceipt[receipt] = name
	s.mu.Unlock()
	return nil
}

// SetDefaultScenario sets the default scenario, or clears it if name is empty.
func (s *Store) SetDefaultScenario(name string) error {
	if name != "" && !ValidScenario(name) {
		return fmt.Errorf("unknown scenario %q", name)
	}
	s.mu.Lock()
	s.defaultScenario = name
	s.mu.Unlock()
	return nil
}

// ---- orders ----

// CreateOrder validates the request and creates an order.
func (s *Store) CreateOrder(req OrderRequest) (OrderView, *APIError) {
	amount, err := parseAmount(req.Amount)
	if err != nil || req.Amount == "" {
		return OrderView{}, badRequest("The amount must be an integer.", "amount")
	}
	if amount < 100 {
		return OrderView{}, badRequest("Order amount less than minimum amount allowed", "amount")
	}
	currency := strings.ToUpper(strings.TrimSpace(req.Currency))
	if len(currency) != 3 {
		return OrderView{}, badRequest("The currency field is required and must be three characters.", "currency")
	}
	if len(req.Receipt) > 40 {
		return OrderView{}, badRequest("receipt: the length may not be greater than 40", "receipt")
	}
	notes, apiErr := parseNotes(req.Notes)
	if apiErr != nil {
		return OrderView{}, apiErr
	}
	capture := "automatic"
	if !s.cfg.AutoCapture {
		capture = "manual"
	}
	if req.Payment != nil && req.Payment.Capture != "" {
		if req.Payment.Capture != "automatic" && req.Payment.Capture != "manual" {
			return OrderView{}, badRequest("payment.capture must be automatic or manual", "payment.capture")
		}
		capture = req.Payment.Capture
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.EnforceReceiptUnique && req.Receipt != "" && len(s.byReceipt[req.Receipt]) > 0 {
		return OrderView{}, badRequest("Duplicate request. This request has already been processed.", "receipt")
	}

	now := s.now()
	s.orderCounter++
	seq := len(s.byReceipt[req.Receipt]) + 1
	key := req.Receipt
	if key == "" {
		key = "no-receipt-" + itoa(s.orderCounter)
	}
	id := entityID("order_", s.cfg.Seed, "order", key, itoa(seq))
	for s.orders[id] != nil {
		id = entityID("order_", s.cfg.Seed, "order", key, itoa(seq), "retry")
	}

	// Two separate things. Accepting a duplicate receipt is OBSERVED Razorpay
	// behaviour (see the order-creation check above). Whether the simulated customer
	// pays a duplicate order is only a SIMULATOR POLICY (cfg.DuplicatePays): by
	// default only the first order of a receipt is paid, "all" pays every one, and
	// SetOrderScenario can pin any single order.
	scenario := ScenarioNone
	var plan *Plan
	if seq == 1 || s.cfg.DuplicatePays == "all" {
		switch {
		case s.planByReceipt[req.Receipt] != nil:
			plan, scenario = s.planByReceipt[req.Receipt], "plan"
		case s.scenarioByReceipt[req.Receipt] != "":
			scenario = s.scenarioByReceipt[req.Receipt]
		case s.defaultScenario != "":
			scenario = s.defaultScenario
		default:
			scenario = pickScenario(s.cfg.ScenarioMix, s.cfg.Seed, key)
		}
	}

	lookupDelay := s.cfg.LookupDelayMin
	if spread := s.cfg.LookupDelayMax - s.cfg.LookupDelayMin; spread > 0 {
		lookupDelay += time.Duration(unit(s.cfg.Seed, "lookup", id) * float64(spread))
	}
	o := &order{
		id: id, amount: amount, currency: currency, receipt: req.Receipt, notes: notes,
		status: "created", createdAt: now, visibleAt: now.Add(s.cfg.scaled(lookupDelay)),
		capture: capture, scenario: scenario, plan: plan,
	}
	s.orders[id] = o
	s.orderList = append(s.orderList, o)
	if req.Receipt != "" {
		s.byReceipt[req.Receipt] = append(s.byReceipt[req.Receipt], o)
	}

	switch {
	case plan != nil:
		s.schedulePlanLocked(o, now)
	case scenario != ScenarioNone:
		delay := s.cfg.scaled(lognormal(s.cfg.PayDelayMedian, 0.4, s.cfg.Seed, "pay-delay", key))
		s.at(now.Add(delay), func() { s.attemptPayment(id) })
	}
	return orderView(o), nil
}

// GetOrder returns one order, or an error if the ID is unknown.
func (s *Store) GetOrder(id string) (OrderView, *APIError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.orders[id]
	if o == nil {
		return OrderView{}, unknownID()
	}
	return orderView(o), nil
}

// ListOrders returns orders newest first. Orders younger than their lookup delay
// are hidden: the real receipt filter was observed to lag. Fetch by id is not
// affected.
func (s *Store) ListOrders(receipt string, count, skip int) ([]OrderView, *APIError) {
	if count < 0 || count > 100 {
		return nil, badRequest("count must be between 0 and 100", "count")
	}
	if count == 0 {
		count = 10
	}
	if skip < 0 {
		return nil, badRequest("skip must not be negative", "skip")
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	var items []OrderView
	skipped := 0
	for i := len(s.orderList) - 1; i >= 0 && len(items) < count; i-- {
		o := s.orderList[i]
		if receipt != "" && o.receipt != receipt {
			continue
		}
		if now.Before(o.visibleAt) {
			continue
		}
		if skipped < skip {
			skipped++
			continue
		}
		items = append(items, orderView(o))
	}
	return items, nil
}

// OrderPayments returns the payments of one order.
func (s *Store) OrderPayments(orderID string) ([]PaymentView, *APIError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.orders[orderID] == nil {
		return nil, unknownID()
	}
	items := make([]PaymentView, 0, len(s.paymentsByOrder[orderID]))
	for _, p := range s.paymentsByOrder[orderID] {
		items = append(items, paymentView(p))
	}
	return items, nil
}

// ---- payments ----

// GetPayment returns one payment, or an error if the ID is unknown.
func (s *Store) GetPayment(id string) (PaymentView, *APIError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.payments[id]
	if p == nil {
		return PaymentView{}, unknownID()
	}
	return paymentView(p), nil
}

// attemptPayment is the simulated customer's attempt as chosen by the order's
// named scenario (the seeded path).
func (s *Store) attemptPayment(orderID string) {
	s.mu.Lock()
	o := s.orders[orderID]
	outcome := ""
	if o != nil {
		switch o.scenario {
		case ScenarioDecline:
			outcome = "decline"
		case ScenarioAuthorizeOnly:
			outcome = "authorize"
		case ScenarioCapture:
			outcome = "capture"
		}
	}
	s.mu.Unlock()
	if outcome != "" {
		s.attempt(orderID, outcome, nil)
	}
}

// attempt is one simulated customer payment attempt: decline, authorize (wait for
// a manual capture) or capture (authorize, then capture after captureAfter when
// the order captures automatically). It is a gateway state transition; the
// notification is queued after the state has changed.
func (s *Store) attempt(orderID, outcome string, captureAfter *Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.orders[orderID]
	if o == nil || o.status == "paid" {
		return
	}
	now := s.now()
	o.paySeq++
	o.attempts++
	if o.status == "created" {
		o.status = "attempted"
	}
	p := &payment{
		id:      entityID("pay_", s.cfg.Seed, "pay", o.id, itoa(o.paySeq)),
		orderID: o.id, amount: o.amount, currency: o.currency, method: "upi", createdAt: now,
	}
	s.payments[p.id] = p
	s.paymentsByOrder[o.id] = append(s.paymentsByOrder[o.id], p)

	if outcome == "decline" {
		p.status = "failed"
		p.errorCode, p.errorSource, p.errorStep, p.errorReason = "BAD_REQUEST_ERROR", "customer", "payment_authentication", "incorrect_otp"
		p.errorDesc = "Payment failed because the one-time password was incorrect"
		s.notify("payment.failed", p.id, p, o, false)
		return
	}

	p.status = "authorized"
	s.notify("payment.authorized", p.id, p, o, false)
	if outcome == "capture" && o.capture == "automatic" {
		var delay time.Duration
		if captureAfter != nil {
			delay = captureAfter.Duration // scripted: exact
		} else {
			delay = s.cfg.scaled(lognormal(s.cfg.CaptureDelayMedian, 0.3, s.cfg.Seed, "capture-delay", p.id))
		}
		paymentID := p.id
		s.at(now.Add(delay), func() { s.autoCapture(paymentID) })
	}
}

// autoCapture captures a payment if it is still only authorized.
func (s *Store) autoCapture(paymentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.payments[paymentID]; p != nil && p.status == "authorized" {
		s.markCaptured(p)
	}
}

// markCaptured moves an authorized payment to captured and its order to paid.
// The caller holds the lock.
func (s *Store) markCaptured(p *payment) {
	p.status = "captured"
	p.captured = true
	o := s.orders[p.orderID]
	if o == nil {
		return
	}
	o.status = "paid"
	o.amountPaid = p.amount
	s.notify("payment.captured", p.id, p, o, false)
	s.notify("order.paid", o.id, p, o, true)
}

// SetOrderScenario pins the scenario of one existing order, so a test can have
// the simulated customer pay any order, including a duplicate. It schedules the
// payment attempt unless the order has already had one.
func (s *Store) SetOrderScenario(orderID, name string) *APIError {
	if !ValidScenario(name) {
		return badRequest("unknown scenario "+name, "name")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.orders[orderID]
	if o == nil {
		return unknownID()
	}
	attempted := o.paySeq > 0
	o.scenario = name
	if name != ScenarioNone && !attempted {
		delay := s.cfg.scaled(lognormal(s.cfg.PayDelayMedian, 0.4, s.cfg.Seed, "pay-delay", o.id))
		s.at(s.now().Add(delay), func() { s.attemptPayment(orderID) })
	}
	return nil
}

// CapturePayment is the merchant capturing an authorized payment.
func (s *Store) CapturePayment(id string, amountRaw json.Number, currency string) (PaymentView, *APIError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.payments[id]
	if p == nil {
		return PaymentView{}, unknownID()
	}
	amount, err := parseAmount(amountRaw)
	if err != nil || amountRaw == "" {
		return PaymentView{}, badRequest("The amount must be an integer.", "amount")
	}
	if strings.ToUpper(currency) != p.currency {
		return PaymentView{}, badRequest("The currency must match the payment currency.", "currency")
	}
	if p.status != "authorized" {
		if p.captured {
			return PaymentView{}, badRequest("This payment has already been captured", "")
		}
		return PaymentView{}, badRequest("This payment is not in the authorized state", "")
	}
	if amount != p.amount {
		return PaymentView{}, badRequest("Capture amount must be equal to the amount authorized", "amount")
	}
	s.markCaptured(p)
	return paymentView(p), nil
}

// ---- refunds ----

// RefundBehavior is what the caller of a refund request is shown, separate from the
// refund itself: the refund is committed before any of this applies, so a lost
// response, a long delay or a client that gave up never undoes it.
type RefundBehavior struct {
	Lost     bool          // close the connection without a response
	Delay    time.Duration // answer this long after the commit
	Error500 bool          // answer HTTP 500 after the commit
	// keyToClear is the idempotency key to release once a delayed response is sent.
	keyToClear string
}

var idempotencyKeyPattern = func(key string) bool {
	if len(key) < 10 {
		return false
	}
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// CreateRefund creates a refund and drops the caller-facing behaviour (used by tests).
func (s *Store) CreateRefund(paymentID string, req RefundRequest) (RefundView, *APIError) {
	view, _, apiErr := s.CreateRefundWithBehavior(paymentID, req)
	return view, apiErr
}

// CreateRefundWithBehavior is atomic with respect to the refundable amount: the
// check and the update happen under one lock, so two concurrent refunds can never
// exceed what was captured. An X-Refund-Idempotency key replays the original refund
// for the same request (assumed: Razorpay documents that a retry is safe but not what
// the replay returns), answers 409 for the same key with a different request or while
// the original is still being answered, and is only remembered for
// cfg.RefundKeyRetention when that is set (an injected fault).
func (s *Store) CreateRefundWithBehavior(paymentID string, req RefundRequest) (RefundView, RefundBehavior, *APIError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fail := func(e *APIError) (RefundView, RefundBehavior, *APIError) { return RefundView{}, RefundBehavior{}, e }

	if req.IdempotencyKey != "" && !idempotencyKeyPattern(req.IdempotencyKey) {
		return fail(badRequest("The idempotency key must be at least 10 characters long and contain only letters, numbers, hyphens and underscores.", ""))
	}
	p := s.payments[paymentID]
	if p == nil {
		return fail(unknownID())
	}
	now := s.now()

	fingerprint := paymentID + "|" + req.Amount.String() + "|" + req.Receipt
	if req.IdempotencyKey != "" {
		if entry, ok := s.refundKeys[req.IdempotencyKey]; ok {
			if s.cfg.RefundKeyRetention > 0 && now.Sub(entry.at) > s.cfg.RefundKeyRetention {
				delete(s.refundKeys, req.IdempotencyKey) // the key was forgotten (fault)
			} else {
				switch {
				case entry.fingerprint != fingerprint:
					e := badRequest("Different request with the same idempotency key has already been processed.", "")
					e.Status = http.StatusConflict
					return fail(e)
				case entry.inFlight:
					e := badRequest("Another request with the same idempotency key is still in progress.", "")
					e.Status = http.StatusConflict
					return fail(e)
				}
				return refundView(s.refunds[entry.refundID]), RefundBehavior{}, nil
			}
		}
	}

	var rule *RefundRule
	if o := s.orders[p.orderID]; o != nil && o.plan != nil {
		ordinal := 0
		for _, pay := range s.paymentsByOrder[o.id] {
			ordinal += len(s.refundsByPay[pay.id])
		}
		if ordinal < len(o.plan.Refunds) {
			rule = &o.plan.Refunds[ordinal]
		}
	}
	// Faults that leave no refund behind.
	if rule != nil {
		switch rule.Response {
		case "reject":
			return fail(badRequest("The refund request was rejected.", ""))
		case "error_before":
			return fail(&APIError{Status: http.StatusInternalServerError, Code: "SERVER_ERROR", Description: "An error occurred before the refund was created.",
				Source: "internal", Step: "refund_initiation", Reason: "server_error"})
		}
	}

	if !p.captured {
		return fail(badRequest("The payment status should be captured for action to be taken.", ""))
	}
	remaining := p.amount - p.amountRefunded
	if remaining <= 0 {
		return fail(badRequest("The payment has been fully refunded already.", ""))
	}
	amount := remaining
	if req.Amount != "" {
		parsed, err := parseAmount(req.Amount)
		if err != nil {
			return fail(badRequest("The amount must be an integer.", "amount"))
		}
		switch {
		case parsed == 0:
			return fail(badRequest("Amount cannot be blank.", "amount"))
		case parsed < 100:
			return fail(badRequest("The refund amount should be at least INR 1.00", "amount"))
		case parsed > remaining:
			return fail(badRequest("The refund amount provided is greater than the amount captured", "amount"))
		}
		amount = parsed
	}
	if len(req.Receipt) > 40 {
		return fail(badRequest("receipt: the length may not be greater than 40", "receipt"))
	}
	if s.cfg.EnforceRefundReceiptUnique && req.Receipt != "" {
		for _, existing := range s.refundsByPay[paymentID] {
			if existing.receipt == req.Receipt {
				return fail(badRequest("Duplicate request. The receipt has already been used on this payment.", "receipt"))
			}
		}
	}
	notes, apiErr := parseNotes(req.Notes)
	if apiErr != nil {
		return fail(apiErr)
	}

	seq := len(s.refundsByPay[paymentID]) + 1
	r := &refund{
		id:        entityID("rfnd_", s.cfg.Seed, "refund", paymentID, itoa(seq)),
		paymentID: paymentID, amount: amount, currency: p.currency, receipt: req.Receipt, notes: notes,
		status: "pending", speed: req.Speed, createdAt: now, visibleAt: now,
	}
	immediate, final := false, "processed"
	after := s.cfg.scaled(lognormal(s.cfg.RefundDelayMedian, 0.3, s.cfg.Seed, "refund-delay", r.id))
	if rule != nil {
		switch rule.Outcome {
		case "processed":
			immediate = true
		case "pending_failed":
			final = "failed"
		}
		if rule.After != nil {
			after = rule.After.Duration // scripted: exact
		}
		if rule.VisibleAfter != nil {
			r.visibleAt = now.Add(rule.VisibleAfter.Duration)
		}
	}
	s.refunds[r.id] = r
	s.refundList = append(s.refundList, r)
	s.refundsByPay[paymentID] = append(s.refundsByPay[paymentID], r)

	// A pending refund already counts against the payment (assumed: the docs do not
	// say), so concurrent refunds cannot over-refund while one is pending.
	p.amountRefunded += amount
	s.recomputeRefundStatus(p)

	behavior := RefundBehavior{}
	if immediate {
		r.status = "processed"
	} else {
		refundID := r.id
		s.at(now.Add(after), func() { s.finishRefund(refundID, final) })
	}
	s.notifyRefund("refund.created", r, p)
	if immediate {
		s.notifyRefund("refund.processed", r, p)
	}
	if rule != nil {
		switch rule.Response {
		case "lost":
			behavior.Lost = true
		case "error_after":
			behavior.Error500 = true
		case "slow":
			behavior.Delay = 30 * time.Second
			if rule.ResponseDelay != nil {
				behavior.Delay = rule.ResponseDelay.Duration
			}
		}
	}
	if req.IdempotencyKey != "" {
		s.refundKeys[req.IdempotencyKey] = &refundKey{
			refundID: r.id, fingerprint: fingerprint, at: now, inFlight: behavior.Delay > 0,
		}
		if behavior.Delay > 0 {
			behavior.keyToClear = req.IdempotencyKey
		}
	}
	return refundView(r), behavior, nil
}

// recomputeRefundStatus derives the payment's refund fields from its refunded amount.
// The caller holds the lock.
func (s *Store) recomputeRefundStatus(p *payment) {
	switch {
	case p.amountRefunded <= 0:
		p.refundStatus, p.status = "", "captured"
	case p.amountRefunded >= p.amount:
		p.refundStatus, p.status = "full", "refunded"
	default:
		p.refundStatus, p.status = "partial", "captured"
	}
}

// finishRefund moves a pending refund to its final provider status. A failed refund
// gives the money back to the refundable amount (assumed; the docs do not say).
func (s *Store) finishRefund(id, final string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.refunds[id]
	if r == nil || r.status != "pending" {
		return
	}
	r.status = final
	p := s.payments[r.paymentID]
	if final == "failed" {
		p.amountRefunded -= r.amount
		s.recomputeRefundStatus(p)
	}
	s.notifyRefund("refund."+final, r, p)
}

// notifyRefund builds a refund event from the state just committed.
func (s *Store) notifyRefund(name string, r *refund, p *payment) {
	if s.notifier == nil {
		return
	}
	refundSnapshot, paymentSnapshot := refundView(r), paymentView(p)
	at := s.now()
	event := Event{
		Name: name, Key: r.id, PaymentID: p.id, OrderID: p.orderID, CreatedAt: at,
		Body: buildRefundEvent(s.cfg.WebhookAccountID, name, at, &refundSnapshot, &paymentSnapshot),
	}
	if o := s.orders[p.orderID]; o != nil && o.plan != nil {
		if rule, ok := o.plan.Delivery[name]; ok {
			event.Rule = &rule
		}
	}
	s.notifier.Notify(event)
}

// ReleaseRefundKey marks a delayed refund response as sent, so the key no longer
// answers 409 "in progress".
func (s *Store) ReleaseRefundKey(key string) {
	if key == "" {
		return
	}
	s.mu.Lock()
	if entry := s.refundKeys[key]; entry != nil {
		entry.inFlight = false
	}
	s.mu.Unlock()
}

// GetRefund reads one refund. A refund still inside its visibility delay is not
// found (assumed: delayed visibility of the read endpoints).
func (s *Store) GetRefund(id string) (RefundView, *APIError) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.refunds[id]
	if r == nil || now.Before(r.visibleAt) {
		return RefundView{}, unknownID()
	}
	return refundView(r), nil
}

// ListPaymentRefunds lists the visible refunds of one payment, oldest first.
func (s *Store) ListPaymentRefunds(paymentID string) ([]RefundView, *APIError) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.payments[paymentID] == nil {
		return nil, unknownID()
	}
	var out []RefundView
	for _, r := range s.refundsByPay[paymentID] {
		if !now.Before(r.visibleAt) {
			out = append(out, refundView(r))
		}
	}
	return out, nil
}

// ListRefunds lists visible refunds across payments, newest first (GET /v1/refunds).
func (s *Store) ListRefunds(count, skip int, from, to int64) ([]RefundView, *APIError) {
	if count < 0 || count > 100 {
		return nil, badRequest("count must be between 0 and 100", "count")
	}
	if count == 0 {
		count = 10
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []RefundView
	skipped := 0
	for i := len(s.refundList) - 1; i >= 0 && len(out) < count; i-- {
		r := s.refundList[i]
		if now.Before(r.visibleAt) || (from > 0 && r.createdAt.Unix() < from) || (to > 0 && r.createdAt.Unix() > to) {
			continue
		}
		if skipped < skip {
			skipped++
			continue
		}
		out = append(out, refundView(r))
	}
	return out, nil
}

// ---- inspection ----

type StoreStats struct {
	Orders         map[string]int `json:"orders_by_status"`
	Payments       map[string]int `json:"payments_by_status"`
	Refunds        map[string]int `json:"refunds_by_status"`
	OrderCount     int            `json:"orders"`
	PaymentCount   int            `json:"payments"`
	RefundCount    int            `json:"refunds"`
	AmountRefunded int64          `json:"amount_refunded_paise"`
	AmountCaptured int64          `json:"amount_captured_paise"`
}

// Stats returns the number of orders, payments and refunds by status.
func (s *Store) Stats() StoreStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	stats := StoreStats{
		Orders: map[string]int{}, Payments: map[string]int{}, Refunds: map[string]int{},
		OrderCount: len(s.orders), PaymentCount: len(s.payments), RefundCount: len(s.refunds),
	}
	for _, o := range s.orders {
		stats.Orders[o.status]++
	}
	for _, p := range s.payments {
		stats.Payments[p.status]++
		if p.captured {
			stats.AmountCaptured += p.amount
			stats.AmountRefunded += p.amountRefunded
		}
	}
	for _, r := range s.refunds {
		stats.Refunds[r.status]++
	}
	return stats
}

// ---- helpers ----

// unknownID builds the error returned for an ID that does not exist.
func unknownID() *APIError {
	e := badRequest("The id provided does not exist", "")
	e.Source, e.Step, e.Reason = "business", "payment_initiation", "input_validation_failed"
	return e
}

// parseAmount parses an amount given as a JSON number into whole paise.
func parseAmount(n json.Number) (int64, error) {
	if n == "" {
		return 0, fmt.Errorf("amount is empty")
	}
	value, err := strconv.ParseInt(n.String(), 10, 64)
	if err != nil {
		return 0, err
	}
	if value < 0 {
		return 0, fmt.Errorf("amount is negative")
	}
	return value, nil
}

// parseNotes checks the notes limits (at most 15 pairs, 256 characters per value) and converts them to strings.
func parseNotes(raw map[string]any) (map[string]string, *APIError) {
	if len(raw) > 15 {
		return nil, badRequest("notes: a maximum of 15 key-value pairs is allowed", "notes")
	}
	notes := make(map[string]string, len(raw))
	for key, value := range raw {
		text := fmt.Sprint(value)
		if len(text) > 256 {
			return nil, badRequest("notes: each value may be at most 256 characters", "notes")
		}
		notes[key] = text
	}
	return notes, nil
}
