package main

// Phase 16 proves that a PAID order is re-entrant: if inventory confirmation
// fails, a later /pay call retries settlement without charging or moving stock twice.
func phase16() phaseDefinition {
	return phaseDefinition{16, "Inventory settlement recovery", "Stops inventory after checkout, lets payment succeed and order become PAID, then restarts inventory and verifies a second /pay repairs the missing confirmation.", func(p *phaseContext) {
		if p.suite.gatewayMode != "success" {
			p.skip("This recovery window requires GATEWAY_MODE=success so the order reaches PAID before confirmation.")
			return
		}
		if !p.suite.canControlServices() {
			p.skip("Run the correctness binary from the host with COMPOSE_FILE set so it can stop and restart inventory-service deterministically.")
			return
		}

		product := p.productID(26)
		before := p.inventory(product)
		_, checkout, checkoutErr := p.checkout("phase16-"+newUUID(), []map[string]any{item(product, 2)})
		p.check("checkout reserves inventory", checkoutErr == nil && checkout.OrderID != "", "order is ready for payment")

		if err := p.suite.compose("stop", "inventory-service"); err != nil {
			p.fail("stop inventory before payment", err.Error())
			return
		}
		inventoryStopped := true
		defer func() {
			if inventoryStopped {
				_ = p.suite.compose("start", "inventory-service")
			}
		}()

		status, _, _ := p.payOrder(checkout.OrderID)
		p.check("failed confirmation returns service unavailable", status == 503, statusDetail(status, nil))
		p.check("payment succeeded before inventory failure", p.scalar("SELECT count(*) FROM payments.payment_attempts WHERE order_id=$1 AND status='SUCCEEDED'", checkout.OrderID) == 1, "one successful payment")
		p.check("order remains paid after confirmation failure", p.text("SELECT status FROM orders.orders WHERE id=$1", checkout.OrderID) == "PAID", "payment truth is retained")
		p.check("reservation remains reserved", p.text("SELECT status FROM inventory.reservations WHERE order_id=$1", checkout.OrderID) == "RESERVED", "confirmation has not happened")
		intermediate := p.inventory(product)
		p.check("stock is held but not consumed", intermediate.OnHand == before.OnHand && intermediate.Reserved == before.Reserved+2, "reserved +2; on_hand unchanged")

		if err := p.suite.compose("start", "inventory-service"); err != nil {
			p.fail("restart inventory for settlement retry", err.Error())
			return
		}
		inventoryStopped = false
		if !waitForInventory(p, product) {
			p.fail("inventory becomes ready", "inventory did not respond after restart")
			return
		}
		status, order, retryErr := p.payOrder(checkout.OrderID)
		after := p.inventory(product)
		p.check("re-entrant pay repairs confirmation", retryErr == nil && status == 200 && order.Status == "PAID", statusDetail(status, retryErr))
		p.check("reservation becomes confirmed", p.text("SELECT status FROM inventory.reservations WHERE order_id=$1", checkout.OrderID) == "CONFIRMED", "settlement repaired")
		p.check("retry creates no second payment", p.scalar("SELECT count(*) FROM payments.payment_attempts WHERE order_id=$1", checkout.OrderID) == 1, "one payment attempt")
		p.check("stock moves exactly once", after.OnHand == before.OnHand-2 && after.Reserved == before.Reserved, "on_hand -2; reserved restored")
	}}
}
