package main

// Phase 14 must run in a fresh stack configured for deterministic gateway decline.
func phase14() phaseDefinition {
	return phaseDefinition{14, "Declined payment end to end", "Checks that a declined payment cancels the order and releases, rather than consumes, reserved inventory.", func(p *phaseContext) {
		if p.suite.gatewayMode != "decline" {
			p.skip("Run a fresh disposable stack with GATEWAY_MODE=decline to execute this phase.")
			return
		}
		product := p.productID(16)
		before := p.inventory(product)
		_, checkout, _ := p.checkout("phase14-"+newUUID(), []map[string]any{item(product, 2)})
		status, order, err := p.payOrder(checkout.OrderID)
		after := p.inventory(product)
		p.check("decline cancels order", err == nil && status == 200 && order.Status == "CANCELLED", statusDetail(status, err))
		p.check("payment is failed", p.text("SELECT status FROM payments.payment_attempts WHERE order_id=$1", checkout.OrderID) == "FAILED", "terminal payment")
		p.check("reservation is released", p.text("SELECT status FROM inventory.reservations WHERE order_id=$1", checkout.OrderID) == "RELEASED", "terminal reservation")
		p.check("decline consumes no stock", after == before, "inventory returns to start")
	}}
}
