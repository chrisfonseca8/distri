package main

// Phase 2 checks inventory input boundaries and verifies server-side pricing.
func phase02() phaseDefinition {
	return phaseDefinition{2, "Inventory validation and pricing", "Exercises valid reservation pricing plus empty, zero, negative, excessive, duplicate, and unknown-product requests.", func(p *phaseContext) {
		product := p.productID(1)
		orderID := newUUID()
		status, result, err := p.reserve(orderID, "phase02-valid", []map[string]any{item(product, 2)})
		price := p.scalar("SELECT price_paise FROM inventory.products WHERE id=$1", product)
		p.check("valid reservation is created", err == nil && status == 201 && result.Status == "RESERVED", statusDetail(status, err))
		p.check("price and total come from inventory", result.TotalPaise == price*2 && result.Items[0].UnitPricePaise == price, "client cannot choose price")
		cases := []struct {
			name  string
			items []map[string]any
			want  int
		}{
			{"empty cart", []map[string]any{}, 400}, {"zero quantity", []map[string]any{item(product, 0)}, 400},
			{"negative quantity", []map[string]any{item(product, -1)}, 400}, {"quantity over 100", []map[string]any{item(product, 101)}, 400},
			{"duplicate product", []map[string]any{item(product, 1), item(product, 1)}, 400},
			{"unknown product", []map[string]any{item(newUUID(), 1)}, 404},
		}
		for _, test := range cases {
			status, _, err := p.reserve(newUUID(), "phase02-"+test.name, test.items)
			p.check(test.name+" is rejected", err == nil && status == test.want, statusDetail(status, err))
		}
		p.changeReservation(orderID, "release")
	}}
}
