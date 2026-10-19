package main

// Phase 16 needs a proxy/failpoint that can fail inventory after payment succeeds.
func phase16() phaseDefinition {
	return phaseDefinition{16, "Inventory settlement recovery", "Would fail inventory confirmation after successful payment, then retry /pay to prove terminal order settlement is recoverable and idempotent.", func(p *phaseContext) {
		p.skip("Requires deterministic inventory outage injection after the order transition. Add a local test proxy/failpoint before enabling; ordinary container shutdown cannot target this timing reliably.")
	}}
}
