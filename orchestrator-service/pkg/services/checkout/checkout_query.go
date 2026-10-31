package checkout

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/orchestrator-service/database/models"
	"gorm.io/gorm"
)

var ErrCheckoutNotFound = errors.New("checkout not found")

type CheckoutStepResult struct {
	Step       string     `json:"step"`
	Status     string     `json:"status"`
	Attempts   int        `json:"attempts"`
	LastError  *string    `json:"last_error"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

type GetCheckoutResult struct {
	OrderID     uuid.UUID            `json:"order_id"`
	Status      string               `json:"status"`
	CurrentStep string               `json:"current_step"`
	Version     int64                `json:"version"`
	UpdatedAt   time.Time            `json:"updated_at"`
	TotalPaise  int64                `json:"total_paise"`
	Currency    string               `json:"currency"`
	Steps       []CheckoutStepResult `json:"steps"`
}

// CheckoutStatusResult is the one-row snapshot used by the event stream. It
// deliberately excludes saga_steps so each transition costs one indexed query.
type CheckoutStatusResult struct {
	OrderID     uuid.UUID `json:"order_id"`
	Status      string    `json:"status"`
	CurrentStep string    `json:"current_step"`
	Version     int64     `json:"version"`
	UpdatedAt   time.Time `json:"updated_at"`
	TotalPaise  int64     `json:"total_paise"`
	Currency    string    `json:"currency"`
}

// GetCheckoutStatus returns the current saga row without loading step history.
func (s *CheckoutService) GetCheckoutStatus(ctx context.Context, orderID uuid.UUID) (CheckoutStatusResult, error) {
	var sagaRow models.Saga
	query := s.db.WithContext(ctx).
		Table("orchestrator.sagas").
		Where("id = ?", orderID).
		Take(&sagaRow)
	if errors.Is(query.Error, gorm.ErrRecordNotFound) {
		return CheckoutStatusResult{}, ErrCheckoutNotFound
	}
	if query.Error != nil {
		return CheckoutStatusResult{}, fmt.Errorf("load checkout saga: %w", query.Error)
	}
	return CheckoutStatusResult{
		OrderID: sagaRow.ID, Status: sagaRow.Status, CurrentStep: sagaRow.CurrentStep,
		Version: sagaRow.Version, UpdatedAt: sagaRow.UpdatedAt,
		TotalPaise: sagaRow.TotalPaise, Currency: sagaRow.Currency,
	}, nil
}

// GetCheckout returns the saga's status, current step, total and step list for orderID.
func (s *CheckoutService) GetCheckout(
	ctx context.Context,
	orderID uuid.UUID,
) (GetCheckoutResult, error) {
	status, err := s.GetCheckoutStatus(ctx, orderID)
	if err != nil {
		return GetCheckoutResult{}, err
	}

	var stepRows []models.SagaStep
	query := s.db.WithContext(ctx).
		Table("orchestrator.saga_steps").
		Where("saga_id = ?", orderID).
		Order("created_at ASC, id ASC").
		Find(&stepRows)
	if query.Error != nil {
		return GetCheckoutResult{}, fmt.Errorf("load checkout saga steps: %w", query.Error)
	}

	steps := make([]CheckoutStepResult, 0, len(stepRows))
	for _, row := range stepRows {
		steps = append(steps, CheckoutStepResult{
			Step:       row.Step,
			Status:     row.Status,
			Attempts:   row.Attempts,
			LastError:  row.LastError,
			StartedAt:  row.StartedAt,
			FinishedAt: row.FinishedAt,
		})
	}

	return GetCheckoutResult{
		OrderID:     status.OrderID,
		Status:      status.Status,
		CurrentStep: status.CurrentStep,
		Version:     status.Version,
		UpdatedAt:   status.UpdatedAt,
		TotalPaise:  status.TotalPaise,
		Currency:    status.Currency,
		Steps:       steps,
	}, nil
}
