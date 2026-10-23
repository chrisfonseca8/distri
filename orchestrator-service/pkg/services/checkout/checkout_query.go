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
	TotalPaise  int64                `json:"total_paise"`
	Currency    string               `json:"currency"`
	Steps       []CheckoutStepResult `json:"steps"`
}

func (s *CheckoutService) GetCheckout(
	ctx context.Context,
	orderID uuid.UUID,
) (GetCheckoutResult, error) {
	var sagaRow models.Saga
	query := s.db.WithContext(ctx).
		Table("orchestrator.sagas").
		Where("id = ?", orderID).
		Take(&sagaRow)
	if errors.Is(query.Error, gorm.ErrRecordNotFound) {
		return GetCheckoutResult{}, ErrCheckoutNotFound
	}
	if query.Error != nil {
		return GetCheckoutResult{}, fmt.Errorf("load checkout saga: %w", query.Error)
	}

	var stepRows []models.SagaStep
	query = s.db.WithContext(ctx).
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
		OrderID:     sagaRow.ID,
		Status:      sagaRow.Status,
		CurrentStep: sagaRow.CurrentStep,
		TotalPaise:  sagaRow.TotalPaise,
		Currency:    sagaRow.Currency,
		Steps:       steps,
	}, nil
}
