package services

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/order-service/database/models"
	"github.com/chrisfonseca08/distri/order-service/pkg/clients"
)

type settlementPaymentClient struct {
	createPayment clients.Payment
	createErr     error
	getPayment    clients.Payment
	getErr        error
	getCalls      int
}

func (c *settlementPaymentClient) Create(
	context.Context,
	uuid.UUID,
	int64,
	string,
) (clients.Payment, error) {
	return c.createPayment, c.createErr
}

func (c *settlementPaymentClient) Get(
	context.Context,
	uuid.UUID,
) (clients.Payment, error) {
	c.getCalls++
	return c.getPayment, c.getErr
}

func TestSettlePendingOrderNonFinalPaymentsAreNotReady(t *testing.T) {
	order := models.Order{
		ID:         uuid.New(),
		Status:     "PENDING_PAYMENT",
		TotalPaise: 149900,
		Currency:   "INR",
	}
	paymentID := uuid.New()

	for _, status := range []string{"CREATED", "PENDING", "UNKNOWN"} {
		t.Run(status, func(t *testing.T) {
			payment := clients.Payment{
				ID:          paymentID,
				OrderID:     order.ID,
				AmountPaise: order.TotalPaise,
				Currency:    order.Currency,
				Status:      status,
			}
			paymentClient := &settlementPaymentClient{
				createPayment: payment,
				getPayment:    payment,
			}
			service := NewOrderService(nil, paymentClient, nil)

			settledOrder, outcome, err := service.settlePendingOrder(
				context.Background(),
				order,
				0,
			)
			if err != nil {
				t.Fatalf("settle pending order: %v", err)
			}
			if outcome != SettleNotReady {
				t.Fatalf("outcome = %q, want %q", outcome, SettleNotReady)
			}
			if settledOrder.ID != order.ID || settledOrder.Status != order.Status {
				t.Fatalf("order changed while payment was %s: %+v", status, settledOrder)
			}

			wantGetCalls := 1
			if status == "UNKNOWN" {
				wantGetCalls = 0
			}
			if paymentClient.getCalls != wantGetCalls {
				t.Fatalf("Get calls = %d, want %d", paymentClient.getCalls, wantGetCalls)
			}
		})
	}
}

func TestSettlePendingOrderUnavailablePaymentIsRetry(t *testing.T) {
	order := models.Order{
		ID:         uuid.New(),
		Status:     "PENDING_PAYMENT",
		TotalPaise: 149900,
		Currency:   "INR",
	}
	paymentClient := &settlementPaymentClient{
		createErr: fmt.Errorf("request failed: %w", clients.ErrPaymentUnavailable),
	}
	service := NewOrderService(nil, paymentClient, nil)

	settledOrder, outcome, err := service.settlePendingOrder(
		context.Background(),
		order,
		0,
	)
	if err != nil {
		t.Fatalf("settle pending order: %v", err)
	}
	if outcome != SettleRetry {
		t.Fatalf("outcome = %q, want %q", outcome, SettleRetry)
	}
	if settledOrder.ID != order.ID || settledOrder.Status != order.Status {
		t.Fatalf("order changed after unavailable payment service: %+v", settledOrder)
	}
}

func TestSettlePendingOrderZeroWaitGetTimeoutIsRetry(t *testing.T) {
	order := models.Order{
		ID:         uuid.New(),
		Status:     "PENDING_PAYMENT",
		TotalPaise: 149900,
		Currency:   "INR",
	}
	payment := clients.Payment{
		ID:          uuid.New(),
		OrderID:     order.ID,
		AmountPaise: order.TotalPaise,
		Currency:    order.Currency,
		Status:      "PENDING",
	}
	paymentClient := &settlementPaymentClient{
		createPayment: payment,
		getErr:        fmt.Errorf("payment lookup timed out: %w", context.DeadlineExceeded),
	}
	service := NewOrderService(nil, paymentClient, nil)

	_, outcome, err := service.settlePendingOrder(context.Background(), order, 0)
	if outcome != SettleRetry {
		t.Fatalf("outcome = %q, want %q", outcome, SettleRetry)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
	if paymentClient.getCalls != 1 {
		t.Fatalf("Get calls = %d, want 1", paymentClient.getCalls)
	}
}

func TestSettlePendingOrderPollingDeadlineIsNotReady(t *testing.T) {
	order := models.Order{
		ID:         uuid.New(),
		Status:     "PENDING_PAYMENT",
		TotalPaise: 149900,
		Currency:   "INR",
	}
	payment := clients.Payment{
		ID:          uuid.New(),
		OrderID:     order.ID,
		AmountPaise: order.TotalPaise,
		Currency:    order.Currency,
		Status:      "PENDING",
	}
	paymentClient := &settlementPaymentClient{createPayment: payment}
	service := NewOrderService(nil, paymentClient, nil)

	_, outcome, err := service.settlePendingOrder(
		context.Background(),
		order,
		time.Millisecond,
	)
	if err != nil {
		t.Fatalf("settle pending order: %v", err)
	}
	if outcome != SettleNotReady {
		t.Fatalf("outcome = %q, want %q", outcome, SettleNotReady)
	}
}
