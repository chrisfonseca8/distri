package main

// Phase 19 confirms that retrying a terminal order cannot repeat payment or stock movement.
func phase19() phaseDefinition {
	return phaseDefinition{19, "Terminal order state idempotency", "Retries payment settlement on a terminal order and verifies stable state, one payment, and one inventory movement.", func(p *phaseContext) {
		if p.suite.gatewayMode != "success" {
			p.skip("The default terminal-state assertion is deterministic under GATEWAY_MODE=success; decline is covered by phase 14.")
			return
		}
		product := p.productID(23)
		before := p.inventory(product)
		_, checkout, _ := p.checkout("phase19-"+newUUID(), []map[string]any{item(product, 1)})
		firstStatus, first, _ := p.payOrder(checkout.OrderID)
		secondStatus, second, _ := p.payOrder(checkout.OrderID)
		after := p.inventory(product)
		p.check("terminal retry remains paid", firstStatus == 200 && secondStatus == 200 && first.Status == "PAID" && second.Status == "PAID", "stable terminal state")
		p.check("terminal retry creates no payment", p.scalar("SELECT count(*) FROM payments.payment_attempts WHERE order_id=$1", checkout.OrderID) == 1, "one payment attempt")
		p.check("terminal retry moves stock once", after.OnHand == before.OnHand-1 && after.Reserved == before.Reserved, "single-unit movement")
		p.note("EXPIRED is not externally triggerable in the current API; its release path needs an expiry worker or test clock before black-box coverage is possible.")
	}}
}
