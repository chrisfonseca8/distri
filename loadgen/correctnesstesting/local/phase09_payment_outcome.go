package main

import "time"

// Phase 9 verifies the configured simulated gateway reaches its documented terminal state.
func phase09() phaseDefinition {
	return phaseDefinition{9, "Payment gateway outcome", "Creates a payment, observes asynchronous processing, and checks the terminal status for the configured simulator mode.", func(p *phaseContext) {
		status, created, err := p.createPayment(newUUID(), 12500, "INR")
		p.check("payment attempt is created", err == nil && status == 201 && created.ID != "", statusDetail(status, err))
		terminal := p.waitPayment(created.ID, 7*time.Second)
		expected := map[string]string{"success": "SUCCEEDED", "decline": "FAILED", "timeout": "UNKNOWN"}[p.suite.gatewayMode]
		if expected == "" {
			p.note("Random mode has no deterministic expected terminal value.")
			p.check("random mode reaches a terminal status", terminal.Status == "SUCCEEDED" || terminal.Status == "FAILED" || terminal.Status == "UNKNOWN", terminal.Status)
		} else {
			p.check("gateway result matches mode", terminal.Status == expected, "expected "+expected+", got "+terminal.Status)
		}
	}}
}
