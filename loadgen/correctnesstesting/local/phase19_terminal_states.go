package main

import "time"

// Phase 19 confirms that replaying a finished checkout cannot repeat payment or stock movement.
func phase19() phaseDefinition {
	return phaseDefinition{19, "Terminal order state idempotency", "Replays the checkout of a PAID order and verifies the same order ID, stable state, one payment, and one inventory movement.", func(p *phaseContext) {
		if p.suite.gatewayMode != "success" {
			p.skip("The default terminal-state assertion is deterministic under GATEWAY_MODE=success; decline is covered by phase 14.")
			return
		}
		product := p.productID(23)
		before := p.inventory(product)
		key := "phase19-" + newUUID()
		items := []map[string]any{item(product, 1)}
		_, checkout, _ := p.checkout(key, items)
		status, order, err := p.awaitOrder(checkout.OrderID, 30*time.Second)
		p.check("order reaches paid", err == nil && status == 200 && order.Status == "PAID", statusDetail(status, err))
		p.waitScalar(30*time.Second, 1, "SELECT count(*) FROM orchestrator.sagas WHERE id=$1 AND status='COMPLETED'", checkout.OrderID)
		replayStatus, replay, _ := p.checkout(key, items)
		time.Sleep(2 * time.Second)
		_, again, _ := p.getOrder(checkout.OrderID)
		after := p.inventory(product)
		p.check("replay returns the same order", replayStatus == 202 && replay.OrderID == checkout.OrderID, "stable order ID")
		p.check("replay leaves the order paid", again.Status == "PAID", "stable terminal state")
		p.check("replay creates no payment", p.scalar("SELECT count(*) FROM payments.payment_attempts WHERE order_id=$1", checkout.OrderID) == 1, "one payment attempt")
		p.check("replay moves stock once", after.OnHand == before.OnHand-1 && after.Reserved == before.Reserved, "single-unit movement")
		p.note("EXPIRED is not externally triggerable in the current API; its release path needs an expiry worker or test clock before black-box coverage is possible.")
	}}
}
