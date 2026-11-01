package saga

import (
	"errors"
	"testing"
)

func TestNext(t *testing.T) {
	tests := []struct {
		name    string
		step    Step
		outcome Outcome
		want    Decision
	}{
		{"reserve succeeds", RESERVE_STOCK, Succeeded, advance(CREATE_ORDER, SagaRunning)},
		{"reserve fails", RESERVE_STOCK, Failed, Decision{SagaStatus: SagaCompensated}},
		{"reserve is ambiguous", RESERVE_STOCK, Ambiguous, retry(RESERVE_STOCK, SagaRunning)},
		{"create succeeds", CREATE_ORDER, Succeeded, advance(TAKE_PAYMENT, SagaRunning)},
		{"create fails", CREATE_ORDER, Failed, advance(MARK_ORDER_CANCELLED, SagaCompensating)},
		{"refund recorded", REFUND_PAYMENT, Succeeded, advance(CANCEL_PAID_ORDER, SagaCompensating)},
		{"refund refused", REFUND_PAYMENT, Failed, Decision{SagaStatus: SagaStuck}},
		{"refund unclear", REFUND_PAYMENT, Ambiguous, retry(REFUND_PAYMENT, SagaCompensating)},
		{"paid order cancelled", CANCEL_PAID_ORDER, Succeeded, advance(RELEASE_STOCK, SagaCompensating)},
		{"paid order cancel refused", CANCEL_PAID_ORDER, Failed, Decision{SagaStatus: SagaStuck}},
		{"paid order cancel unclear", CANCEL_PAID_ORDER, Ambiguous, retry(CANCEL_PAID_ORDER, SagaCompensating)},
		{"create is ambiguous", CREATE_ORDER, Ambiguous, retry(CREATE_ORDER, SagaRunning)},
		{"payment succeeds", TAKE_PAYMENT, Succeeded, advance(MARK_ORDER_PAID, SagaRunning)},
		{"payment fails", TAKE_PAYMENT, Failed, advance(MARK_ORDER_CANCELLED, SagaCompensating)},
		{
			"payment is ambiguous",
			TAKE_PAYMENT,
			Ambiguous,
			Decision{NextStep: TAKE_PAYMENT, SagaStatus: SagaRunning},
		},
		{"mark paid succeeds", MARK_ORDER_PAID, Succeeded, advance(CONFIRM_STOCK, SagaRunning)},
		{"mark paid fails", MARK_ORDER_PAID, Failed, Decision{SagaStatus: SagaStuck}},
		{"mark paid is ambiguous", MARK_ORDER_PAID, Ambiguous, retry(MARK_ORDER_PAID, SagaRunning)},
		{"confirm succeeds", CONFIRM_STOCK, Succeeded, Decision{SagaStatus: SagaCompleted}},
		{"confirm fails", CONFIRM_STOCK, Failed, Decision{SagaStatus: SagaStuck}},
		{"confirm is ambiguous", CONFIRM_STOCK, Ambiguous, retry(CONFIRM_STOCK, SagaRunning)},
		{
			"mark cancelled succeeds",
			MARK_ORDER_CANCELLED,
			Succeeded,
			advance(RELEASE_STOCK, SagaCompensating),
		},
		{
			"mark cancelled fails",
			MARK_ORDER_CANCELLED,
			Failed,
			Decision{SagaStatus: SagaStuck},
		},
		{
			"mark cancelled is ambiguous",
			MARK_ORDER_CANCELLED,
			Ambiguous,
			retry(MARK_ORDER_CANCELLED, SagaCompensating),
		},
		{"release succeeds", RELEASE_STOCK, Succeeded, Decision{SagaStatus: SagaCompensated}},
		{"release fails", RELEASE_STOCK, Failed, Decision{SagaStatus: SagaStuck}},
		{"release is ambiguous", RELEASE_STOCK, Ambiguous, retry(RELEASE_STOCK, SagaCompensating)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Next(test.step, test.outcome)
			if err != nil {
				t.Fatalf("Next() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("Next() = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestNextRejectsUndefinedTransitions(t *testing.T) {
	tests := []struct {
		name    string
		step    Step
		outcome Outcome
	}{
		{"unknown step", Step("NOT_A_STEP"), Succeeded},
		{"unknown outcome", RESERVE_STOCK, Outcome("Maybe")},
		{"unsupported terminal transition", RELEASE_STOCK, Outcome("Cancelled")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision, err := Next(test.step, test.outcome)
			if !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("Next() error = %v, want ErrInvalidTransition", err)
			}
			if decision != (Decision{}) {
				t.Fatalf("Next() decision = %+v, want zero value", decision)
			}
		})
	}
}

// Every documented step/outcome pair must be defined, and every non-terminal
// decision must still have a path to a terminal saga status. This makes adding a
// step without completing its transition table fail loudly.
func TestTransitionTableIsCompleteAndEveryDecisionCanReachATerminalState(t *testing.T) {
	steps := []Step{RESERVE_STOCK, CREATE_ORDER, TAKE_PAYMENT, MARK_ORDER_PAID, MARK_ORDER_CANCELLED, CONFIRM_STOCK, RELEASE_STOCK, REFUND_PAYMENT, CANCEL_PAID_ORDER}
	outcomes := []Outcome{Succeeded, Failed, Ambiguous}
	terminal := map[Status]bool{SagaCompleted: true, SagaCompensated: true, SagaStuck: true}

	for _, step := range steps {
		for _, outcome := range outcomes {
			decision, err := Next(step, outcome)
			if err != nil {
				t.Errorf("missing transition for %s/%s: %v", step, outcome, err)
				continue
			}
			if decision.SagaStatus == "" {
				t.Errorf("transition %s/%s has no saga status", step, outcome)
			}
			if !terminal[decision.SagaStatus] && decision.NextStep == "" {
				t.Errorf("transition %s/%s is a non-terminal dead end: %+v", step, outcome, decision)
			}
			if decision.NextStep != "" && !canReachTerminal(decision.NextStep, terminal, map[Step]bool{}) {
				t.Errorf("transition %s/%s cannot reach a terminal state: %+v", step, outcome, decision)
			}
		}
	}
}

func canReachTerminal(step Step, terminal map[Status]bool, visiting map[Step]bool) bool {
	if visiting[step] {
		return false
	}
	visiting[step] = true
	defer delete(visiting, step)
	for _, outcome := range []Outcome{Succeeded, Failed, Ambiguous} {
		decision, err := Next(step, outcome)
		if err != nil {
			continue
		}
		if terminal[decision.SagaStatus] {
			return true
		}
		if decision.NextStep != "" && canReachTerminal(decision.NextStep, terminal, visiting) {
			return true
		}
	}
	return false
}
