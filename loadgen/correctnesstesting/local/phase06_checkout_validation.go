package main

import "net/http"

// Phase 6 checks the public checkout contract and error mapping.
func phase06() phaseDefinition {
	return phaseDefinition{6, "Checkout validation", "Validates a normal checkout and rejects missing idempotency keys, invalid quantities, duplicates, unknown products, and insufficient stock.", func(p *phaseContext) {
		product := p.productID(7)
		status, result, err := p.checkout("phase06-"+newUUID(), []map[string]any{item(product, 1)})
		p.check("valid checkout is accepted by the saga", err == nil && status == 202 && result.Status == "RUNNING", statusDetail(status, err))
		status, _, err = p.request(http.MethodPost, p.suite.orchestratorURL+"/v1/checkouts", map[string]any{"items": []map[string]any{item(product, 1)}}, nil, nil)
		p.check("missing idempotency key is rejected", err == nil && status == 400, statusDetail(status, err))
		cases := []struct {
			name  string
			items []map[string]any
			want  int
		}{
			{"empty cart", []map[string]any{}, 400}, {"zero quantity", []map[string]any{item(product, 0)}, 400},
			{"negative quantity", []map[string]any{item(product, -1)}, 400}, {"duplicate product", []map[string]any{item(product, 1), item(product, 1)}, 400},
			{"unknown product", []map[string]any{item(newUUID(), 1)}, 404},
		}
		for _, test := range cases {
			status, _, err := p.checkout("phase06-"+newUUID(), test.items)
			p.check(test.name+" mapping", err == nil && status == test.want, statusDetail(status, err))
		}
		limitedProduct := p.productID(8)
		p.setStock(limitedProduct, 1)
		status, _, err = p.checkout("phase06-"+newUUID(), []map[string]any{item(limitedProduct, 2)})
		p.check("insufficient stock mapping", err == nil && status == 409, statusDetail(status, err))
	}}
}
