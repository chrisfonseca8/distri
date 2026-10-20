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
	"github.com/chrisfonseca08/distri/payment-service/pkg/jobtypes"
	"github.com/chrisfonseca08/distri/payment-service/pkg/payment_gateway"
	"gorm.io/gorm"
)

type countingGateway struct {
	inner paymentgateway.Gateway
	calls atomic.Int64
}

func (g *countingGateway) CreateCharge(
	ctx context.Context,
	orderID uuid.UUID,
	amountPaise int64,
) (string, error) {
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
	simulator, err := paymentgateway.NewSimulator(mode, 0)
	if err != nil {
		t.Fatalf("create simulator: %v", err)
	}
	return &countingGateway{inner: simulator}
}

func cleanupOrder(t *testing.T, db *gorm.DB, orderID uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		if err := db.Exec(`DELETE FROM payments.jobs
			WHERE payment_id IN (
				SELECT id FROM payments.payment_attempts WHERE order_id = ?
			)`, orderID).Error; err != nil {
			t.Errorf("delete payment jobs: %v", err)
		}
		if err := db.Where("order_id = ?", orderID).Delete(&models.PaymentAttempt{}).Error; err != nil {
			t.Errorf("delete payment attempt: %v", err)
		}
	})
}

func TestPaymentFlowOutcomes(t *testing.T) {
	db := openTestDB(t)
	service := NewPaymentService(db)

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

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gateway := newGateway(t, test.mode)
			handler := NewPaymentJobHandler(db, gateway, 100*time.Millisecond)
			orderID := uuid.New()
			cleanupOrder(t, db, orderID)

			result, err := service.CreatePayment(context.Background(), CreatePaymentInput{
				OrderID: orderID, AmountPaise: 149900, Currency: "inr",
			})
			if err != nil {
				t.Fatalf("create payment: %v", err)
			}
			if !result.Created || result.Attempt.Status != "CREATED" || result.Attempt.Currency != "INR" {
				t.Fatalf("unexpected create result: %+v", result)
			}

			outcome, err := handler.ProcessForJob(context.Background(), result.Attempt.ID, 1)
			if err != nil || outcome != ProcessDone {
				t.Fatalf("process payment = %q, %v; want Done, nil", outcome, err)
			}
			got, err := service.GetPayment(context.Background(), result.Attempt.ID)
			if err != nil {
				t.Fatalf("get payment: %v", err)
			}
			if got.Status != test.wantStatus {
				t.Fatalf("status = %s, want %s", got.Status, test.wantStatus)
			}
			if test.wantFailure == "" && got.FailureCode != nil {
				t.Errorf("failure_code = %v, want nil", *got.FailureCode)
			}
			if test.wantFailure != "" && (got.FailureCode == nil || *got.FailureCode != test.wantFailure) {
				t.Errorf("failure_code = %v, want %s", got.FailureCode, test.wantFailure)
			}
			if test.wantRef != (got.GatewayOrderID != nil) {
				t.Errorf("gateway_order_id set = %v, want %v", got.GatewayOrderID != nil, test.wantRef)
			}
			assertJobCount(t, db, result.Attempt.ID, jobtypes.ProcessPayment, 1)
			assertJobCount(t, db, result.Attempt.ID, jobtypes.NotifyOrder, 1)
			if gateway.calls.Load() != 1 {
				t.Errorf("gateway calls = %d, want 1", gateway.calls.Load())
			}
		})
	}
}

func TestReclaimedPendingPaymentBecomesUnknownWithoutGatewayCall(t *testing.T) {
	db := openTestDB(t)
	service := NewPaymentService(db)
	gateway := newGateway(t, paymentgateway.ModeSuccess)
	handler := NewPaymentJobHandler(db, gateway, time.Second)
	orderID := uuid.New()
	cleanupOrder(t, db, orderID)

	result, err := service.CreatePayment(context.Background(), CreatePaymentInput{
		OrderID: orderID, AmountPaise: 1000,
	})
	if err != nil {
		t.Fatalf("create payment: %v", err)
	}
	if err := db.Table("payments.payment_attempts").
		Where("id = ? AND status = ?", result.Attempt.ID, "CREATED").
		Update("status", "PENDING").Error; err != nil {
		t.Fatalf("fixture pending payment: %v", err)
	}

	outcome, err := handler.ProcessForJob(context.Background(), result.Attempt.ID, 2)
	if err != nil || outcome != ProcessDone {
		t.Fatalf("process reclaimed payment = %q, %v; want Done, nil", outcome, err)
	}
	stored, err := service.GetPayment(context.Background(), result.Attempt.ID)
	if err != nil {
		t.Fatalf("get payment: %v", err)
	}
	if stored.Status != "UNKNOWN" {
		t.Fatalf("status = %s, want UNKNOWN", stored.Status)
	}
	if gateway.calls.Load() != 0 {
		t.Fatalf("gateway calls = %d, want 0", gateway.calls.Load())
	}
	assertJobCount(t, db, result.Attempt.ID, jobtypes.NotifyOrder, 1)
}

