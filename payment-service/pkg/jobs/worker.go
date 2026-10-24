package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"sync"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/payment-service/database/models"
	"github.com/chrisfonseca08/distri/payment-service/pkg/config"
	"github.com/chrisfonseca08/distri/payment-service/pkg/jobtypes"
	"github.com/chrisfonseca08/distri/payment-service/pkg/services"
)

const jobFinishTimeout = 5 * time.Second

type jobStore interface {
	Claim(context.Context, string, int, time.Duration) ([]models.Job, error)
	Succeed(context.Context, uuid.UUID, string) (bool, error)
	Reschedule(context.Context, uuid.UUID, string, time.Duration, bool, string) (bool, error)
	Kill(context.Context, uuid.UUID, string, string) (bool, error)
}

type paymentProcessor interface {
	ProcessForJob(context.Context, uuid.UUID, int) (services.ProcessOutcome, error)
}

type Worker struct {
	store       jobStore
	processor   paymentProcessor
	config      config.WorkerConfig
	hostname    string
	pid         int
	randomFloat func() float64
}

// NewWorker creates a job worker after validating its configuration.
func NewWorker(
	store jobStore,
	processor paymentProcessor,
	workerConfig config.WorkerConfig,
) (*Worker, error) {
	if err := workerConfig.Validate(); err != nil {
		return nil, fmt.Errorf("validate worker configuration: %w", err)
	}
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("read hostname: %w", err)
	}
	return &Worker{
		store:       store,
		processor:   processor,
		config:      workerConfig,
		hostname:    hostname,
		pid:         os.Getpid(),
		randomFloat: rand.Float64,
	}, nil
}

// Run starts the configured number of worker goroutines and waits for them to stop.
func (w *Worker) Run(ctx context.Context) {
	var workers sync.WaitGroup
	for index := 0; index < w.config.Concurrency; index++ {
		workerID := fmt.Sprintf("%s-%d-%d", w.hostname, w.pid, index)
		workers.Add(1)
		go func() {
			defer workers.Done()
			w.runLoop(ctx, workerID)
		}()
	}
	workers.Wait()
}

// runLoop claims one job at a time and runs it until ctx is cancelled.
func (w *Worker) runLoop(ctx context.Context, workerID string) {
	slog.Info("payment job worker started", "worker_id", workerID)
	defer slog.Info("payment job worker stopped", "worker_id", workerID)

	for ctx.Err() == nil {
		claimed, err := w.store.Claim(ctx, workerID, 1, w.config.Lease)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("claim payment job", "worker_id", workerID, "error", err)
			if !waitForJobs(ctx, w.config.PollInterval) {
				return
			}
			continue
		}
		if len(claimed) == 0 {
			if !waitForJobs(ctx, w.config.PollInterval) {
				return
			}
			continue
		}
		w.runJob(ctx, workerID, claimed[0])
	}
}

// waitForJobs sleeps for interval and returns false if ctx is cancelled first.
func waitForJobs(ctx context.Context, interval time.Duration) bool {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// runJob runs one job and then marks it succeeded, retried with a backoff or dead.
func (w *Worker) runJob(_ context.Context, workerID string, job models.Job) {
	// A shutdown signal stops new claims, but a job whose lease is already held
	// gets its own bounded context so it can drain and persist its result.
	handlerContext, cancelHandler := context.WithTimeout(context.Background(), w.config.HandlerTimeout)
	outcome, handlerErr := w.handleJob(handlerContext, job)
	cancelHandler()

	lastError := ""
	if handlerErr != nil {
		lastError = handlerErr.Error()
	}

	finishContext, cancelFinish := context.WithTimeout(context.Background(), jobFinishTimeout)
	defer cancelFinish()

	var finished bool
	var finishErr error
	switch outcome {
	case services.ProcessDone:
		finished, finishErr = w.store.Succeed(finishContext, job.ID, workerID)
	case services.ProcessRetry:
		if lastError == "" {
			lastError = "temporary payment processing failure"
		}
		if job.Attempts >= job.MaxAttempts {
			lastError = "retries exhausted: " + lastError
			finished, finishErr = w.store.Kill(finishContext, job.ID, workerID, lastError)
			break
		}
		delay := RetryBackoff(
			job.Attempts,
			w.config.RetryBase,
			w.config.RetryCap,
			w.config.RetryJitter,
			w.randomFloat(),
		)
		finished, finishErr = w.store.Reschedule(
			finishContext,
			job.ID,
			workerID,
			delay,
			true,
			lastError,
		)
	case services.ProcessPermanent:
		if lastError == "" {
			lastError = "permanent payment processing failure"
		}
		finished, finishErr = w.store.Kill(finishContext, job.ID, workerID, lastError)
	default:
		lastError = fmt.Sprintf("unknown process outcome %q", outcome)
		finished, finishErr = w.store.Kill(finishContext, job.ID, workerID, lastError)
	}

	if finishErr != nil {
		slog.Error(
			"finish payment job",
			"job_id", job.ID.String(),
			"worker_id", workerID,
			"outcome", outcome,
			"error", finishErr,
		)
		return
	}
	if !finished {
		slog.Warn(
			"payment job lease lost before finish",
			"job_id", job.ID.String(),
			"worker_id", workerID,
			"outcome", outcome,
		)
	}
}

// handleJob runs the payment handler for a job and turns a panic into a retry.
func (w *Worker) handleJob(
	ctx context.Context,
	job models.Job,
) (outcome services.ProcessOutcome, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			outcome = services.ProcessRetry
			err = fmt.Errorf("payment job handler panic: %v", recovered)
		}
	}()

	if job.Type != jobtypes.ProcessPayment {
		return services.ProcessPermanent, fmt.Errorf("unknown payment job type %q", job.Type)
	}
	return w.processor.ProcessForJob(ctx, job.PaymentID, job.Attempts)
}
