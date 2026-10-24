package sim

import (
	"encoding/json"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// manualClock lets tests run scheduled transitions without waiting.
type manualClock struct {
	mu    sync.Mutex
	now   time.Time
	tasks []task
	seq   uint64
}

func newClock() *manualClock {
	return &manualClock{now: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)}
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *manualClock) At(at time.Time, fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	c.tasks = append(c.tasks, task{at: at, seq: c.seq, fn: fn})
}

// Advance moves time forward and runs every transition that became due, in order.
func (c *manualClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []task
	rest := c.tasks[:0]
	for _, t := range c.tasks {
		if !t.at.After(c.now) {
			due = append(due, t)
		} else {
			rest = append(rest, t)
		}
	}
	c.tasks = rest
	c.mu.Unlock()
	sort.Slice(due, func(i, j int) bool {
		if due[i].at.Equal(due[j].at) {
			return due[i].seq < due[j].seq
		}
		return due[i].at.Before(due[j].at)
	})
	for _, t := range due {
		t.fn()
	}
}

func testConfig() Config {
	return Config{
		KeyID: "rzp_test_x", KeySecret: "s", Seed: 7, TimeScale: 1, AutoCapture: true,
		ScenarioMix: []WeightedScenario{{Name: ScenarioCapture, Weight: 1}},
		APILatency:  "zero", PayDelayMedian: 2 * time.Second, CaptureDelayMedian: 500 * time.Millisecond,
		RefundDelayMedian: time.Second,
	}
}

func newTestStore(cfg Config) (*Store, *manualClock) {
	clock := newClock()
	store := NewStore(cfg, clock.At)
	store.now = clock.Now
	return store, clock
}

func newOrder(amount string, receipt string) OrderRequest {
	return OrderRequest{Amount: json.Number(amount), Currency: "INR", Receipt: receipt}
}

func TestCreateOrderValidation(t *testing.T) {
	store, _ := newTestStore(testConfig())
	long := make([]byte, 41)
	for i := range long {
		long[i] = 'a'
	}
	manyNotes := map[string]any{}
	for i := 0; i < 16; i++ {
		manyNotes["k"+itoa(i)] = "v"
	}
	tests := []struct {
		name string
		req  OrderRequest
		want string
	}{
		{"missing amount", OrderRequest{Currency: "INR"}, "amount"},
		{"float amount", newOrder("100.5", "r"), "amount"},
		{"negative amount", newOrder("-5", "r"), "amount"},
		{"below minimum", newOrder("99", "r"), "amount"},
		{"missing currency", OrderRequest{Amount: "500"}, "currency"},
		{"long receipt", newOrder("500", string(long)), "receipt"},
		{"too many notes", OrderRequest{Amount: "500", Currency: "INR", Notes: manyNotes}, "notes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, apiErr := store.CreateOrder(test.req)
			if apiErr == nil || apiErr.Status != 400 || apiErr.Field != test.want {
				t.Fatalf("err = %+v, want 400 on field %q", apiErr, test.want)
			}
		})
	}
	if _, apiErr := store.CreateOrder(newOrder("100", "ok")); apiErr != nil {
		t.Fatalf("minimum amount rejected: %v", apiErr)
	}
}

func TestDuplicateReceiptCompatibility(t *testing.T) {
	t.Run("observed behaviour: duplicate is accepted as a second unpaid order", func(t *testing.T) {
		store, _ := newTestStore(testConfig())
		first, _ := store.CreateOrder(newOrder("1000", "rcpt-1"))
		second, apiErr := store.CreateOrder(newOrder("1000", "rcpt-1"))
		if apiErr != nil || first.ID == second.ID {
			t.Fatalf("second=%+v err=%v, want a distinct order", second, apiErr)
		}
		items, _ := store.ListOrders("rcpt-1", 0, 0)
		if len(items) != 2 || items[0].ID != second.ID {
			t.Fatalf("list by receipt = %d items, want both, newest first", len(items))
		}
		store.mu.Lock()
		defer store.mu.Unlock()
		if store.orders[second.ID].scenario != ScenarioNone {
			t.Fatal("a duplicate order must not be paid by the simulated customer")
		}
	})
	t.Run("documented behaviour: duplicate is rejected", func(t *testing.T) {
		cfg := testConfig()
		cfg.EnforceReceiptUnique = true
		store, _ := newTestStore(cfg)
		store.CreateOrder(newOrder("1000", "rcpt-1"))
		if _, apiErr := store.CreateOrder(newOrder("1000", "rcpt-1")); apiErr == nil || apiErr.Status != 400 {
			t.Fatalf("err = %v, want 400 duplicate", apiErr)
		}
	})
}

