package sim

import "strconv"

// Scenarios replace the missing customer interaction. Stage 2 covers the basic
// lifecycle; ambiguity, deadline races, refunds and faults are added in later
// stages.
const (
	// ScenarioCapture: the customer pays; authorization, then capture.
	ScenarioCapture = "capture"
	// ScenarioDecline: the customer's attempt fails definitively.
	ScenarioDecline = "decline"
	// ScenarioNone: no customer payment attempt ever happens.
	ScenarioNone = "none"
	// ScenarioAuthorizeOnly: the payment is authorized and waits for a manual capture.
	ScenarioAuthorizeOnly = "authorize_only"
)

func ValidScenario(name string) bool {
	switch name {
	case ScenarioCapture, ScenarioDecline, ScenarioNone, ScenarioAuthorizeOnly:
		return true
	}
	return false
}

// pickScenario chooses a scenario for a checkout from the weighted mix. The
// choice depends only on the seed and the receipt, so the same checkout gets the
// same scenario on every run and under any scheduling.
func pickScenario(mix []WeightedScenario, seed uint64, receipt string) string {
	total := 0
	for _, entry := range mix {
		total += entry.Weight
	}
	roll := int(hashParts(seed, "scenario", receipt) % uint64(total))
	for _, entry := range mix {
		if roll < entry.Weight {
			return entry.Name
		}
		roll -= entry.Weight
	}
	return mix[len(mix)-1].Name
}

func itoa(n int) string { return strconv.Itoa(n) }
