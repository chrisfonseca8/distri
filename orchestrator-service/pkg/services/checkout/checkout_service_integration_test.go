package checkout

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/orchestrator-service/database/models"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/clients"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/config"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/messages"
	"gorm.io/gorm"
)

type stubInventoryClient struct {
	reserve func(context.Context, uuid.UUID, string, []clients.ReserveItem) (clients.ReserveResult, error)
}

func (s stubInventoryClient) Reserve(
	ctx context.Context,
	orderID uuid.UUID,
	requestHash string,
	items []clients.ReserveItem,
) (clients.ReserveResult, error) {
	return s.reserve(ctx, orderID, requestHash, items)
}

func TestCreateCheckoutCommitsSagaStepsOutboxAndAttempt(t *testing.T) {
	db := checkoutIntegrationDB(t)
	key := "checkout-success-test-" + uuid.New().String()
	cleanupCheckoutIntegration(t, db, key)

	productID := uuid.New()
	reservationID := uuid.New()
	var reserveCalls atomic.Int32
	inventory := stubInventoryClient{reserve: func(
		_ context.Context,
		orderID uuid.UUID,
		_ string,
		items []clients.ReserveItem,
	) (clients.ReserveResult, error) {
		reserveCalls.Add(1)
		if len(items) != 1 || items[0].ProductID != productID || items[0].Quantity != 2 {
			t.Errorf("reserve items = %+v", items)
		}
		return clients.ReserveResult{
			ReservationID: reservationID,
			Status:        "RESERVED",
			TotalPaise:    12345,
			Currency:      "INR",
			Items: []clients.ReservedLine{{
				ProductID:      productID,
				Quantity:       2,
				UnitPricePaise: 6172,
				Currency:       "INR",
			}},
		}, nil
	}}

	const sagaDeadline = 15 * time.Minute
	const stepTimeout = 30 * time.Second
	service := NewCheckoutService(db, inventory, sagaDeadline, stepTimeout)
	before := time.Now().UTC()
	response, err := service.CreateCheckout(context.Background(), CreateCheckoutInput{
		IdempotencyKey: key,
		Items:          []CheckoutItemInput{{ProductID: productID, Quantity: 2}},
	})
	after := time.Now().UTC()
	if err != nil {
		t.Fatalf("create checkout: %v", err)
	}
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("status code = %d, want 202", response.StatusCode)
	}

	var result CreateCheckoutResult
	if err := json.Unmarshal(response.Body, &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.Status != "RUNNING" || result.TotalPaise != 12345 || result.Currency != "INR" {
		t.Fatalf("response = %+v", result)
	}

	var sagaRow models.Saga
	if err := db.Table("orchestrator.sagas").Where("id = ?", result.OrderID).Take(&sagaRow).Error; err != nil {
		t.Fatalf("load saga: %v", err)
	}
	if sagaRow.Status != "RUNNING" || sagaRow.CurrentStep != "CREATE_ORDER" {
		t.Fatalf("saga = %+v", sagaRow)
	}
	if sagaRow.DeadlineAt.Before(before.Add(sagaDeadline)) || sagaRow.DeadlineAt.After(after.Add(sagaDeadline)) {
		t.Fatalf("deadline = %v, want between %v and %v", sagaRow.DeadlineAt, before.Add(sagaDeadline), after.Add(sagaDeadline))
	}

	var steps []models.SagaStep
	if err := db.Table("orchestrator.saga_steps").Where("saga_id = ?", result.OrderID).Find(&steps).Error; err != nil {
		t.Fatalf("load saga steps: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("step count = %d, want 2", len(steps))
	}
	stepsByName := map[string]models.SagaStep{steps[0].Step: steps[0], steps[1].Step: steps[1]}
	reserveStep := stepsByName["RESERVE_STOCK"]
	if reserveStep.Status != "SUCCEEDED" || reserveStep.FinishedAt == nil {
		t.Fatalf("reserve step = %+v", reserveStep)
	}
	createStep := stepsByName["CREATE_ORDER"]
	if createStep.Status != "RUNNING" || createStep.Attempts != 1 || createStep.NextAttemptAt == nil {
		t.Fatalf("create-order step = %+v", createStep)
	}
	if createStep.NextAttemptAt.Before(before.Add(stepTimeout)) || createStep.NextAttemptAt.After(after.Add(stepTimeout)) {
		t.Fatalf("next attempt = %v, want between %v and %v", createStep.NextAttemptAt, before.Add(stepTimeout), after.Add(stepTimeout))
	}

	var outboxRow models.Outbox
	if err := db.Table("orchestrator.outbox").
		Where("payload ->> 'order_id' = ?", result.OrderID.String()).
		Take(&outboxRow).Error; err != nil {
		t.Fatalf("load outbox row: %v", err)
	}
	if outboxRow.MessageID == uuid.Nil() || outboxRow.RoutingKey != messages.OrderCreateRoutingKey {
		t.Fatalf("outbox row = %+v", outboxRow)
	}
	var command messages.OrderCreate
	if err := json.Unmarshal(outboxRow.Payload, &command); err != nil {
		t.Fatalf("decode order.create command: %v", err)
	}
	if command.OrderID != result.OrderID || command.TotalPaise != 12345 || command.Currency != "INR" ||
		len(command.Items) != 1 || command.Items[0].UnitPricePaise != 6172 {
		t.Fatalf("order.create command = %+v", command)
	}

	var attempt models.CheckoutAttempt
	if err := db.Table("orchestrator.checkout_attempts").Where("idempotency_key = ?", key).Take(&attempt).Error; err != nil {
		t.Fatalf("load checkout attempt: %v", err)
	}
	if attempt.Status != "completed" || attempt.ResponseCode == nil || *attempt.ResponseCode != http.StatusAccepted ||
		!bytes.Equal(attempt.ResponseBody, response.Body) {
		t.Fatalf("attempt = %+v", attempt)
	}

	replayed, err := service.CreateCheckout(context.Background(), CreateCheckoutInput{
		IdempotencyKey: key,
		Items:          []CheckoutItemInput{{ProductID: productID, Quantity: 2}},
	})
	if err != nil {
		t.Fatalf("replay checkout: %v", err)
	}
	if replayed.StatusCode != response.StatusCode || !bytes.Equal(replayed.Body, response.Body) {
		t.Fatalf("replayed response = %+v, want %+v", replayed, response)
	}
	if reserveCalls.Load() != 1 {
		t.Fatalf("reserve calls = %d, want 1", reserveCalls.Load())
	}
}

func TestCreateCheckoutStoresDefinitiveInventoryFailure(t *testing.T) {
	db := checkoutIntegrationDB(t)
	key := "checkout-failure-test-" + uuid.New().String()
	cleanupCheckoutIntegration(t, db, key)

	var reserveCalls atomic.Int32
	inventory := stubInventoryClient{reserve: func(
		context.Context,
		uuid.UUID,
		string,
		[]clients.ReserveItem,
	) (clients.ReserveResult, error) {
		reserveCalls.Add(1)
		return clients.ReserveResult{}, clients.ErrInsufficientStock
	}}
	service := NewCheckoutService(db, inventory, 15*time.Minute, 30*time.Second)
	request := CreateCheckoutInput{
		IdempotencyKey: key,
		Items:          []CheckoutItemInput{{ProductID: uuid.New(), Quantity: 1}},
	}

	first, err := service.CreateCheckout(context.Background(), request)
	if err != nil {
		t.Fatalf("create failed checkout: %v", err)
	}
	if first.StatusCode != http.StatusConflict {
		t.Fatalf("status code = %d, want 409", first.StatusCode)
	}
	second, err := service.CreateCheckout(context.Background(), request)
	if err != nil {
		t.Fatalf("replay failed checkout: %v", err)
	}
	if second.StatusCode != first.StatusCode || !bytes.Equal(second.Body, first.Body) {
		t.Fatalf("replayed response = %+v, want %+v", second, first)
	}
	if reserveCalls.Load() != 1 {
		t.Fatalf("reserve calls = %d, want 1", reserveCalls.Load())
	}

	var attempt models.CheckoutAttempt
	if err := db.Table("orchestrator.checkout_attempts").Where("idempotency_key = ?", key).Take(&attempt).Error; err != nil {
		t.Fatalf("load checkout attempt: %v", err)
	}
	if attempt.Status != "completed" || attempt.ResponseCode == nil || *attempt.ResponseCode != http.StatusConflict {
		t.Fatalf("attempt = %+v", attempt)
	}
}

func TestCreateCheckoutLeavesAmbiguousInventoryFailureInProgress(t *testing.T) {
	db := checkoutIntegrationDB(t)
	key := "checkout-ambiguous-test-" + uuid.New().String()
	cleanupCheckoutIntegration(t, db, key)

	inventory := stubInventoryClient{reserve: func(
		context.Context,
		uuid.UUID,
		string,
		[]clients.ReserveItem,
	) (clients.ReserveResult, error) {
		return clients.ReserveResult{}, clients.ErrInventoryUnavailable
	}}
	service := NewCheckoutService(db, inventory, 15*time.Minute, 30*time.Second)
	_, err := service.CreateCheckout(context.Background(), CreateCheckoutInput{
		IdempotencyKey: key,
		Items:          []CheckoutItemInput{{ProductID: uuid.New(), Quantity: 1}},
	})
	if !errors.Is(err, ErrCheckoutInventoryUnavailable) {
		t.Fatalf("error = %v, want ErrCheckoutInventoryUnavailable", err)
	}

	var attempt models.CheckoutAttempt
	if err := db.Table("orchestrator.checkout_attempts").Where("idempotency_key = ?", key).Take(&attempt).Error; err != nil {
		t.Fatalf("load checkout attempt: %v", err)
	}
	if attempt.Status != "in_progress" || attempt.ResponseCode != nil || len(attempt.ResponseBody) != 0 {
		t.Fatalf("attempt = %+v", attempt)
	}
}

func TestCreateCheckoutConcurrentDuplicateReturnsWinningResponse(t *testing.T) {
	db := checkoutIntegrationDB(t)
	key := "checkout-concurrent-test-" + uuid.New().String()
	cleanupCheckoutIntegration(t, db, key)

	productID := uuid.New()
	ready := make(chan struct{})
	var reserveCalls atomic.Int32
	inventory := stubInventoryClient{reserve: func(
		_ context.Context,
		_ uuid.UUID,
		_ string,
		_ []clients.ReserveItem,
	) (clients.ReserveResult, error) {
		if reserveCalls.Add(1) == 2 {
			close(ready)
		}
		<-ready
		return clients.ReserveResult{
			ReservationID: uuid.New(),
			Status:        "RESERVED",
			TotalPaise:    5000,
			Currency:      "INR",
			Items: []clients.ReservedLine{{
				ProductID: productID, Quantity: 1, UnitPricePaise: 5000, Currency: "INR",
			}},
		}, nil
	}}
	service := NewCheckoutService(db, inventory, 15*time.Minute, 30*time.Second)
	request := CreateCheckoutInput{
		IdempotencyKey: key,
		Items:          []CheckoutItemInput{{ProductID: productID, Quantity: 1}},
	}

	type outcome struct {
		response Response
		err      error
	}
	outcomes := make(chan outcome, 2)
	var callers sync.WaitGroup
	callers.Add(2)
	for range 2 {
		go func() {
			defer callers.Done()
			response, err := service.CreateCheckout(context.Background(), request)
			outcomes <- outcome{response: response, err: err}
		}()
	}
	callers.Wait()
	close(outcomes)

	var responses []Response
	for outcome := range outcomes {
		if outcome.err != nil {
			t.Fatalf("concurrent checkout: %v", outcome.err)
		}
		responses = append(responses, outcome.response)
	}
	if len(responses) != 2 || responses[0].StatusCode != http.StatusAccepted ||
		responses[1].StatusCode != http.StatusAccepted || !bytes.Equal(responses[0].Body, responses[1].Body) {
		t.Fatalf("responses = %+v", responses)
	}

	var attempt models.CheckoutAttempt
	if err := db.Table("orchestrator.checkout_attempts").Where("idempotency_key = ?", key).Take(&attempt).Error; err != nil {
		t.Fatalf("load checkout attempt: %v", err)
	}
	var sagaCount int64
	if err := db.Table("orchestrator.sagas").Where("id = ?", attempt.OrderID).Count(&sagaCount).Error; err != nil {
		t.Fatalf("count sagas: %v", err)
	}
	if sagaCount != 1 {
		t.Fatalf("saga count = %d, want 1", sagaCount)
	}
}

func checkoutIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("RUN_CHECKOUT_INTEGRATION") != "1" {
		t.Skip("set RUN_CHECKOUT_INTEGRATION=1 to run database integration tests")
	}
	db, err := config.OpenDB()
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	return db
}

