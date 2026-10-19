package main

import (
	"sync"
	"time"
)

// Phase 10 races payment creation to prove the order-level unique claim is effective.
func phase10() phaseDefinition {
	return phaseDefinition{10, "Payment idempotency and concurrency", "Sends concurrent create-payment requests for one order and verifies one database row, one payment ID, and one asynchronous result.", func(p *phaseContext) {
		orderID := newUUID()
		const workers = 16
		statuses := make([]int, workers)
		ids := make([]string, workers)
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(i int) { defer wg.Done(); statuses[i], ids[i], _ = paymentID(p, orderID) }(i)
		}
		wg.Wait()
		created, reused, same := 0, 0, true
		for i, status := range statuses {
			if status == 201 {
				created++
			}
			if status == 200 {
				reused++
			}
			if ids[i] != ids[0] {
				same = false
			}
		}
		p.check("one request creates and retries reuse", created == 1 && reused == workers-1 && same, "expected one 201, remaining 200, one ID")
		p.check("one payment row exists", p.scalar("SELECT count(*) FROM payments.payment_attempts WHERE order_id=$1", orderID) == 1, "database uniqueness")
		terminal := p.waitPayment(ids[0], 7*time.Second)
		p.check("payment leaves CREATED/PENDING", terminal.Status != "CREATED" && terminal.Status != "PENDING", terminal.Status)
	}}
}

func paymentID(p *phaseContext, orderID string) (int, string, error) {
	status, result, err := p.createPayment(orderID, 9900, "INR")
	return status, result.ID, err
}
