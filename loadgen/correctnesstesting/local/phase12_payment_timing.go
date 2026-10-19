package main

// Phase 12 checks that /pay represents either a completed or explicitly pending outcome.
func phase12() phaseDefinition {
	return phaseDefinition{12, "Payment polling and timing", "Measures the order pay endpoint while it waits for asynchronous payment, validating 200 terminal versus 202 pending semantics.", func(p *phaseContext) {
		_, checkout, _ := p.checkout("phase12-"+newUUID(), []map[string]any{item(p.productID(14), 1)})
		status, order, err := p.payOrder(checkout.OrderID)
		valid := err == nil && ((status == 200 && (order.Status == "PAID" || order.Status == "CANCELLED")) || (status == 202 && order.Status == "PENDING_PAYMENT"))
		p.check("pay response status matches order state", valid, "HTTP/order status must agree")
		if p.suite.gatewayMode == "success" {
			p.check("success simulator settles synchronously", status == 200 && order.Status == "PAID", statusDetail(status, err))
		}
	}}
}
