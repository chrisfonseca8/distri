package services

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestWebhookProcessorDrainsConsecutiveBatchesWithoutIdleDelay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	started := time.Now()
	runWebhookProcessor(ctx, time.Second, time.Second, func(context.Context) (int, error) {
		switch calls.Add(1) {
		case 1, 2:
			return 100, nil
		default:
			cancel()
			return 0, nil
		}
	})
	if calls.Load() != 3 {
		t.Fatalf("process calls = %d, want 3", calls.Load())
	}
	if elapsed := time.Since(started); elapsed >= 500*time.Millisecond {
		t.Fatalf("consecutive batches incurred idle delay: %s", elapsed)
	}
}

func TestWebhookProcessorWaitsWhenInboxIsIdle(t *testing.T) {
	const idleDelay = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	started := time.Now()
	runWebhookProcessor(ctx, idleDelay, time.Second, func(context.Context) (int, error) {
		if calls.Add(1) == 2 {
			cancel()
		}
		return 0, nil
	})
	elapsed := time.Since(started)
	if calls.Load() != 2 {
		t.Fatalf("process calls = %d, want 2", calls.Load())
	}
	if elapsed < idleDelay {
		t.Fatalf("idle inbox was repolled too soon: %s", elapsed)
	}
}

func TestNextWebhookErrorDelayBacksOffAndCaps(t *testing.T) {
	const cap = time.Second
	tests := []struct {
		current time.Duration
		want    time.Duration
	}{
		{100 * time.Millisecond, 200 * time.Millisecond},
		{200 * time.Millisecond, 400 * time.Millisecond},
		{600 * time.Millisecond, cap},
		{cap, cap},
	}
	for _, test := range tests {
		if got := nextWebhookErrorDelay(test.current, cap); got != test.want {
			t.Fatalf("nextWebhookErrorDelay(%s, %s) = %s, want %s", test.current, cap, got, test.want)
		}
	}
}

func TestWaitForWebhookPassStopsPromptlyOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	if waitForWebhookPass(ctx, time.Second) {
		t.Fatal("wait returned true after cancellation")
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("cancelled wait took %s", elapsed)
	}
}

func TestRunWebhookWorkersStartsEveryWorkerAndWaitsForAllToStop(t *testing.T) {
	const workers = 3
	ctx, cancel := context.WithCancel(context.Background())
	var running, peak, stopped atomic.Int32
	var seen sync.Map
	allStarted := make(chan struct{})
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		runWebhookWorkers(ctx, workers, func(ctx context.Context, worker int) {
			seen.Store(worker, true)
			now := running.Add(1)
			for {
				old := peak.Load()
				if now <= old || peak.CompareAndSwap(old, now) {
					break
				}
			}
			if now == workers {
				close(allStarted)
			}
			<-ctx.Done()
			time.Sleep(30 * time.Millisecond) // a slow worker: the call must still wait for it
			stopped.Add(1)
		})
	}()
	select {
	case <-allStarted:
	case <-time.After(2 * time.Second):
		t.Fatalf("only %d of %d workers started", running.Load(), workers)
	}
	select {
	case <-returned:
		t.Fatal("RunWebhookWorkers returned while workers were still running")
	default:
	}
	cancel()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("RunWebhookWorkers did not return after cancellation")
	}
	if stopped.Load() != workers || peak.Load() != workers {
		t.Fatalf("stopped=%d peak concurrent=%d, want %d and %d", stopped.Load(), peak.Load(), workers, workers)
	}
	for i := 0; i < workers; i++ {
		if _, ok := seen.Load(i); !ok {
			t.Fatalf("worker %d never ran", i)
		}
	}
}

func TestNewWebhookProcessorsGivesEachWorkerItsOwnLeaseIdentity(t *testing.T) {
	config := WebhookProcessorConfig{
		Interval: time.Second, Batch: 20, Lease: 30 * time.Second, MaxAttempts: 3,
		BackoffBase: time.Second, BackoffCap: time.Minute, PaymentWindow: time.Minute, GatewayName: "razorpay",
	}
	processors, err := NewWebhookProcessors(&gorm.DB{}, config, 4)
	if err != nil || len(processors) != 4 {
		t.Fatalf("processors=%d err=%v, want 4", len(processors), err)
	}
	ids := map[string]bool{}
	for _, processor := range processors {
		if processor.config.Batch != 20 || processor.config.Interval != time.Second {
			t.Fatalf("worker settings differ from the shared config: %+v", processor.config)
		}
		ids[processor.workerID] = true
	}
	if len(ids) != 4 {
		t.Fatalf("worker ids %v: the lease guard needs a distinct id per worker", ids)
	}
	if _, err := NewWebhookProcessors(&gorm.DB{}, config, 0); err == nil {
		t.Fatal("zero workers must be rejected")
	}
	if _, err := NewWebhookProcessors(nil, config, 2); err == nil {
		t.Fatal("a missing database must be rejected")
	}
}