func TestLifecycleKeepsEveryReadEndpointConsistent(t *testing.T) {
	store, clock := newTestStore(testConfig())
	created, _ := store.CreateOrder(newOrder("25000", "rcpt-life"))
	if created.Status != "created" || created.AmountDue != 25000 {
		t.Fatalf("new order = %+v", created)
	}

	clock.Advance(30 * time.Second) // customer attempts; authorization then auto-capture is scheduled
	payments, _ := store.OrderPayments(created.ID)
	if len(payments) != 1 || payments[0].Status != "authorized" || payments[0].Captured {
		t.Fatalf("after the attempt: %+v", payments)
	}
	got, _ := store.GetOrder(created.ID)
	if got.Status != "attempted" || got.Attempts != 1 || got.AmountPaid != 0 {
		t.Fatalf("order after attempt = %+v", got)
	}

	clock.Advance(30 * time.Second) // auto-capture
	payment, _ := store.GetPayment(payments[0].ID)
	got, _ = store.GetOrder(created.ID)
	listed, _ := store.OrderPayments(created.ID)
	if payment.Status != "captured" || !payment.Captured || got.Status != "paid" || got.AmountPaid != 25000 ||
		len(listed) != 1 || listed[0].Status != "captured" {
		t.Fatalf("payment=%+v order=%+v listed=%+v", payment, got, listed)
	}
	if payment.OrderID != created.ID {
		t.Fatal("payment must reference its order")
	}
}

func TestScenarios(t *testing.T) {
	cases := []struct {
		scenario      string
		wantOrder     string
		wantPayments  int
		wantPayStatus string
	}{
		{ScenarioDecline, "attempted", 1, "failed"},
		{ScenarioNone, "created", 0, ""},
		{ScenarioAuthorizeOnly, "attempted", 1, "authorized"},
	}
	for _, c := range cases {
		t.Run(c.scenario, func(t *testing.T) {
			store, clock := newTestStore(testConfig())
			store.SetScenario("rcpt", c.scenario)
			o, _ := store.CreateOrder(newOrder("5000", "rcpt"))
			clock.Advance(10 * time.Minute)
			got, _ := store.GetOrder(o.ID)
			payments, _ := store.OrderPayments(o.ID)
			if got.Status != c.wantOrder || len(payments) != c.wantPayments {
				t.Fatalf("order=%s payments=%d", got.Status, len(payments))
			}
			if c.wantPayments == 1 && payments[0].Status != c.wantPayStatus {
				t.Fatalf("payment status = %s", payments[0].Status)
			}
			if c.scenario == ScenarioDecline && (payments[0].ErrorCode == nil || payments[0].ErrorReason == nil) {
				t.Fatal("a failed payment must carry error fields")
			}
		})
	}
}

func TestManualCaptureAccount(t *testing.T) {
	cfg := testConfig()
	cfg.AutoCapture = false
	store, clock := newTestStore(cfg)
	o, _ := store.CreateOrder(newOrder("5000", "rcpt"))
	clock.Advance(10 * time.Minute)
	payments, _ := store.OrderPayments(o.ID)
	if len(payments) != 1 || payments[0].Status != "authorized" {
		t.Fatalf("without auto-capture the payment stays authorized: %+v", payments)
	}
	if _, apiErr := store.CapturePayment(payments[0].ID, "4000", "INR"); apiErr == nil {
		t.Fatal("capturing a different amount must fail")
	}
	if _, apiErr := store.CapturePayment("pay_missing", "5000", "INR"); apiErr == nil {
		t.Fatal("capturing an unknown payment must fail")
	}
	captured, apiErr := store.CapturePayment(payments[0].ID, "5000", "INR")
	if apiErr != nil || captured.Status != "captured" {
		t.Fatalf("capture: %+v %v", captured, apiErr)
	}
	if got, _ := store.GetOrder(o.ID); got.Status != "paid" {
		t.Fatalf("order after manual capture = %s", got.Status)
	}
	if _, apiErr := store.CapturePayment(payments[0].ID, "5000", "INR"); apiErr == nil {
		t.Fatal("capturing twice must fail")
	}
}

