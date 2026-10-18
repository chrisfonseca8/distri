package paymentgateway

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"time"
	"uuid"
)

const (
	ModeSuccess = "success"
	ModeDecline = "decline"
	ModeTimeout = "timeout"
	ModeRandom  = "random"

	defaultMode    = ModeSuccess
	defaultLatency = 200 * time.Millisecond
	successPercent = 60
	declinePercent = 30
	totalPercent   = 100
)

// Gateway is the provider behavior required by payment handlers.
type Gateway interface {
	CreateCharge(ctx context.Context, orderID uuid.UUID, amountPaise int64) (ref string, err error)
}

// ErrDeclined represents a final, known refusal from the payment provider.
type ErrDeclined struct {
	OrderID uuid.UUID
}

func (e *ErrDeclined) Error() string {
	return fmt.Sprintf("payment for order %s was declined", e.OrderID)
}

// Simulator is a controllable stand-in for an external payment provider.
type Simulator struct {
	mode    string
	latency time.Duration
	roll    func() int
}

var _ Gateway = (*Simulator)(nil)

// NewSimulator validates and creates a simulator.
func NewSimulator(mode string, latency time.Duration) (*Simulator, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = defaultMode
	}
	if mode != ModeSuccess && mode != ModeDecline && mode != ModeTimeout && mode != ModeRandom {
		return nil, fmt.Errorf("unsupported gateway mode %q", mode)
	}
	if latency < 0 {
		return nil, fmt.Errorf("gateway latency cannot be negative")
	}

	return &Simulator{
		mode:    mode,
		latency: latency,
		roll: func() int {
			return rand.IntN(totalPercent)
		},
	}, nil
}

// NewSimulatorFromEnv creates a simulator from GATEWAY_MODE and
// GATEWAY_LATENCY. An empty mode defaults to success.
func NewSimulatorFromEnv() (*Simulator, error) {
	mode := os.Getenv("GATEWAY_MODE")

	latency := defaultLatency
	if value := os.Getenv("GATEWAY_LATENCY"); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return nil, fmt.Errorf("GATEWAY_LATENCY must be a duration: %w", err)
		}
		latency = parsed
	}

	return NewSimulator(mode, latency)
}

// CreateCharge simulates success, a final decline, or an ambiguous timeout.
func (s *Simulator) CreateCharge(
	ctx context.Context,
	orderID uuid.UUID,
	amountPaise int64,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	mode := s.mode
	if mode == ModeRandom {
		roll := s.roll()
		if roll < 0 || roll >= totalPercent {
			return "", fmt.Errorf("gateway outcome roll must be between 0 and 99, got %d", roll)
		}

		switch {
		case roll < successPercent:
			mode = ModeSuccess
		case roll < successPercent+declinePercent:
			mode = ModeDecline
		default:
			mode = ModeTimeout
		}
	}

	if mode == ModeTimeout {
		<-ctx.Done()
		return "", ctx.Err()
	}

	timer := time.NewTimer(s.latency)
	defer timer.Stop()

	select {
	case <-timer.C:
	case <-ctx.Done():
		return "", ctx.Err()
	}

	if mode == ModeSuccess {
		return "sim_order_" + orderID.String(), nil
	}

	return "", &ErrDeclined{OrderID: orderID}
}