func cleanupCheckoutIntegration(t *testing.T, db *gorm.DB, idempotencyKey string) {
	t.Helper()
	t.Cleanup(func() {
		var attempt models.CheckoutAttempt
		query := db.Table("orchestrator.checkout_attempts").
			Where("idempotency_key = ?", idempotencyKey).
			Take(&attempt)
		if query.Error == nil && attempt.OrderID != nil {
			if err := db.Table("orchestrator.outbox").
				Where("payload ->> 'order_id' = ?", attempt.OrderID.String()).
				Delete(&models.Outbox{}).Error; err != nil {
				t.Errorf("delete outbox rows: %v", err)
			}
			if err := db.Table("orchestrator.sagas").
				Where("id = ?", *attempt.OrderID).
				Delete(&models.Saga{}).Error; err != nil {
				t.Errorf("delete saga: %v", err)
			}
		}
		if query.Error != nil && !errors.Is(query.Error, gorm.ErrRecordNotFound) {
			t.Errorf("load checkout attempt for cleanup: %v", query.Error)
		}
		if err := db.Table("orchestrator.checkout_attempts").
			Where("idempotency_key = ?", idempotencyKey).
			Delete(&models.CheckoutAttempt{}).Error; err != nil {
			t.Errorf("delete checkout attempt: %v", err)
		}
	})
}
