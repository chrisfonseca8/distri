package services

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/chrisfonseca08/distri/payment-service/database/models"
	"github.com/chrisfonseca08/distri/payment-service/pkg/payment_gateway"
	"gorm.io/gorm"
)

const persistenceTimeout = 10 * time.Second

// PaymentProcessor claims a created payment, calls the external gateway
// outside the HTTP request, and persists the resulting payment status.
type PaymentProcessor struct {
	db             *gorm.DB
	gateway        paymentgateway.Gateway
	gatewayTimeout time.Duration
	workers        sync.WaitGroup
}

func NewPaymentProcessor(
	db *gorm.DB,
	gateway paymentgateway.Gateway,
	gatewayTimeout time.Duration,
) *PaymentProcessor {
	return &PaymentProcessor{
		db:             db,
		gateway:        gateway,
		gatewayTimeout: gatewayTimeout,
	}
}

// Start launches gateway processing in a new goroutine and returns immediately.
func (p *PaymentProcessor) Start(attempt models.PaymentAttempt) {
	p.workers.Add(1)
	go func() {
		defer p.workers.Done()
		p.process(attempt)
	}()
}

// Wait blocks until every gateway-processing goroutine has finished or ctx
// expires, whichever comes first.
func (p *PaymentProcessor) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		p.workers.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *PaymentProcessor) process(attempt models.PaymentAttempt) {
	claimContext, cancelClaim := context.WithTimeout(context.Background(), persistenceTimeout)
	claimResult := p.db.WithContext(claimContext).
		Model(&models.PaymentAttempt{}).
		Where("id = ? AND status = ?", attempt.ID, "CREATED").
		Update("status", "PENDING")
	cancelClaim()

	if claimResult.Error != nil {
		slog.Error(
			"claim payment for gateway processing",
			"payment_id", attempt.ID.String(),
			"error", claimResult.Error,
		)
		return
	}
	if claimResult.RowsAffected != 1 {
		slog.Info(
			"payment gateway processing skipped",
			"payment_id", attempt.ID.String(),
			"reason", "payment was already claimed",
		)
		return
	}

	slog.Info(
		"payment claimed for gateway processing",
		"payment_id", attempt.ID.String(),
		"status", "PENDING",
	)

	gatewayContext, cancelGateway := context.WithTimeout(context.Background(), p.gatewayTimeout)
	ref, gatewayErr := p.gateway.CreateCharge(
		gatewayContext,
		attempt.OrderID,
		attempt.AmountPaise,
	)
	cancelGateway()

	updates := paymentResultUpdates(ref, gatewayErr)

	persistenceContext, cancelPersistence := context.WithTimeout(context.Background(), persistenceTimeout)
	defer cancelPersistence()

	result := p.db.WithContext(persistenceContext).
		Model(&models.PaymentAttempt{}).
		Where("id = ? AND status = ?", attempt.ID, "PENDING").
		Updates(updates)
	if result.Error != nil {
		slog.Error(
			"persist payment gateway result",
			"payment_id", attempt.ID.String(),
			"error", result.Error,
		)
		return
	}

	if result.RowsAffected == 0 {
		slog.Info(
			"payment gateway result ignored",
			"payment_id", attempt.ID.String(),
			"reason", "payment is no longer pending",
		)
		return
	}

	slog.Info(
		"payment gateway result persisted",
		"payment_id", attempt.ID.String(),
		"status", updates["status"],
	)
}

func paymentResultUpdates(ref string, gatewayErr error) map[string]any {
	if gatewayErr == nil {
		return map[string]any{
			"status":           "SUCCEEDED",
			"gateway_order_id": ref,
			"failure_code":     nil,
		}
	}

	var declined *paymentgateway.ErrDeclined
	if errors.As(gatewayErr, &declined) {
		return map[string]any{
			"status":       "FAILED",
			"failure_code": "DECLINED",
		}
	}

	return map[string]any{
		"status":       "UNKNOWN",
		"failure_code": nil,
	}
}
