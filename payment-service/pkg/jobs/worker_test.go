package jobs

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/payment-service/database/models"
	"github.com/chrisfonseca08/distri/payment-service/pkg/config"
	"github.com/chrisfonseca08/distri/payment-service/pkg/jobtypes"
	"github.com/chrisfonseca08/distri/payment-service/pkg/services"
)

type processForJobFunc func(context.Context, uuid.UUID, int) (services.ProcessOutcome, error)

func (function processForJobFunc) ProcessForJob(
	ctx context.Context,
	paymentID uuid.UUID,
	attempts int,
) (services.ProcessOutcome, error) {
	return function(ctx, paymentID, attempts)
}

type workerFinishCall struct {
	method       string
	delay        time.Duration
	countAttempt bool
	lastError    string
	contextErr   error
}

type recordingStore struct {
	call workerFinishCall
}

func (*recordingStore) Claim(context.Context, string, int, time.Duration) ([]models.Job, error) {
	return nil, nil
}

func (store *recordingStore) Succeed(ctx context.Context, _ uuid.UUID, _ string) (bool, error) {
	store.call = workerFinishCall{method: "succeed", contextErr: ctx.Err()}
	return true, nil
}

func (store *recordingStore) Reschedule(
	ctx context.Context,
	_ uuid.UUID,
	_ string,
	delay time.Duration,
	countAttempt bool,
	lastError string,
) (bool, error) {
	store.call = workerFinishCall{
		method: "reschedule", delay: delay, countAttempt: countAttempt,
		lastError: lastError, contextErr: ctx.Err(),
	}
	return true, nil
}

func (store *recordingStore) Kill(
	ctx context.Context,
	_ uuid.UUID,
	_ string,
	lastError string,
) (bool, error) {
	store.call = workerFinishCall{method: "kill", lastError: lastError, contextErr: ctx.Err()}
	return true, nil
}

func TestWorkerMapsPaymentOutcomes(t *testing.T) {
	tests := []struct {
		name       string
		outcome    services.ProcessOutcome
		handlerErr error
		attempts   int
		wantMethod string
		wantDelay  time.Duration
	}{
		{name: "done", outcome: services.ProcessDone, attempts: 1, wantMethod: "succeed"},
		{name: "retry", outcome: services.ProcessRetry, handlerErr: errors.New("temporary"), attempts: 3, wantMethod: "reschedule", wantDelay: 8 * time.Second},
		{name: "retry exhausted", outcome: services.ProcessRetry, handlerErr: errors.New("temporary"), attempts: 5, wantMethod: "kill"},
		{name: "permanent", outcome: services.ProcessPermanent, handlerErr: errors.New("permanent"), attempts: 1, wantMethod: "kill"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &recordingStore{}
			worker := testWorker(store, processForJobFunc(func(context.Context, uuid.UUID, int) (services.ProcessOutcome, error) {
				return test.outcome, test.handlerErr
			}))
			worker.runJob(context.Background(), "worker-1", models.Job{
				ID: uuid.New(), Type: jobtypes.ProcessPayment, PaymentID: uuid.New(),
				Attempts: test.attempts, MaxAttempts: 5,
			})
			if store.call.method != test.wantMethod {
				t.Fatalf("finish method = %q, want %q", store.call.method, test.wantMethod)
			}
			if store.call.delay != test.wantDelay {
				t.Fatalf("delay = %s, want %s", store.call.delay, test.wantDelay)
			}
			if store.call.contextErr != nil {
				t.Fatalf("finish context was cancelled: %v", store.call.contextErr)
			}
		})
	}
}

func TestWorkerRecoversPaymentHandlerPanic(t *testing.T) {
	store := &recordingStore{}
	worker := testWorker(store, processForJobFunc(func(context.Context, uuid.UUID, int) (services.ProcessOutcome, error) {
		panic("boom")
	}))
	worker.runJob(context.Background(), "worker-1", models.Job{
		ID: uuid.New(), Type: jobtypes.ProcessPayment, PaymentID: uuid.New(),
		Attempts: 1, MaxAttempts: 5,
	})
	if store.call.method != "reschedule" || !store.call.countAttempt {
		t.Fatalf("panic finish call = %#v, want counted reschedule", store.call)
	}
}

func TestWorkerDrainsClaimedJobAfterShutdownSignal(t *testing.T) {
	store := &recordingStore{}
	started := make(chan struct{})
	release := make(chan struct{})
	worker := testWorker(store, processForJobFunc(func(ctx context.Context, _ uuid.UUID, _ int) (services.ProcessOutcome, error) {
		close(started)
		<-release
		if err := ctx.Err(); err != nil {
			return services.ProcessRetry, err
		}
		return services.ProcessDone, nil
	}))

	shutdownContext, cancelShutdown := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.runJob(shutdownContext, "worker-1", models.Job{
			ID: uuid.New(), Type: jobtypes.ProcessPayment, PaymentID: uuid.New(), MaxAttempts: 5,
		})
	}()
	<-started
	cancelShutdown()
	close(release)
	<-done

	if store.call.method != "succeed" {
		t.Fatalf("finish method = %q, want succeed", store.call.method)
	}
}

func testWorker(store jobStore, processor paymentProcessor) *Worker {
	return &Worker{
		store: store, processor: processor,
		config: config.WorkerConfig{
			Concurrency: 1, Lease: 30 * time.Second, HandlerTimeout: 20 * time.Second,
			PollInterval: 200 * time.Millisecond, NotReadyDelay: time.Second,
			RetryBase: 2 * time.Second, RetryCap: time.Minute, RetryJitter: 0.25,
		},
		randomFloat: func() float64 { return 0.5 },
	}
}
