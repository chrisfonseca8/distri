package sweeper

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/order-service/pkg/clients"
	"github.com/chrisfonseca08/distri/order-service/pkg/config"
	"github.com/chrisfonseca08/distri/order-service/pkg/services"
	"github.com/chrisfonseca08/distri/order-service/pkg/utils/state"
)

type fakeSweepInventory struct {
	reservations []clients.ReservationSummary
	cutoff       time.Time
	limit        int
	released     []uuid.UUID
	mu           sync.Mutex
}

func (f *fakeSweepInventory) ListReserved(
	_ context.Context,
	cutoff time.Time,
	after *clients.ReservationCursor,
	limit int,
) ([]clients.ReservationSummary, error) {
	f.cutoff = cutoff
	f.limit = limit
	start := 0
	if after != nil {
		start = len(f.reservations)
		for index, reservation := range f.reservations {
			if reservation.CreatedAt.Equal(after.CreatedAt) && reservation.ReservationID == after.ID {
				start = index + 1
				break
			}
		}
	}
	end := min(start+limit, len(f.reservations))
	return f.reservations[start:end], nil
}

func TestReservationSweeperPagesPastPendingReservations(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	reservations := make([]clients.ReservationSummary, 0, 151)
	statuses := make(map[uuid.UUID]state.OrderStatus, 150)
	for index := range 150 {
		orderID := uuid.New()
		statuses[orderID] = state.OrderPendingPayment
		reservations = append(reservations, clients.ReservationSummary{
			ReservationID: uuid.New(),
			OrderID:       orderID,
			Status:        "RESERVED",
			CreatedAt:     now.Add(-20*time.Minute + time.Duration(index)*time.Millisecond),
		})
	}
	orphanID := uuid.New()
	reservations = append(reservations, clients.ReservationSummary{
		ReservationID: uuid.New(),
		OrderID:       orphanID,
		Status:        "RESERVED",
		CreatedAt:     now.Add(-19 * time.Minute),
	})

	inventory := &fakeSweepInventory{reservations: reservations}
	sweeper, err := NewReservationSweeper(inventory, fakeOrderStatuses{
		statuses: statuses,
		errors:   map[uuid.UUID]error{orphanID: services.ErrOrderNotFound},
	}, config.SweeperConfig{
		ReservationTTL: 10 * time.Minute,
		Interval:       time.Minute,
		BatchSize:      100,
	})
	if err != nil {
		t.Fatalf("create sweeper: %v", err)
	}
	sweeper.now = func() time.Time { return now }

	if err := sweeper.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(inventory.released) != 1 || inventory.released[0] != orphanID {
		t.Fatalf("released = %v, want orphan %s", inventory.released, orphanID)
	}
}

func (f *fakeSweepInventory) Release(
	_ context.Context,
	orderID uuid.UUID,
) (clients.ChangeReservationResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released = append(f.released, orderID)
	return clients.ChangeReservationResult{OrderID: orderID, Status: "RELEASED"}, nil
}

type fakeOrderStatuses struct {
	statuses map[uuid.UUID]state.OrderStatus
	errors   map[uuid.UUID]error
}

func (f fakeOrderStatuses) GetOrderStatus(
	_ context.Context,
	orderID uuid.UUID,
) (state.OrderStatus, error) {
	if err := f.errors[orderID]; err != nil {
		return "", err
	}
	return f.statuses[orderID], nil
}

func TestReservationSweeperDecisionTable(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	orphanID := uuid.New()
	cancelledID := uuid.New()
	expiredID := uuid.New()
	paidID := uuid.New()
	pendingID := uuid.New()
	reservations := []clients.ReservationSummary{
		{ReservationID: uuid.New(), OrderID: orphanID},
		{ReservationID: uuid.New(), OrderID: cancelledID},
		{ReservationID: uuid.New(), OrderID: expiredID},
		{ReservationID: uuid.New(), OrderID: paidID},
		{ReservationID: uuid.New(), OrderID: pendingID},
	}
	inventory := &fakeSweepInventory{reservations: reservations}
	orders := fakeOrderStatuses{
		statuses: map[uuid.UUID]state.OrderStatus{
			cancelledID: state.OrderCancelled,
			expiredID:   state.OrderExpired,
			paidID:      state.OrderPaid,
			pendingID:   state.OrderPendingPayment,
		},
		errors: map[uuid.UUID]error{orphanID: services.ErrOrderNotFound},
	}
	sweeper, err := NewReservationSweeper(inventory, orders, config.SweeperConfig{
		ReservationTTL: 10 * time.Minute,
		Interval:       time.Minute,
		BatchSize:      50,
	})
	if err != nil {
		t.Fatalf("create sweeper: %v", err)
	}
	sweeper.now = func() time.Time { return now }

	if err := sweeper.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if !inventory.cutoff.Equal(now.Add(-10 * time.Minute)) {
		t.Fatalf("cutoff = %s, want %s", inventory.cutoff, now.Add(-10*time.Minute))
	}
	if inventory.limit != 50 {
		t.Fatalf("limit = %d, want 50", inventory.limit)
	}

	wantReleased := map[uuid.UUID]bool{orphanID: true, cancelledID: true, expiredID: true}
	if len(inventory.released) != len(wantReleased) {
		t.Fatalf("released = %v, want orphan/cancelled/expired", inventory.released)
	}
	for _, orderID := range inventory.released {
		if !wantReleased[orderID] {
			t.Fatalf("unexpected release for order %s", orderID)
		}
	}
	if wantReleased[paidID] || wantReleased[pendingID] {
		t.Fatal("test setup incorrectly allows paid or pending release")
	}
}

func TestReservationSweeperDoesNotReleaseOnLookupFailure(t *testing.T) {
	orderID := uuid.New()
	inventory := &fakeSweepInventory{reservations: []clients.ReservationSummary{{
		ReservationID: uuid.New(), OrderID: orderID,
	}}}
	sweeper, err := NewReservationSweeper(inventory, fakeOrderStatuses{
		statuses: map[uuid.UUID]state.OrderStatus{},
		errors:   map[uuid.UUID]error{orderID: errors.New("database unavailable")},
	}, config.SweeperConfig{ReservationTTL: time.Minute, Interval: time.Minute, BatchSize: 1})
	if err != nil {
		t.Fatalf("create sweeper: %v", err)
	}

	if err := sweeper.Sweep(context.Background()); err == nil {
		t.Fatal("sweep returned nil for order lookup failure")
	}
	if len(inventory.released) != 0 {
		t.Fatalf("released orders = %v, want none", inventory.released)
	}
}
