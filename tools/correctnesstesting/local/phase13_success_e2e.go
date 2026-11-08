package main

import "time"

// Phase 13 validates the complete successful business transaction across all services.
func phase13() phaseDefinition {
	return phaseDefinition{13, "Successful checkout end to end", "Runs the checkout saga through payment and inventory confirmation, then cross-checks order, payment, reservation, and stock records.", func(p *phaseContext) {
		if p.suite.gatewayMode != "success" {
			p.skip("Run with GATEWAY_MODE=success to exercise the deterministic success path.")
			return
		}
		product := p.productID(15)
		before := p.inventory(product)
		_, checkout, _ := p.checkout("phase13-"+newUUID(), []map[string]any{item(product, 2)})
		status, order, err := p.awaitOrder(checkout.OrderID, 30*time.Second)
		// The order turns terminal before the saga's last inventory step lands.
		p.check("saga finishes its inventory step", p.waitScalar(30*time.Second, 1, "SELECT count(*) FROM orchestrator.sagas WHERE id=$1 AND status='COMPLETED'", checkout.OrderID) == 1, "saga status COMPLETED")
		after := p.inventory(product)
		p.check("order becomes paid", err == nil && status == 200 && order.Status == "PAID", statusDetail(status, err))
		p.check("one successful payment exists", p.scalar("SELECT count(*) FROM payments.payment_attempts WHERE order_id=$1 AND status='SUCCEEDED'", checkout.OrderID) == 1, "payment must succeed")
		p.check("reservation is confirmed", p.text("SELECT status FROM inventory.reservations WHERE order_id=$1", checkout.OrderID) == "CONFIRMED", "reservation terminal state")
		p.check("confirmed stock is accounted once", after.OnHand == before.OnHand-2 && after.Reserved == before.Reserved, "on_hand -2, reserved restored")
	}}
}
