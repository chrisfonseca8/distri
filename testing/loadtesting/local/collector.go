package main

import (
	"math"
	"strconv"
	"sync"
	"time"
)

type activity struct {
	current, max int64
	area         float64
	last         time.Time
}
type collector struct {
	mu                                                                         sync.Mutex
	started                                                                    time.Time
	offered, accepted, completed, paid, cancelled, pending                     int64
	checkout, pay, endToEnd                                                    []time.Duration
	checkoutMix, payMix                                                        StatusMix
	requests, workflows                                                        activity
	duplicateInjected, duplicateExtra, duplicateReplay200, duplicateMismatches int64
}

func (c *collector) duplicate(injected, extra, replay200, mismatches int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.duplicateInjected += injected
	c.duplicateExtra += extra
	c.duplicateReplay200 += replay200
	c.duplicateMismatches += mismatches
}

func newCollector() *collector {
	now := time.Now()
	return &collector{started: now, checkoutMix: newStatusMix(), payMix: newStatusMix(), requests: activity{last: now}, workflows: activity{last: now}}
}
func newStatusMix() StatusMix { return StatusMix{ByHTTPStatus: make(map[string]int64)} }
func (c *collector) update(a *activity, delta int64) {
	now := time.Now()
	a.area += float64(a.current) * now.Sub(a.last).Seconds()
	a.last = now
	a.current += delta
	if a.current > a.max {
		a.max = a.current
	}
}
func (c *collector) beginRequest()  { c.mu.Lock(); defer c.mu.Unlock(); c.update(&c.requests, 1) }
func (c *collector) endRequest()    { c.mu.Lock(); defer c.mu.Unlock(); c.update(&c.requests, -1) }
func (c *collector) beginWorkflow() { c.mu.Lock(); defer c.mu.Unlock(); c.update(&c.workflows, 1) }
func (c *collector) endWorkflow()   { c.mu.Lock(); defer c.mu.Unlock(); c.update(&c.workflows, -1) }
func (c *collector) offeredOne()    { c.mu.Lock(); defer c.mu.Unlock(); c.offered++ }
func (c *collector) acceptedOne()   { c.mu.Lock(); defer c.mu.Unlock(); c.accepted++ }
func (c *collector) pendingOne()    { c.mu.Lock(); defer c.mu.Unlock(); c.pending++ }
func (c *collector) completedOne(status string, latency time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.completed++
	if status == "PAID" {
		c.paid++
	} else {
		c.cancelled++
	}
	c.endToEnd = append(c.endToEnd, latency)
}
func (c *collector) checkoutResult(status int, latency time.Duration, transport, decode error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checkout = append(c.checkout, latency)
	recordStatus(&c.checkoutMix, status, transport, decode)
}
func (c *collector) payResult(status int, latency time.Duration, transport, decode error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pay = append(c.pay, latency)
	recordStatus(&c.payMix, status, transport, decode)
}
func recordStatus(m *StatusMix, status int, transport, decode error) {
	if transport != nil {
		m.TransportErrors++
		return
	}
	if decode != nil {
		m.DecodeErrors++
		return
	}
	m.ByHTTPStatus[strconv.Itoa(status)]++
	if status >= 200 && status < 300 {
		m.Accepted2xx++
	}
	if status == 409 {
		m.Business409++
	}
	if status >= 500 {
		m.Server5xx++
	}
}
func (c *collector) snapshot(elapsed time.Duration) ClientMetrics {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.update(&c.requests, 0)
	c.update(&c.workflows, 0)
	seconds := elapsed.Seconds()
	if seconds <= 0 {
		seconds = 1
	}
	meanE2E := SummarizeLatencies(c.endToEnd).MeanMS / 1000
	completedRate := float64(c.completed) / seconds
	observed := c.workflows.area / seconds
	expected := completedRate * meanE2E
	difference := 0.0
	if expected > 0 {
		difference = math.Abs(observed-expected) / expected * 100
	}
	return ClientMetrics{Offered: c.offered, OfferedRate: float64(c.offered) / seconds, Accepted: c.accepted, AcceptedRate: float64(c.accepted) / seconds, Completed: c.completed, CompletedRate: completedRate, Paid: c.paid, Cancelled: c.cancelled, PendingAtDrain: c.pending, CheckoutLatency: SummarizeLatencies(c.checkout), PayLatency: SummarizeLatencies(c.pay), EndToEndLatency: SummarizeLatencies(c.endToEnd), CheckoutStatusMix: c.checkoutMix, PayStatusMix: c.payMix, InFlight: InFlightSummary{MeanRequests: c.requests.area / seconds, MaxRequests: c.requests.max, MeanWorkflows: observed, MaxWorkflows: c.workflows.max}, LittleLaw: LittleLawCheck{ObservedMeanInFlight: observed, ExpectedMeanInFlight: expected, DifferencePercent: difference}, Duplicate: DuplicateMetrics{InjectedWorkflows: c.duplicateInjected, ExtraRequests: c.duplicateExtra, Replay200: c.duplicateReplay200, Mismatches: c.duplicateMismatches}}
}
