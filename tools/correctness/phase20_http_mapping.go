package main

import "net/http"

// Phase 20 locks down representative public error-to-HTTP mappings.
func phase20() phaseDefinition {
	return phaseDefinition{20, "HTTP error mapping", "Sends malformed IDs, missing resources, invalid payment data, and checkout conflicts to verify stable client-facing status codes.", func(p *phaseContext) {
		cases := []struct {
			name, method, url string
			body              any
			want              int
		}{
			{"bad order ID", http.MethodGet, p.suite.orderURL + "/v1/orders/not-a-uuid", nil, 400},
			{"missing order", http.MethodGet, p.suite.orderURL + "/v1/orders/" + newUUID(), nil, 404},
			{"bad payment ID", http.MethodGet, p.suite.paymentURL + "/v1/payments/not-a-uuid", nil, 400},
			{"missing payment", http.MethodGet, p.suite.paymentURL + "/v1/payments/" + newUUID(), nil, 404},
			{"invalid payment amount", http.MethodPost, p.suite.paymentURL + "/v1/payments", map[string]any{"order_id": newUUID(), "amount_paise": 0, "currency": "INR"}, 400},
			{"invalid currency length", http.MethodPost, p.suite.paymentURL + "/v1/payments", map[string]any{"order_id": newUUID(), "amount_paise": 1, "currency": "US"}, 400},
		}
		for _, test := range cases {
			status, _, err := p.request(test.method, test.url, test.body, nil, nil)
			p.check(test.name, err == nil && status == test.want, statusDetail(status, err))
		}
		product, key := p.productID(24), "phase20-conflict-"+newUUID()
		p.checkout(key, []map[string]any{item(product, 1)})
		status, _, _ := p.checkout(key, []map[string]any{item(product, 2)})
		p.check("idempotency body conflict", status == 422, statusDetail(status, nil))
	}}
}
