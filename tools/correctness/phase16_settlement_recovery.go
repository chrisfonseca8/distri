package main

import "time"

// Phase 16 proves a payment command sent while payment-service is down is resent and charged once.
func phase16() phaseDefinition {
	return phaseDefinition{16, "Payment command recovery", "Stops payment-service, starts a checkout, waits for the saga sweeper to resend payment.initiate, restarts payment-service, and verifies one payment and a completed saga.", func(p *phaseContext) {
		if p.suite.gatewayMode != "success" {
			p.skip("Run with GATEWAY_MODE=success so the recovered saga reaches PAID.")
			return
		}
		if !p.suite.canControlService("payment-service") {
			p.skip("Configure COMPOSE_FILE or PAYMENT_DOCKER_SOCKET so the runner can stop and restart payment-service deterministically.")
			return
		}

		product := p.productID(26)
		before := p.inventory(product)
		if err := p.suite.compose("stop", "payment-service"); err != nil {
			p.fail("stop payment-service", err.Error())
			return
		}
		paymentStopped := true
		defer func() {
			if paymentStopped {
				_ = p.suite.compose("start", "payment-service")
			}
		}()

		status, checkout, err := p.checkout("phase16-"+newUUID(), []map[string]any{item(product, 2)})
		p.check("checkout is accepted while payment-service is down", err == nil && status == 202 && checkout.OrderID != "", statusDetail(status, err))
		p.check("sweeper resends payment.initiate", p.waitStepAttempts(checkout.OrderID, "TAKE_PAYMENT", 2, 3*time.Minute), "TAKE_PAYMENT attempts must reach 2")
		p.check("order stays pending while payment is unavailable", p.text("SELECT status FROM orders.orders WHERE id=$1", checkout.OrderID) == "PENDING_PAYMENT", "never paid without a confirmed payment")
		intermediate := p.inventory(product)
		p.check("stock is held but not consumed", intermediate.OnHand == before.OnHand && intermediate.Reserved == before.Reserved+2, "reserved +2; on_hand unchanged")

		if err := p.suite.compose("start", "payment-service"); err != nil {
			p.fail("restart payment-service", err.Error())
			return
		}
		paymentStopped = false
		status, order, err := p.awaitOrder(checkout.OrderID, 2*time.Minute)
		p.check("saga completes after recovery", err == nil && status == 200 && order.Status == "PAID", statusDetail(status, err))
		p.check("resends create one successful payment", p.scalar("SELECT count(*) FROM payments.payment_attempts WHERE order_id=$1 AND status='SUCCEEDED'", checkout.OrderID) == 1 && p.scalar("SELECT count(*) FROM payments.payment_attempts WHERE order_id=$1", checkout.OrderID) == 1, "exactly one payment attempt")
		p.check("reservation becomes confirmed", p.waitScalar(30*time.Second, 1, "SELECT count(*) FROM inventory.reservations WHERE order_id=$1 AND status='CONFIRMED'", checkout.OrderID) == 1, "settlement repaired")
		after := p.inventory(product)
		p.check("stock moves exactly once", after.OnHand == before.OnHand-2 && after.Reserved == before.Reserved, "on_hand -2; reserved restored")
	}}
}
