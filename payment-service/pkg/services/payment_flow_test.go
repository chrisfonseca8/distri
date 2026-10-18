package services

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/payment-service/database/models"
	"github.com/chrisfonseca08/distri/payment-service/pkg/config"
	"github.com/chrisfonseca08/distri/payment-service/pkg/payment_gateway"
	"gorm.io/gorm"
)

// These tests run against the dev/test database (DATABASE_TEST_URL). Rows use
// random order IDs and are deleted on cleanup.

type countingGateway struct {
	inner paymentgateway.Gateway
	calls atomic.Int64
}

func (g *countingGateway) CreateCharge(ctx context.Context, orderID uuid.UUID, amountPaise int64) (string, error) {
	g.calls.Add(1)
	return g.inner.CreateCharge(ctx, orderID, amountPaise)
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := config.OpenDB()
	if err != nil {
		t.Skipf("test database unavailable: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func newGateway(t *testing.T, mode string) *countingGateway {
	t.Helper()
	sim, err := paymentgateway.NewSimulator(mode, 0)
	if err != nil {
		t.Fatalf("create simulator: %v", err)
	}
	return &countingGateway{inner: sim}
}

func cleanupOrder(t *testing.T, db *gorm.DB, orderID uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		db.Where("order_id = ?", orderID).Delete(&models.PaymentAttempt{})
	})
}

func waitForStatus(t *testing.T, svc *PaymentService, id uuid.UUID, want string) models.PaymentAttempt {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second) // Neon round-trips can take seconds
	for {
		attempt, err := svc.GetPayment(context.Background(), id)
		if err != nil {
			t.Fatalf("get payment: %v", err)
		}
		if attempt.Status == want {
			return attempt
		}
		if time.Now().After(deadline) {
			t.Fatalf("status = %s, want %s after 30s", attempt.Status, want)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestPaymentFlowOutcomes(t *testing.T) {
	db := openTestDB(t)
	svc := NewPaymentService(db)

	tests := []struct {
		name        string
		mode        string
		wantStatus  string
		wantFailure string
		wantRef     bool
	}{
		{"success", paymentgateway.ModeSuccess, "SUCCEEDED", "", true},
		{"decline", paymentgateway.ModeDecline, "FAILED", "DECLINED", false},
		{"ambiguous timeout", paymentgateway.ModeTimeout, "UNKNOWN", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw := newGateway(t, tt.mode)
			processor := NewPaymentProcessor(db, gw, 100*time.Millisecond)
			orderID := uuid.New()
			cleanupOrder(t, db, orderID)

			result, err := svc.CreatePayment(context.Background(), CreatePaymentInput{
				OrderID: orderID, AmountPaise: 149900, Currency: "inr",
			})
			if err != nil {
				t.Fatalf("create payment: %v", err)
			}
			if !result.Created || result.Attempt.Status != "CREATED" || result.Attempt.Currency != "INR" {
				t.Fatalf("unexpected create result: %+v", result)
			}

			processor.Start(result.Attempt)
			got := waitForStatus(t, svc, result.Attempt.ID, tt.wantStatus)

			if tt.wantFailure == "" && got.FailureCode != nil {
				t.Errorf("failure_code = %v, want nil", *got.FailureCode)
			}
			if tt.wantFailure != "" && (got.FailureCode == nil || *got.FailureCode != tt.wantFailure) {
				t.Errorf("failure_code = %v, want %s", got.FailureCode, tt.wantFailure)
			}
			if tt.wantRef != (got.GatewayOrderID != nil) {
				t.Errorf("gateway_order_id set = %v, want %v", got.GatewayOrderID != nil, tt.wantRef)
			}
			if gw.calls.Load() != 1 {
				t.Errorf("gateway calls = %d, want 1", gw.calls.Load())
			}
		})
	}
}

func TestRepeatedCreateReturnsSameAttempt(t *testing.T) {
	db := openTestDB(t)
	svc := NewPaymentService(db)
	orderID := uuid.New()
	cleanupOrder(t, db, orderID)

	first, err := svc.CreatePayment(context.Background(), CreatePaymentInput{OrderID: orderID, AmountPaise: 1000})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	second, err := svc.CreatePayment(context.Background(), CreatePaymentInput{OrderID: orderID, AmountPaise: 1000})
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	if !first.Created || second.Created {
		t.Fatalf("created flags = %v, %v; want true, false", first.Created, second.Created)
	}
	if first.Attempt.ID != second.Attempt.ID {
		t.Fatalf("attempt IDs differ: %s vs %s", first.Attempt.ID, second.Attempt.ID)
	}
}

func TestConcurrentCreateYieldsOneWinner(t *testing.T) {
	db := openTestDB(t)
	svc := NewPaymentService(db)
	gw := newGateway(t, paymentgateway.ModeSuccess)
	processor := NewPaymentProcessor(db, gw, time.Second)
	orderID := uuid.New()
	cleanupOrder(t, db, orderID)

	const callers = 10
	var created atomic.Int64
	var wg sync.WaitGroup
	var attemptID atomic.Value
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := svc.CreatePayment(context.Background(), CreatePaymentInput{OrderID: orderID, AmountPaise: 1000})
			if err != nil {
				t.Errorf("create payment: %v", err)
				return
			}
			attemptID.Store(result.Attempt.ID)
			if result.Created {
				created.Add(1)
				processor.Start(result.Attempt)
			}
		}()
	}
	wg.Wait()

	if created.Load() != 1 {
		t.Fatalf("created = %d, want exactly 1", created.Load())
	}
	waitForStatus(t, svc, attemptID.Load().(uuid.UUID), "SUCCEEDED")
	if gw.calls.Load() != 1 {
		t.Fatalf("gateway calls = %d, want 1", gw.calls.Load())
	}
}

func TestStartTwiceCallsGatewayOnce(t *testing.T) {
	db := openTestDB(t)
	svc := NewPaymentService(db)
	gw := newGateway(t, paymentgateway.ModeSuccess)
	processor := NewPaymentProcessor(db, gw, time.Second)
	orderID := uuid.New()
	cleanupOrder(t, db, orderID)

	result, err := svc.CreatePayment(context.Background(), CreatePaymentInput{OrderID: orderID, AmountPaise: 1000})
	if err != nil {
		t.Fatalf("create payment: %v", err)
	}

	processor.Start(result.Attempt)
	processor.Start(result.Attempt)
	waitForStatus(t, svc, result.Attempt.ID, "SUCCEEDED")
	time.Sleep(200 * time.Millisecond) // let a wrongly started second call finish
	if gw.calls.Load() != 1 {
		t.Fatalf("gateway calls = %d, want 1", gw.calls.Load())
	}
}

func TestCreatePaymentValidation(t *testing.T) {
	svc := NewPaymentService(nil) // validation fails before any DB access

	for _, tt := range []struct {
		name  string
		input CreatePaymentInput
		want  error
	}{
		{"zero amount", CreatePaymentInput{OrderID: uuid.New(), AmountPaise: 0}, ErrInvalidAmount},
		{"negative amount", CreatePaymentInput{OrderID: uuid.New(), AmountPaise: -5}, ErrInvalidAmount},
		{"bad currency", CreatePaymentInput{OrderID: uuid.New(), AmountPaise: 100, Currency: "RUPEES"}, ErrInvalidCurrency},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := svc.CreatePayment(context.Background(), tt.input); err != tt.want {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}
