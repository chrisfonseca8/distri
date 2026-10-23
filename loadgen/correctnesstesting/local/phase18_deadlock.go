package main

import "sync"

// Phase 18 reverses input order to expose inconsistent database lock ordering.
func phase18() phaseDefinition {
	return phaseDefinition{18, "Multi-item deadlock resistance", "Submits concurrent two-item carts in opposite input order and verifies canonical locking prevents deadlocks and timeouts.", func(p *phaseContext) {
		left, right := p.productID(21), p.productID(22)
		p.setStock(left, 20)
		p.setStock(right, 20)
		const workers = 20
		statuses := make([]int, workers)
		errs := make([]error, workers)
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				cart := []map[string]any{item(left, 1), item(right, 1)}
				if i%2 == 1 {
					cart[0], cart[1] = cart[1], cart[0]
				}
				statuses[i], _, errs[i] = p.checkout("phase18-"+newUUID(), cart)
			}(i)
		}
		wg.Wait()
		created, failures := 0, 0
		for i, status := range statuses {
			if status == 202 && errs[i] == nil {
				created++
			} else {
				failures++
			}
		}
		leftStock, rightStock := p.inventory(left), p.inventory(right)
		p.check("all opposite-order carts complete", created == workers && failures == 0, "no deadlocks, transport timeouts, or conflicts")
		p.check("both products reserved exactly", leftStock.Reserved == 20 && rightStock.Reserved == 20, "each product reserved 20")
	}}
}
