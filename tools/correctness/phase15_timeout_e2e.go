package main

import "time"

// Phase 15 must run in a fresh stack configured for deterministic gateway timeout.
func phase15() phaseDefinition {
	return phaseDefinition{15, "Ambiguous payment timeout", "Checks that an unknown gateway outcome keeps the order pending and its stock reserved for later reconciliation.", func(p *phaseContext) {
		if p.suite.gatewayMode != "timeout" {
			p.skip("Run a fresh disposable stack with GATEWAY_MODE=timeout to execute this phase.")
			return
		}
		product := p.productID(17)
		before := p.inventory(product)
		_, checkout, _ := p.checkout("phase15-"+newUUID(), []map[string]any{item(product, 2)})
		status, order, err := p.awaitOrder(checkout.OrderID, 8*time.Second)
		after := p.inventory(product)
		p.check("timeout keeps order pending", err == nil && status == 200 && order.Status == "PENDING_PAYMENT", statusDetail(status, err))
		p.check("payment is unknown", p.text("SELECT status FROM payments.payment_attempts WHERE order_id=$1", checkout.OrderID) == "UNKNOWN", "ambiguous terminal payment")
		p.check("reservation remains reserved", p.text("SELECT status FROM inventory.reservations WHERE order_id=$1", checkout.OrderID) == "RESERVED", "do not release uncertain stock")
		p.check("stock remains held", after.Reserved == before.Reserved+2 && after.OnHand == before.OnHand, "reserved +2")
	}}
}
