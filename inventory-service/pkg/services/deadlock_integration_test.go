package services

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/inventory-service/database/models"
	"gorm.io/gorm"
)

// Phase D4. Two checkouts want the same two products in opposite order. A barrier after the
// first inventory update makes the interleaving deterministic: each transaction then holds
// the row the other needs. Without a lock order PostgreSQL must abort one with 40P01.

type deadlockFixture struct {
	db            *gorm.DB
	first, second uuid.UUID // product ids
	orders        [2]uuid.UUID
}

func newDeadlockFixture(t *testing.T) *deadlockFixture {
	t.Helper()
	db := openSweeperTestDB(t)
	f := &deadlockFixture{db: db, first: uuid.New(), second: uuid.New(), orders: [2]uuid.UUID{uuid.New(), uuid.New()}}
	for _, id := range []uuid.UUID{f.first, f.second} {
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Table("inventory.products").Create(&models.Product{ID: id, Name: "deadlock test", PricePaise: 100, Currency: "INR"}).Error; err != nil {
				return err
			}
			return tx.Table("inventory.inventories").Create(&models.Inventory{ProductID: id, OnHand: 10}).Error
		}); err != nil {
			t.Fatalf("create fixture: %v", err)
		}
	}
	t.Cleanup(func() {
		for _, order := range f.orders {
			_ = db.Exec(`DELETE FROM inventory.reservation_items WHERE reservation_id IN (SELECT id FROM inventory.reservations WHERE order_id = ?)`, order).Error
			_ = db.Table("inventory.reservations").Where("order_id = ?", order).Delete(&models.Reservation{}).Error
		}
		for _, id := range []uuid.UUID{f.first, f.second} {
			_ = db.Table("inventory.inventories").Where("product_id = ?", id).Delete(&models.Inventory{}).Error
			_ = db.Table("inventory.products").Where("id = ?", id).Delete(&models.Product{}).Error
		}
	})
	return f
}

func (f *deadlockFixture) carts() [2][]ReservationItemInput {
	a, b := ReservationItemInput{ProductID: f.first, Quantity: 1}, ReservationItemInput{ProductID: f.second, Quantity: 1}
	return [2][]ReservationItemInput{{a, b}, {b, a}}
}

// useBarrier makes the first `parties` transactions that finish their first inventory update
// wait for each other (bounded, so a correctly ordered run just proceeds).
func useBarrier(t *testing.T, sorted bool, parties int32) {
	t.Helper()
	prevSort, prevHook := sortItemsForLocking, afterInventoryLock
	var arrived atomic.Int32
	release := make(chan struct{})
	sortItemsForLocking = sorted
	afterInventoryLock = func(index int) {
		if index != 0 || arrived.Add(1) > parties {
			return
		}
		if arrived.Load() == parties {
			close(release)
			return
		}
		select {
		case <-release:
		case <-time.After(1500 * time.Millisecond):
		}
	}
	t.Cleanup(func() { sortItemsForLocking, afterInventoryLock = prevSort, prevHook })
}

func (f *deadlockFixture) run(t *testing.T, create func(CreateReservationInput) error) [2]error {
	t.Helper()
	carts := f.carts()
	var errs [2]error
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = create(CreateReservationInput{OrderID: f.orders[i], RequestHash: "h", Items: carts[i]})
		}()
	}
	wg.Wait()
	return errs
}

func (f *deadlockFixture) assertReserved(t *testing.T, want int64) {
	t.Helper()
	for _, id := range []uuid.UUID{f.first, f.second} {
		var inv models.Inventory
		if err := f.db.Table("inventory.inventories").Where("product_id = ?", id).First(&inv).Error; err != nil {
			t.Fatalf("load inventory: %v", err)
		}
		if inv.Reserved != want || inv.OnHand != 10 {
			t.Fatalf("product %s on_hand=%d reserved=%d, want 10/%d", id, inv.OnHand, inv.Reserved, want)
		}
	}
}

// REPRODUCTION: no sorted order, no retry (a single attempt) -> PostgreSQL aborts one with 40P01.
func TestReversedCartsDeadlockWithoutLockOrder(t *testing.T) {
	f := newDeadlockFixture(t)
	useBarrier(t, false, 2)
	service := NewReservationService(f.db)
	errs := f.run(t, func(in CreateReservationInput) error {
		items, _ := prepareReservationItems(in)
		var out CreateReservationResult
		return service.createReservationOnce(context.Background(), in, items, &out)
	})
	deadlocks, ok := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case retryableConflictCode(err) == "40P01":
			deadlocks++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if deadlocks != 1 || ok != 1 {
		t.Fatalf("deadlocks=%d ok=%d, want exactly one victim and one survivor (errs=%v)", deadlocks, ok, errs)
	}
	f.assertReserved(t, 1) // the victim rolled back completely
}

// FIX 1: the sorted lock order removes the cycle, so there is nothing to retry.
func TestReversedCartsDoNotDeadlockWithSortedLockOrder(t *testing.T) {
	f := newDeadlockFixture(t)
	useBarrier(t, true, 2)
	service := NewReservationService(f.db)
	before := conflictRetries.Load()
	for i, err := range f.run(t, func(in CreateReservationInput) error {
		_, err := service.CreateReservation(context.Background(), in)
		return err
	}) {
		if err != nil {
			t.Fatalf("checkout %d: %v", i, err)
		}
	}
	if got := conflictRetries.Load() - before; got != 0 {
		t.Fatalf("conflict retries = %d, want 0 with a sorted lock order", got)
	}
	f.assertReserved(t, 2)
}

// FIX 2 (defence in depth): if a cycle ever forms again, the victim is re-run a bounded number of
// times and both checkouts succeed.
func TestDeadlockVictimIsRetriedAndSucceeds(t *testing.T) {
	f := newDeadlockFixture(t)
	useBarrier(t, false, 2)
	service := NewReservationService(f.db)
	before := conflictRetries.Load()
	for i, err := range f.run(t, func(in CreateReservationInput) error {
		_, err := service.CreateReservation(context.Background(), in)
		return err
	}) {
		if err != nil {
			t.Fatalf("checkout %d: %v", i, err)
		}
	}
	if got := conflictRetries.Load() - before; got < 1 || got >= maxConflictAttempts*2 {
		t.Fatalf("conflict retries = %d, want 1..%d", got, maxConflictAttempts*2-1)
	}
	f.assertReserved(t, 2)
}
