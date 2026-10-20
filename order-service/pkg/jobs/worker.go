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

	"github.com/chrisfonseca08/distri/order-service/database/models"
	"github.com/chrisfonseca08/distri/order-service/pkg/config"
	"github.com/chrisfonseca08/distri/order-service/pkg/services"
	"github.com/chrisfonseca08/distri/order-service/pkg/utils"
)

const jobFinishTimeout = 5 * time.Second

type jobStore interface {
	Claim(context.Context, string, int, time.Duration) ([]models.Job, error)
	Succeed(context.Context, uuid.UUID, string) (bool, error)
	Reschedule(context.Context, uuid.UUID, string, time.Duration, bool, string) (bool, error)
	Kill(context.Context, uuid.UUID, string, string) (bool, error)
}

type orderSettler interface {
	SettleForJob(context.Context, uuid.UUID) (services.SettleOutcome, error)
}

type Worker struct {
	store        jobStore
	orderService orderSettler
	config       config.WorkerConfig
	hostname     string
	pid          int
	randomFloat  func() float64
}

func NewWorker(
	store jobStore,
	orderService orderSettler,
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
		store:        store,
		orderService: orderService,
		config:       workerConfig,
		hostname:     hostname,
		pid:          os.Getpid(),
		randomFloat:  rand.Float64,
	}, nil
}

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

func (w *Worker) runLoop(ctx context.Context, workerID string) {
	slog.Info("job worker started", "worker_id", workerID)
	defer slog.Info("job worker stopped", "worker_id", workerID)

	for ctx.Err() == nil {
		claimed, err := w.store.Claim(ctx, workerID, 1, w.config.Lease)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("claim job", "worker_id", workerID, "error", err)
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

func (w *Worker) runJob(ctx context.Context, workerID string, job models.Job) {
	handlerContext, cancelHandler := context.WithTimeout(ctx, w.config.HandlerTimeout)
	outcome, handlerErr := w.handleJob(handlerContext, job)
	cancelHandler()

	lastError := ""
	if handlerErr != nil {
		lastError = handlerErr.Error()
	}

	finishContext, cancelFinish := context.WithTimeout(context.Background(), jobFinishTimeout)
	defer cancelFinish()

	var (
		finished  bool
		finishErr error
	)
	switch outcome {
	case services.SettleDone:
		finished, finishErr = w.store.Succeed(finishContext, job.ID, workerID)
	case services.SettleNotReady:
		finished, finishErr = w.store.Reschedule(
			finishContext,
			job.ID,
			workerID,
			w.config.NotReadyDelay,
			false,
			lastError,
		)
	case services.SettleRetry:
		if lastError == "" {
			lastError = "temporary settlement failure"
		}
		if job.Attempts >= job.MaxAttempts {
			lastError = "retries exhausted: " + lastError
			finished, finishErr = w.store.Kill(
				finishContext,
				job.ID,
				workerID,
				lastError,
			)
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
	case services.SettlePermanent:
		if lastError == "" {
			lastError = "permanent settlement failure"
		}
		finished, finishErr = w.store.Kill(finishContext, job.ID, workerID, lastError)
	default:
		lastError = fmt.Sprintf("unknown settle outcome %q", outcome)
		finished, finishErr = w.store.Kill(finishContext, job.ID, workerID, lastError)
	}

	if finishErr != nil {
		slog.Error(
			"finish job",
			"job_id", job.ID.String(),
			"worker_id", workerID,
			"outcome", outcome,
			"error", finishErr,
		)
		return
	}
	if !finished {
		slog.Warn(
			"job lease lost before finish",
			"job_id", job.ID.String(),
			"worker_id", workerID,
			"outcome", outcome,
		)
	}
}

func (w *Worker) handleJob(
	ctx context.Context,
	job models.Job,
) (outcome services.SettleOutcome, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			outcome = services.SettleRetry
			err = fmt.Errorf("job handler panic: %v", recovered)
		}
	}()

	switch job.Type {
	case utils.JobTypeSettleOrder:
		return w.orderService.SettleForJob(ctx, job.OrderID)
	default:
		return services.SettlePermanent, fmt.Errorf("unknown job type %q", job.Type)
	}
}
