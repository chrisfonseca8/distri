package paymentgateway

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"
)

func TestSimulatorSuccessRange(t *testing.T) {
	orderID := uuid.MustParse("40000000-0000-4000-8000-000000000001")
	simulator, err := NewSimulator(ModeRandom, 0)
	if err != nil {
		t.Fatalf("create simulator: %v", err)
	}

	for _, roll := range []int{0, 59} {
		simulator.roll = func() int { return roll }
		ref, err := simulator.CreateCharge(context.Background(), orderID, 149900)
		if err != nil {
			t.Fatalf("roll %d: create charge: %v", roll, err)
		}
		if ref != "sim_order_"+orderID.String() {
			t.Fatalf("roll %d: unexpected reference %q", roll, ref)
		}
	}
}

func TestSimulatorDeclineRange(t *testing.T) {
	orderID := uuid.MustParse("40000000-0000-4000-8000-000000000002")
	simulator, err := NewSimulator(ModeRandom, 0)
	if err != nil {
		t.Fatalf("create simulator: %v", err)
	}

	for _, roll := range []int{60, 89} {
		simulator.roll = func() int { return roll }
		_, err = simulator.CreateCharge(context.Background(), orderID, 499900)
		var declined *ErrDeclined
		if !errors.As(err, &declined) {
			t.Fatalf("roll %d: expected ErrDeclined, got %v", roll, err)
		}
		if declined.OrderID != orderID {
			t.Fatalf("roll %d: declined order ID = %s, want %s", roll, declined.OrderID, orderID)
		}
	}
}

func TestSimulatorTimeoutRangeWaitsForContext(t *testing.T) {
	simulator, err := NewSimulator(ModeRandom, 0)
	if err != nil {
		t.Fatalf("create simulator: %v", err)
	}

	for _, roll := range []int{90, 99} {
		simulator.roll = func() int { return roll }
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		_, err = simulator.CreateCharge(ctx, uuid.New(), 249900)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("roll %d: expected context deadline exceeded, got %v", roll, err)
		}
	}
}

func TestSimulatorHonorsCancellationDuringLatency(t *testing.T) {
	simulator, err := NewSimulator(ModeSuccess, time.Hour)
	if err != nil {
		t.Fatalf("create simulator: %v", err)
	}
	simulator.roll = func() int { return 0 }

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = simulator.CreateCharge(ctx, uuid.New(), 149900)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestNewSimulatorFromEnv(t *testing.T) {
	t.Setenv("GATEWAY_MODE", ModeDecline)
	t.Setenv("GATEWAY_LATENCY", "25ms")

	simulator, err := NewSimulatorFromEnv()
	if err != nil {
		t.Fatalf("create simulator from env: %v", err)
	}
	if simulator.mode != ModeDecline {
		t.Fatalf("mode = %q, want %q", simulator.mode, ModeDecline)
	}
	if simulator.latency != 25*time.Millisecond {
		t.Fatalf("latency = %s, want 25ms", simulator.latency)
	}
}

func TestNewSimulatorDefaultsToSuccess(t *testing.T) {
	simulator, err := NewSimulator("", 0)
	if err != nil {
		t.Fatalf("create simulator: %v", err)
	}
	if simulator.mode != ModeSuccess {
		t.Fatalf("mode = %q, want %q", simulator.mode, ModeSuccess)
	}
}
