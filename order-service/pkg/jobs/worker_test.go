package jobs

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/order-service/database/models"
	"github.com/chrisfonseca08/distri/order-service/pkg/config"
	"github.com/chrisfonseca08/distri/order-service/pkg/services"
	"github.com/chrisfonseca08/distri/order-service/pkg/utils"
)

type settleForJobFunc func(context.Context, uuid.UUID) (services.SettleOutcome, error)

func (f settleForJobFunc) SettleForJob(
	ctx context.Context,
	orderID uuid.UUID,
) (services.SettleOutcome, error) {
	return f(ctx, orderID)
}

type finishCall struct {
	method       string
	delay        time.Duration
	countAttempt bool
	lastError    string
	contextErr   error
}

type recordingStore struct {
	call finishCall
}

func (s *recordingStore) Claim(
	context.Context,
	string,
	int,
	time.Duration,
) ([]models.Job, error) {
	return nil, nil
}

func (s *recordingStore) Succeed(ctx context.Context, _ uuid.UUID, _ string) (bool, error) {
	s.call = finishCall{method: "succeed", contextErr: ctx.Err()}
	return true, nil
}

func (s *recordingStore) Reschedule(
	ctx context.Context,
	_ uuid.UUID,
	_ string,
	delay time.Duration,
	countAttempt bool,
	lastError string,
) (bool, error) {
	s.call = finishCall{
		method:       "reschedule",
		delay:        delay,
		countAttempt: countAttempt,
		lastError:    lastError,
		contextErr:   ctx.Err(),
	}
	return true, nil
}

func (s *recordingStore) Kill(
	ctx context.Context,
	_ uuid.UUID,
	_ string,
	lastError string,
) (bool, error) {
	s.call = finishCall{method: "kill", lastError: lastError, contextErr: ctx.Err()}
	return true, nil
}

func TestWorkerMapsSettleOutcomes(t *testing.T) {
	temporaryErr := errors.New("temporary failure")
	permanentErr := errors.New("permanent failure")
	tests := []struct {
		name             string
		outcome          services.SettleOutcome
		handlerErr       error
		wantMethod       string
		wantDelay        time.Duration
		wantCountAttempt bool
		wantLastError    string
	}{
		{name: "done", outcome: services.SettleDone, wantMethod: "succeed"},
		{
			name:       "not ready",
			outcome:    services.SettleNotReady,
			wantMethod: "reschedule",
			wantDelay:  time.Second,
		},
		{
			name:             "retry",
			outcome:          services.SettleRetry,
			handlerErr:       temporaryErr,
			wantMethod:       "reschedule",
			wantDelay:        5 * time.Second,
			wantCountAttempt: true,
			wantLastError:    temporaryErr.Error(),
		},
		{
			name:          "permanent",
			outcome:       services.SettlePermanent,
			handlerErr:    permanentErr,
			wantMethod:    "kill",
			wantLastError: permanentErr.Error(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &recordingStore{}
			worker := testWorker(store, settleForJobFunc(func(context.Context, uuid.UUID) (services.SettleOutcome, error) {
				return test.outcome, test.handlerErr
			}))

			worker.runJob(context.Background(), "worker-1", models.Job{
				ID:      uuid.New(),
				Type:    utils.JobTypeSettleOrder,
				OrderID: uuid.New(),
			})

			if store.call.method != test.wantMethod {
				t.Errorf("finish method = %q, want %q", store.call.method, test.wantMethod)
			}
			if store.call.delay != test.wantDelay {
				t.Errorf("delay = %s, want %s", store.call.delay, test.wantDelay)
			}
			if store.call.countAttempt != test.wantCountAttempt {
				t.Errorf("countAttempt = %v, want %v", store.call.countAttempt, test.wantCountAttempt)
			}
			if store.call.lastError != test.wantLastError {
				t.Errorf("lastError = %q, want %q", store.call.lastError, test.wantLastError)
			}
			if store.call.contextErr != nil {
				t.Errorf("finish context was cancelled: %v", store.call.contextErr)
			}
		})
	}
}

func TestWorkerUsesFreshContextAfterHandlerCancellation(t *testing.T) {
	store := &recordingStore{}
	worker := testWorker(store, settleForJobFunc(func(ctx context.Context, _ uuid.UUID) (services.SettleOutcome, error) {
		<-ctx.Done()
		return services.SettleRetry, ctx.Err()
	}))
	worker.config.HandlerTimeout = time.Millisecond

	worker.runJob(context.Background(), "worker-1", models.Job{
		ID:      uuid.New(),
		Type:    utils.JobTypeSettleOrder,
		OrderID: uuid.New(),
	})

	if store.call.method != "reschedule" {
		t.Fatalf("finish method = %q, want reschedule", store.call.method)
	}
	if store.call.contextErr != nil {
		t.Fatalf("finish context was cancelled: %v", store.call.contextErr)
	}
}

func TestWorkerRecoversHandlerPanic(t *testing.T) {
	store := &recordingStore{}
	worker := testWorker(store, settleForJobFunc(func(context.Context, uuid.UUID) (services.SettleOutcome, error) {
		panic("boom")
	}))

	worker.runJob(context.Background(), "worker-1", models.Job{
		ID:      uuid.New(),
		Type:    utils.JobTypeSettleOrder,
		OrderID: uuid.New(),
	})

	if store.call.method != "reschedule" || !store.call.countAttempt {
		t.Fatalf("panic finish call = %#v, want counted reschedule", store.call)
	}
}

func TestWorkerKillsUnknownJobType(t *testing.T) {
	store := &recordingStore{}
	worker := testWorker(store, settleForJobFunc(func(context.Context, uuid.UUID) (services.SettleOutcome, error) {
		t.Fatal("settler called for unknown job type")
		return "", nil
	}))

	worker.runJob(context.Background(), "worker-1", models.Job{
		ID:      uuid.New(),
		Type:    "unknown",
		OrderID: uuid.New(),
	})

	if store.call.method != "kill" {
		t.Fatalf("finish method = %q, want kill", store.call.method)
	}
}

func testWorker(store jobStore, settler orderSettler) *Worker {
	return &Worker{
		store:        store,
		orderService: settler,
		config: config.WorkerConfig{
			Concurrency:    1,
			Lease:          30 * time.Second,
			HandlerTimeout: 20 * time.Second,
			PollInterval:   200 * time.Millisecond,
			NotReadyDelay:  time.Second,
			RetryDelay:     5 * time.Second,
		},
	}
}