func TestRepeatedCreateReturnsSameAttemptAndOneProcessJob(t *testing.T) {
	db := openTestDB(t)
	service := NewPaymentService(db)
	orderID := uuid.New()
	cleanupOrder(t, db, orderID)

	first, err := service.CreatePayment(context.Background(), CreatePaymentInput{OrderID: orderID, AmountPaise: 1000})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	second, err := service.CreatePayment(context.Background(), CreatePaymentInput{OrderID: orderID, AmountPaise: 1000})
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	if !first.Created || second.Created {
		t.Fatalf("created flags = %v, %v; want true, false", first.Created, second.Created)
	}
	if first.Attempt.ID != second.Attempt.ID {
		t.Fatalf("attempt IDs differ: %s vs %s", first.Attempt.ID, second.Attempt.ID)
	}
	assertJobCount(t, db, first.Attempt.ID, jobtypes.ProcessPayment, 1)
}

func TestConcurrentCreateYieldsOnePaymentAndOneProcessJob(t *testing.T) {
	db := openTestDB(t)
	service := NewPaymentService(db)
	orderID := uuid.New()
	cleanupOrder(t, db, orderID)

	const callers = 10
	var created atomic.Int64
	var waitGroup sync.WaitGroup
	var attemptID atomic.Value
	for range callers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			result, err := service.CreatePayment(context.Background(), CreatePaymentInput{
				OrderID: orderID, AmountPaise: 1000,
			})
			if err != nil {
				t.Errorf("create payment: %v", err)
				return
			}
			attemptID.Store(result.Attempt.ID)
			if result.Created {
				created.Add(1)
			}
		}()
	}
	waitGroup.Wait()

	if created.Load() != 1 {
		t.Fatalf("created = %d, want exactly 1", created.Load())
	}
	assertJobCount(t, db, attemptID.Load().(uuid.UUID), jobtypes.ProcessPayment, 1)
}

func TestProcessingFinalPaymentDoesNotCallGatewayAgain(t *testing.T) {
	db := openTestDB(t)
	service := NewPaymentService(db)
	gateway := newGateway(t, paymentgateway.ModeSuccess)
	handler := NewPaymentJobHandler(db, gateway, time.Second)
	orderID := uuid.New()
	cleanupOrder(t, db, orderID)

	result, err := service.CreatePayment(context.Background(), CreatePaymentInput{
		OrderID: orderID, AmountPaise: 1000,
	})
	if err != nil {
		t.Fatalf("create payment: %v", err)
	}
	if outcome, err := handler.ProcessForJob(context.Background(), result.Attempt.ID, 1); err != nil || outcome != ProcessDone {
		t.Fatalf("first process = %q, %v", outcome, err)
	}
	if outcome, err := handler.ProcessForJob(context.Background(), result.Attempt.ID, 2); err != nil || outcome != ProcessDone {
		t.Fatalf("second process = %q, %v", outcome, err)
	}
	if gateway.calls.Load() != 1 {
		t.Fatalf("gateway calls = %d, want 1", gateway.calls.Load())
	}
}

func TestCreatePaymentValidation(t *testing.T) {
	service := NewPaymentService(nil)
	for _, test := range []struct {
		name  string
		input CreatePaymentInput
		want  error
	}{
		{"zero amount", CreatePaymentInput{OrderID: uuid.New(), AmountPaise: 0}, ErrInvalidAmount},
		{"negative amount", CreatePaymentInput{OrderID: uuid.New(), AmountPaise: -5}, ErrInvalidAmount},
		{"bad currency", CreatePaymentInput{OrderID: uuid.New(), AmountPaise: 100, Currency: "RUPEES"}, ErrInvalidCurrency},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := service.CreatePayment(context.Background(), test.input); err != test.want {
				t.Fatalf("err = %v, want %v", err, test.want)
			}
		})
	}
}

func assertJobCount(t *testing.T, db *gorm.DB, paymentID uuid.UUID, jobType string, want int64) {
	t.Helper()
	var count int64
	if err := db.Table("payments.jobs").
		Where("payment_id = ? AND type = ?", paymentID, jobType).
		Count(&count).Error; err != nil {
		t.Fatalf("count %s jobs: %v", jobType, err)
	}
	if count != want {
		t.Fatalf("%s jobs = %d, want %d", jobType, count, want)
	}
}