func TestReceiptLookupLag(t *testing.T) {
	cfg := testConfig()
	cfg.LookupDelayMin, cfg.LookupDelayMax = 10*time.Second, 10*time.Second
	store, clock := newTestStore(cfg)
	o, _ := store.CreateOrder(newOrder("5000", "rcpt-lag"))
	if items, _ := store.ListOrders("rcpt-lag", 0, 0); len(items) != 0 {
		t.Fatal("a brand-new order must not be listed by receipt yet")
	}
	if _, apiErr := store.GetOrder(o.ID); apiErr != nil {
		t.Fatal("fetch by id is not delayed")
	}
	clock.Advance(11 * time.Second)
	if items, _ := store.ListOrders("rcpt-lag", 0, 0); len(items) != 1 {
		t.Fatal("the order must be listed once the lag has passed")
	}
}

func TestRefundRules(t *testing.T) {
	cfg := testConfig()
	cfg.EnforceRefundReceiptUnique = true // the documented error text; off by default
	store, clock := newTestStore(cfg)
	o, _ := store.CreateOrder(newOrder("10000", "rcpt-r"))
	clock.Advance(time.Minute) // customer attempts
	clock.Advance(time.Minute) // auto-capture
	pay, _ := store.OrderPayments(o.ID)
	id := pay[0].ID

	// not captured yet is covered by authorize_only below; here it is captured.
	if _, apiErr := store.CreateRefund(id, RefundRequest{Amount: "0"}); apiErr == nil {
		t.Fatal("zero amount must be rejected")
	}
	if _, apiErr := store.CreateRefund(id, RefundRequest{Amount: "20000"}); apiErr == nil {
		t.Fatal("over-refund must be rejected")
	}
	first, apiErr := store.CreateRefund(id, RefundRequest{Amount: "4000", Receipt: "ref-1"})
	if apiErr != nil || first.Status != "pending" || first.Amount != 4000 {
		t.Fatalf("first refund: %+v %v", first, apiErr)
	}
	if _, apiErr := store.CreateRefund(id, RefundRequest{Amount: "100", Receipt: "ref-1"}); apiErr == nil {
		t.Fatal("a repeated refund receipt on the same payment must be rejected")
	}
	partial, _ := store.GetPayment(id)
	if partial.AmountRefunded != 4000 || partial.RefundStatus == nil || *partial.RefundStatus != "partial" || partial.Status != "captured" {
		t.Fatalf("partial payment = %+v", partial)
	}
	rest, apiErr := store.CreateRefund(id, RefundRequest{})
	if apiErr != nil || rest.Amount != 6000 {
		t.Fatalf("an omitted amount refunds the remainder: %+v %v", rest, apiErr)
	}
	full, _ := store.GetPayment(id)
	if full.Status != "refunded" || *full.RefundStatus != "full" || full.AmountRefunded != 10000 {
		t.Fatalf("fully refunded payment = %+v", full)
	}
	if _, apiErr := store.CreateRefund(id, RefundRequest{}); apiErr == nil {
		t.Fatal("refunding a fully refunded payment must fail")
	}
	clock.Advance(time.Minute)
	if stats := store.Stats(); stats.Refunds["processed"] != 2 {
		t.Fatalf("pending refunds must complete: %+v", stats.Refunds)
	}
}

