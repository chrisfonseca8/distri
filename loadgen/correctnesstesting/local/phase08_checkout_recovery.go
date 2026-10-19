package main

// Phase 8 is reserved for process-crash recovery; the normal stack has no safe crash hook.
func phase08() phaseDefinition {
	return phaseDefinition{8, "Checkout crash recovery", "Would interrupt checkout between idempotency claim and completion, restart the service, and verify a retry safely finishes without duplicate stock or orders.", func(p *phaseContext) {
		p.skip("Requires a deterministic checkout failpoint or process-kill hook between transaction stages. The production-shaped local image exposes neither; add a test-only fault injector before enabling this phase.")
	}}
}
