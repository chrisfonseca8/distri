package main

import "time"

// Phase 22 is the final database-wide consistency audit after all prior mutations.
func phase22() phaseDefinition {
	return phaseDefinition{22, "Database invariants", "Scans all three schemas for negative/oversold stock, duplicate business keys, and cross-service terminal-state contradictions.", func(p *phaseContext) {
		drained := p.waitForActionableJobs(10 * time.Second)
		p.check("actionable background jobs drain before audit", drained, "timed out waiting for the worker backlog")
		checks := []struct{ name, query string }{
			{"inventory counters stay within bounds", `SELECT count(*) FROM inventory.inventories WHERE on_hand < 0 OR reserved < 0 OR allocated < 0 OR reserved + allocated > on_hand`},
			{"one reservation per order", `SELECT count(*) FROM (SELECT order_id FROM inventory.reservations GROUP BY order_id HAVING count(*) > 1) x`},
			{"one payment per order", `SELECT count(*) FROM (SELECT order_id FROM payments.payment_attempts GROUP BY order_id HAVING count(*) > 1) x`},
			{"one product line per order", `SELECT count(*) FROM (SELECT order_id, product_id FROM orders.order_items GROUP BY order_id, product_id HAVING count(*) > 1) x`},
			{"paid orders have successful payment", `SELECT count(*) FROM orders.orders o LEFT JOIN payments.payment_attempts p ON p.order_id=o.id AND p.status='SUCCEEDED' WHERE o.status='PAID' AND p.id IS NULL`},
			{"cancelled orders have failed payment", `SELECT count(*) FROM orders.orders o LEFT JOIN payments.payment_attempts p ON p.order_id=o.id AND p.status='FAILED' WHERE o.status='CANCELLED' AND p.id IS NULL`},
			{"paid reservations are confirmed", `SELECT count(*) FROM orders.orders o JOIN inventory.reservations r ON r.order_id=o.id WHERE o.status='PAID' AND r.status<>'CONFIRMED'`},
			{"cancelled reservations are released", `SELECT count(*) FROM orders.orders o JOIN inventory.reservations r ON r.order_id=o.id WHERE o.status='CANCELLED' AND r.status<>'RELEASED'`},
			{"unknown payments retain pending orders", `SELECT count(*) FROM payments.payment_attempts p JOIN orders.orders o ON o.id=p.order_id WHERE p.status='UNKNOWN' AND o.status<>'PENDING_PAYMENT'`},
		}
		for _, check := range checks {
			violations := p.scalar(check.query)
			p.check(check.name, violations == 0, "violations="+fmtInt(violations))
		}
	}}
}
