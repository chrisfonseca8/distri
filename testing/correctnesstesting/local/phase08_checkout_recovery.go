package main

import "time"

// Phase 8 proves the saga survives a participant being down when its command is sent.
func phase08() phaseDefinition {
	return phaseDefinition{8, "Order command recovery", "Stops order-service, starts a checkout, waits for the saga sweeper to resend order.create, restarts order-service, and verifies one order is created and the saga completes.", func(p *phaseContext) {
		if p.suite.gatewayMode != "success" {
			p.skip("Run with GATEWAY_MODE=success so the recovered saga reaches PAID.")
			return
		}
		if !p.suite.canControlServices() {
			p.skip("Run the correctness binary from the host with COMPOSE_FILE set so it can stop and restart order-service deterministically.")
			return
		}

		product := p.productID(18)
		before := p.inventory(product)
		if err := p.suite.compose("stop", "order-service"); err != nil {
			p.fail("stop order-service", err.Error())
			return
		}
		orderStopped := true
		defer func() {
			if orderStopped {
				_ = p.suite.compose("start", "order-service")
			}
		}()

		status, checkout, err := p.checkout("phase08-"+newUUID(), []map[string]any{item(product, 1)})
		p.check("checkout is accepted while order-service is down", err == nil && status == 202 && checkout.OrderID != "", statusDetail(status, err))
		p.check("sweeper resends order.create", p.waitStepAttempts(checkout.OrderID, "CREATE_ORDER", 2, 3*time.Minute), "CREATE_ORDER attempts must reach 2")
		p.check("no order exists while the participant is down", p.scalar("SELECT count(*) FROM orders.orders WHERE id=$1", checkout.OrderID) == 0, "order creation is pending")

		if err := p.suite.compose("start", "order-service"); err != nil {
			p.fail("restart order-service", err.Error())
			return
		}
		orderStopped = false
		status, order, err := p.awaitOrder(checkout.OrderID, 2*time.Minute)
		p.check("saga completes after recovery", err == nil && status == 200 && order.Status == "PAID", statusDetail(status, err))
		p.check("resends create exactly one order", p.scalar("SELECT count(*) FROM orders.orders WHERE id=$1", checkout.OrderID) == 1, "one order row")
		p.check("saga is completed", p.waitScalar(30*time.Second, 1, "SELECT count(*) FROM orchestrator.sagas WHERE id=$1 AND status='COMPLETED'", checkout.OrderID) == 1, "COMPLETED saga")
		after := p.inventory(product)
		p.check("stock moves exactly once", after.OnHand == before.OnHand-1 && after.Reserved == before.Reserved, "on_hand -1; reserved restored")
	}}
}

// waitForInventory polls inventory-service until it answers, for up to 20 seconds.
func waitForInventory(p *phaseContext, productID string) bool {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		status, _, err := p.request("GET", p.suite.inventoryURL+"/v1/products/"+productID, nil, nil, nil)
		if err == nil && status == 200 {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}
