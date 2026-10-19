package main

import "sync"

// Phase 7 races the same checkout key to verify exactly-once order creation.
func phase07() phaseDefinition {
	return phaseDefinition{7, "Checkout idempotency and concurrency", "Checks sequential response replay, canonical item ordering, key/body conflicts, and concurrent retries of one logical checkout.", func(p *phaseContext) {
		product := p.productID(9)
		key := "phase07-sequential-" + newUUID()
		firstStatus, first, _ := p.checkout(key, []map[string]any{item(product, 1)})
		secondStatus, second, _ := p.checkout(key, []map[string]any{item(product, 1)})
		conflictStatus, _, _ := p.checkout(key, []map[string]any{item(product, 2)})
		p.check("sequential retry replays order", firstStatus == 201 && secondStatus == 200 && first.OrderID == second.OrderID, "one stable order ID")
		p.check("key reuse with different body is rejected", conflictStatus == 422, statusDetail(conflictStatus, nil))

		left, right := p.productID(10), p.productID(11)
		canonicalKey := "phase07-canonical-" + newUUID()
		_, canonical1, _ := p.checkout(canonicalKey, []map[string]any{item(left, 1), item(right, 1)})
		canonicalStatus, canonical2, _ := p.checkout(canonicalKey, []map[string]any{item(right, 1), item(left, 1)})
		p.check("item order is canonicalized", canonicalStatus == 200 && canonical1.OrderID == canonical2.OrderID, "reordered cart is same request")

		concurrentProduct := p.productID(12)
		before := p.inventory(concurrentProduct)
		concurrentKey := "phase07-race-" + newUUID()
		const workers = 12
		statuses := make([]int, workers)
		ids := make([]string, workers)
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(i int) { defer wg.Done(); statuses[i], ids[i], _ = checkoutID(p, concurrentKey, concurrentProduct) }(i)
		}
		wg.Wait()
		created, replayed, same := 0, 0, true
		for i, status := range statuses {
			if status == 201 {
				created++
			}
			if status == 200 {
				replayed++
			}
			if ids[i] != ids[0] {
				same = false
			}
		}
		after := p.inventory(concurrentProduct)
		p.check("concurrent retries have one creator", created == 1 && replayed == workers-1 && same, "expected one 201, remaining 200, one order ID")
		p.check("concurrent retries reserve once", after.Reserved-before.Reserved == 1, "reserved delta must equal 1")
	}}
}

func checkoutID(p *phaseContext, key, product string) (int, string, error) {
	status, result, err := p.checkout(key, []map[string]any{item(product, 1)})
	return status, result.OrderID, err
}
