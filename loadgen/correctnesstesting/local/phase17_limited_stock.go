package main

import (
	"sync"
	"time"
)

// Phase 17 applies high contention to a deliberately small stock pool.
func phase17() phaseDefinition {
	return phaseDefinition{17, "Concurrent limited-stock checkout", "Runs 50 distinct checkouts against ten units and verifies exactly ten winners, no oversell, and no negative counters.", func(p *phaseContext) {
		product := p.productID(20)
		p.setStock(product, 10)
		const workers = 50
		statuses := make([]int, workers)
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				statuses[i], _, _ = p.checkout("phase17-"+newUUID(), []map[string]any{item(product, 1)})
			}(i)
		}
		wg.Wait()
		created, conflicts := 0, 0
		for _, status := range statuses {
			if status == 202 {
				created++
			}
			if status == 409 {
				conflicts++
			}
		}
		stock := p.inventory(product)
		p.check("exactly available stock wins", created == 10 && conflicts == workers-10, "expected 10 created and 40 conflicts")
		p.check("stock is not oversold", stock.OnHand == 10 && stock.Reserved == 10 && stock.Allocated == 0, "on_hand=10 reserved=10 allocated=0")
		p.check("ten orders were created", p.waitScalar(20*time.Second, 10, `SELECT count(*) FROM orders.order_items WHERE product_id=$1`, product) == 10, "one order per unit")
	}}
}
