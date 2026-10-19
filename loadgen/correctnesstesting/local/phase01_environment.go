package main

import "net/http"

// Phase 1 proves the disposable stack, schemas, migrations, and inventory seed are ready.
func phase01() phaseDefinition {
	return phaseDefinition{1, "Environment and migrations", "Checks service reachability, database schemas, inventory seed count, and the required empty payment table.", func(p *phaseContext) {
		status, _, err := p.request(http.MethodGet, p.suite.orderURL+"/health", nil, nil, nil)
		p.check("order service health", err == nil && status == 200, statusDetail(status, err))
		status, _, err = p.request(http.MethodGet, p.suite.paymentURL+"/health", nil, nil, nil)
		p.check("payment service health", err == nil && status == 200, statusDetail(status, err))
		status, _, err = p.request(http.MethodGet, p.suite.inventoryURL+"/v1/products/"+p.productID(0), nil, nil, nil)
		p.check("inventory service and seed", err == nil && status == 200, statusDetail(status, err))
		p.check("50 inventory products were seeded", p.scalar("SELECT count(*) FROM inventory.products") == 50, "expected exactly 50")
		p.check("payments start empty", p.scalar("SELECT count(*) FROM payments.payment_attempts") == 0, "payment seeding must remain disabled")
	}}
}

func statusDetail(status int, err error) string {
	if err != nil {
		return err.Error()
	}
	return "HTTP status=" + http.StatusText(status)
}
