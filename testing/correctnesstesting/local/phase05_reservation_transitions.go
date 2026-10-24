package main

// Phase 5 checks terminal reservation transitions and their stock accounting.
func phase05() phaseDefinition {
	return phaseDefinition{5, "Confirm and release transitions", "Confirms one reservation and releases another twice, verifying idempotency and preventing contradictory terminal transitions.", func(p *phaseContext) {
		confirmProduct, releaseProduct := p.productID(5), p.productID(6)
		confirmBefore, releaseBefore := p.inventory(confirmProduct), p.inventory(releaseProduct)
		confirmOrder, releaseOrder := newUUID(), newUUID()
		p.reserve(confirmOrder, "phase05-confirm", []map[string]any{item(confirmProduct, 2)})
		status1, first, _ := p.changeReservation(confirmOrder, "confirm")
		status2, second, _ := p.changeReservation(confirmOrder, "confirm")
		conflict, _, _ := p.changeReservation(confirmOrder, "release")
		confirmAfter := p.inventory(confirmProduct)
		p.check("confirm is idempotent", status1 == 200 && status2 == 200 && first.Status == "CONFIRMED" && second.Status == "CONFIRMED", "both calls return CONFIRMED")
		p.check("confirm moves stock once", confirmAfter.OnHand == confirmBefore.OnHand-2 && confirmAfter.Reserved == confirmBefore.Reserved, "on_hand decreases and reserved returns")
		p.check("confirmed cannot be released", conflict == 409, statusDetail(conflict, nil))
		p.reserve(releaseOrder, "phase05-release", []map[string]any{item(releaseProduct, 3)})
		release1, _, _ := p.changeReservation(releaseOrder, "release")
		release2, final, _ := p.changeReservation(releaseOrder, "release")
		releaseAfter := p.inventory(releaseProduct)
		p.check("release is idempotent", release1 == 200 && release2 == 200 && final.Status == "RELEASED", "both calls return RELEASED")
		p.check("release restores availability", releaseAfter == releaseBefore, "all inventory counters return to starting values")
	}}
}
