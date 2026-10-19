package main

// Phase 11 makes payment data disagree with its order and verifies settlement refuses it.
func phase11() phaseDefinition {
	return phaseDefinition{11, "Order/payment consistency", "Pre-creates wrong-amount and wrong-currency attempts for real orders, then verifies pay rejects both without changing order state.", func(p *phaseContext) {
		product := p.productID(13)
		_, amountOrder, _ := p.checkout("phase11-amount-"+newUUID(), []map[string]any{item(product, 1)})
		p.createPayment(amountOrder.OrderID, amountOrder.TotalPaise+1, "INR")
		status, _, _ := p.payOrder(amountOrder.OrderID)
		_, amountAfter, _ := p.getOrder(amountOrder.OrderID)
		p.check("wrong amount is rejected", status == 502 && amountAfter.Status == "PENDING_PAYMENT", statusDetail(status, nil))

		_, currencyOrder, _ := p.checkout("phase11-currency-"+newUUID(), []map[string]any{item(product, 1)})
		_, insertErr := p.suite.db.Exec(p.suite.ctx, `INSERT INTO payments.payment_attempts
			(id, order_id, amount_paise, currency, status, gateway, created_at, updated_at)
			VALUES (gen_random_uuid(), $1, $2, 'USD', 'SUCCEEDED', 'test-fixture', now(), now())`,
			currencyOrder.OrderID, currencyOrder.TotalPaise)
		p.check("wrong-currency fixture was inserted", insertErr == nil, "direct database fixture")
		status, _, _ = p.payOrder(currencyOrder.OrderID)
		_, currencyAfter, _ := p.getOrder(currencyOrder.OrderID)
		p.check("wrong currency is rejected", status == 502 && currencyAfter.Status == "PENDING_PAYMENT", statusDetail(status, nil))
	}}
}
