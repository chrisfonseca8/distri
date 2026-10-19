package main

// Phase 4 proves repeated inventory requests reserve stock only once.
func phase04() phaseDefinition {
	return phaseDefinition{4, "Inventory idempotency", "Repeats the same order/hash, then changes the hash, checking response reuse, conflict detection, and a single stock mutation.", func(p *phaseContext) {
		product, orderID := p.productID(4), newUUID()
		before := p.inventory(product)
		firstStatus, first, _ := p.reserve(orderID, "phase04-same", []map[string]any{item(product, 2)})
		secondStatus, second, _ := p.reserve(orderID, "phase04-same", []map[string]any{item(product, 2)})
		conflictStatus, _, _ := p.reserve(orderID, "phase04-different", []map[string]any{item(product, 2)})
		after := p.inventory(product)
		p.check("first request creates reservation", firstStatus == 201, statusDetail(firstStatus, nil))
		p.check("same request reuses reservation", secondStatus == 200 && first.ReservationID == second.ReservationID, statusDetail(secondStatus, nil))
		p.check("different hash conflicts", conflictStatus == 409, statusDetail(conflictStatus, nil))
		p.check("stock reserved once", after.Reserved-before.Reserved == 2, "reserved delta must equal 2")
		p.changeReservation(orderID, "release")
	}}
}
