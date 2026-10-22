package saga

import (
	"errors"
	"fmt"
)

type Step string

const (
	RESERVE_STOCK        Step = "RESERVE_STOCK"
	CREATE_ORDER         Step = "CREATE_ORDER"
	TAKE_PAYMENT         Step = "TAKE_PAYMENT"
	MARK_ORDER_PAID      Step = "MARK_ORDER_PAID"
	MARK_ORDER_CANCELLED Step = "MARK_ORDER_CANCELLED"
	CONFIRM_STOCK        Step = "CONFIRM_STOCK"
	RELEASE_STOCK        Step = "RELEASE_STOCK"
)

type Outcome string

const (
	Succeeded Outcome = "Succeeded"
	Failed    Outcome = "Failed"
	Ambiguous Outcome = "Ambiguous"
)

type Status string

const (
	SagaRunning      Status = "RUNNING"
	SagaCompleted    Status = "COMPLETED"
	SagaCompensating Status = "COMPENSATING"
	SagaCompensated  Status = "COMPENSATED"
	SagaStuck        Status = "STUCK"
)

// Decision describes the state change and command caused by one step result.
// An empty NextStep or Command means there is no next step or command.
type Decision struct {
	NextStep   Step
	Command    Step
	SagaStatus Status
}

var ErrInvalidTransition = errors.New("invalid saga transition")

type transition struct {
	step    Step
	outcome Outcome
}

var transitions = map[transition]Decision{
	{RESERVE_STOCK, Succeeded}:        advance(CREATE_ORDER, SagaRunning),
	{RESERVE_STOCK, Failed}:           {SagaStatus: SagaCompensated},
	{RESERVE_STOCK, Ambiguous}:        retry(RESERVE_STOCK, SagaRunning),
	{CREATE_ORDER, Succeeded}:         advance(TAKE_PAYMENT, SagaRunning),
	{CREATE_ORDER, Failed}:            advance(RELEASE_STOCK, SagaCompensating),
	{CREATE_ORDER, Ambiguous}:         retry(CREATE_ORDER, SagaRunning),
	{TAKE_PAYMENT, Succeeded}:         advance(MARK_ORDER_PAID, SagaRunning),
	{TAKE_PAYMENT, Failed}:            advance(MARK_ORDER_CANCELLED, SagaCompensating),
	{TAKE_PAYMENT, Ambiguous}:         {NextStep: TAKE_PAYMENT, SagaStatus: SagaRunning},
	{MARK_ORDER_PAID, Succeeded}:      advance(CONFIRM_STOCK, SagaRunning),
	{MARK_ORDER_PAID, Failed}:         {SagaStatus: SagaStuck},
	{MARK_ORDER_PAID, Ambiguous}:      retry(MARK_ORDER_PAID, SagaRunning),
	{CONFIRM_STOCK, Succeeded}:        {SagaStatus: SagaCompleted},
	{CONFIRM_STOCK, Failed}:           {SagaStatus: SagaStuck},
	{CONFIRM_STOCK, Ambiguous}:        retry(CONFIRM_STOCK, SagaRunning),
	{MARK_ORDER_CANCELLED, Succeeded}: advance(RELEASE_STOCK, SagaCompensating),
	{MARK_ORDER_CANCELLED, Failed}:    {SagaStatus: SagaStuck},
	{MARK_ORDER_CANCELLED, Ambiguous}: retry(MARK_ORDER_CANCELLED, SagaCompensating),
	{RELEASE_STOCK, Succeeded}:        {SagaStatus: SagaCompensated},
	{RELEASE_STOCK, Failed}:           {SagaStatus: SagaStuck},
	{RELEASE_STOCK, Ambiguous}:        retry(RELEASE_STOCK, SagaCompensating),
}

// Next looks up the deterministic transition for a completed step outcome.
func Next(currentStep Step, outcome Outcome) (Decision, error) {
	decision, exists := transitions[transition{step: currentStep, outcome: outcome}]
	if !exists {
		return Decision{}, fmt.Errorf(
			"%w: step %q with outcome %q",
			ErrInvalidTransition,
			currentStep,
			outcome,
		)
	}
	return decision, nil
}

func advance(next Step, status Status) Decision {
	return Decision{NextStep: next, Command: next, SagaStatus: status}
}

func retry(step Step, status Status) Decision {
	return Decision{NextStep: step, Command: step, SagaStatus: status}
}
