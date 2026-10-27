package services

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
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
