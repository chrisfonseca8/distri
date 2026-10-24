package main

// Phase 3 ensures a multi-line failure rolls back every database write.
func phase03() phaseDefinition {
	return phaseDefinition{3, "Atomic inventory rollback", "Attempts a two-product reservation whose second line lacks stock and verifies no stock or reservation row is partially committed.", func(p *phaseContext) {
		first, second := p.productID(2), p.productID(3)
		beforeFirst, beforeSecond := p.inventory(first), p.inventory(second)
		orderID := newUUID()
		status, _, err := p.reserve(orderID, "phase03-rollback", []map[string]any{item(first, 1), item(second, 100)})
		afterFirst, afterSecond := p.inventory(first), p.inventory(second)
		p.check("insufficient stock returns conflict", err == nil && status == 409, statusDetail(status, err))
		p.check("first stock update rolled back", beforeFirst == afterFirst, "first product inventory must be unchanged")
		p.check("second stock is unchanged", beforeSecond == afterSecond, "second product inventory must be unchanged")
		p.check("reservation claim rolled back", p.scalar("SELECT count(*) FROM inventory.reservations WHERE order_id=$1", orderID) == 0, "no partial reservation")
	}}
}