func TestRefundRequiresCapture(t *testing.T) {
	store, clock := newTestStore(testConfig())
	store.SetScenario("rcpt", ScenarioAuthorizeOnly)
	o, _ := store.CreateOrder(newOrder("5000", "rcpt"))
	clock.Advance(time.Minute)
	pay, _ := store.OrderPayments(o.ID)
	if _, apiErr := store.CreateRefund(pay[0].ID, RefundRequest{}); apiErr == nil {
		t.Fatal("an authorized (uncaptured) payment cannot be refunded")
	}
}

func TestConcurrentRefundsNeverExceedTheCapturedAmount(t *testing.T) {
	store, clock := newTestStore(testConfig())
	o, _ := store.CreateOrder(newOrder("10000", "rcpt-c"))
	clock.Advance(time.Minute) // customer attempts
	clock.Advance(time.Minute) // auto-capture
	pay, _ := store.OrderPayments(o.ID)

	var accepted atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 80; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, apiErr := store.CreateRefund(pay[0].ID, RefundRequest{Amount: "300"}); apiErr == nil {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	payment, _ := store.GetPayment(pay[0].ID)
	if payment.AmountRefunded > 10000 || accepted.Load() != 33 || payment.AmountRefunded != 33*300 {
		t.Fatalf("accepted=%d refunded=%d, want 33 refunds totalling 9900 and never more than 10000", accepted.Load(), payment.AmountRefunded)
	}
}

func TestDecisionsAreReproducibleAndIndependentOfCreationOrder(t *testing.T) {
	mix := []WeightedScenario{{ScenarioCapture, 60}, {ScenarioDecline, 30}, {ScenarioNone, 10}}
	receipts := make([]string, 200)
	for i := range receipts {
		receipts[i] = "00000000-0000-4000-8000-" + itoa(100000+i)
	}
	run := func(sequence []int) (map[string]string, map[string]string) {
		cfg := testConfig()
		cfg.ScenarioMix = mix
		store, _ := newTestStore(cfg)
		scenarios, ids := map[string]string{}, map[string]string{}
		for _, i := range sequence {
			view, _ := store.CreateOrder(OrderRequest{Amount: "5000", Currency: "INR", Receipt: receipts[i]})
			store.mu.Lock()
			scenarios[receipts[i]] = store.orders[view.ID].scenario
			store.mu.Unlock()
			ids[receipts[i]] = view.ID
		}
		return scenarios, ids
	}
	forward := make([]int, len(receipts))
	backward := make([]int, len(receipts))
	for i := range receipts {
		forward[i], backward[i] = i, len(receipts)-1-i
	}
	s1, id1 := run(forward)
	s2, id2 := run(backward)
	counts := map[string]int{}
	for receipt := range s1 {
		if s1[receipt] != s2[receipt] || id1[receipt] != id2[receipt] {
			t.Fatalf("receipt %s: scenario/id changed with creation order", receipt)
		}
		counts[s1[receipt]]++
	}
	if counts[ScenarioCapture] < 90 || counts[ScenarioDecline] < 40 || counts[ScenarioNone] < 5 {
		t.Fatalf("mix = %v, want roughly 60/30/10 of 200", counts)
	}
}

func TestSchedulerRunsTasksInTimeOrder(t *testing.T) {
	scheduler := NewScheduler()
	ctx, cancel := contextWithCancel(t)
	defer cancel()
	go scheduler.Run(ctx)

	var mu sync.Mutex
	var ran []int
	done := make(chan struct{})
	now := time.Now()
	scheduler.At(now.Add(60*time.Millisecond), func() { mu.Lock(); ran = append(ran, 3); mu.Unlock(); close(done) })
	scheduler.At(now.Add(20*time.Millisecond), func() { mu.Lock(); ran = append(ran, 1); mu.Unlock() })
	scheduler.At(now.Add(40*time.Millisecond), func() { mu.Lock(); ran = append(ran, 2); mu.Unlock() })
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not run the tasks")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ran) != 3 || ran[0] != 1 || ran[1] != 2 || ran[2] != 3 {
		t.Fatalf("order = %v, want [1 2 3]", ran)
	}
	if stats := scheduler.Stats(); stats.Executed != 3 {
		t.Fatalf("stats = %+v", stats)
	}
}
