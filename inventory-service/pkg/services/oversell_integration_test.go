package services

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"uuid"

	"github.com/chrisfonseca08/distri/inventory-service/database/models"
	"gorm.io/gorm"
)

// Two hundred buyers race for the final ten units. The conditional inventory
// update is the serialization point: exactly ten reservations may win.
func TestConcurrentBuyersCannotOversellLastTenUnits(t *testing.T) {
	db := openSweeperTestDB(t)
	productID := uuid.New()
	product := models.Product{ID: productID, Name: "oversell test", PricePaise: 100, Currency: "INR"}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("inventory.products").Create(&product).Error; err != nil {
			return err
		}
		return tx.Table("inventory.inventories").Create(&models.Inventory{ProductID: productID, OnHand: 10}).Error
	}); err != nil {
		t.Fatalf("create oversell fixture: %v", err)
	}

	const buyers = 200
	orders := make([]uuid.UUID, buyers)
	for index := range orders {
		orders[index] = uuid.New()
	}
	t.Cleanup(func() {
		_ = db.Exec(`DELETE FROM inventory.reservation_items WHERE reservation_id IN (SELECT id FROM inventory.reservations WHERE order_id IN ?)`, orders).Error
		_ = db.Table("inventory.reservations").Where("order_id IN ?", orders).Delete(&models.Reservation{}).Error
		_ = db.Table("inventory.inventories").Where("product_id = ?", productID).Delete(&models.Inventory{}).Error
		_ = db.Table("inventory.products").Where("id = ?", productID).Delete(&models.Product{}).Error
	})

	service := NewReservationService(db)
	start := make(chan struct{})
	var successes atomic.Int64
	errorsByBuyer := make(chan error, buyers)
	var workers sync.WaitGroup
	for index, orderID := range orders {
		workers.Add(1)
		go func(index int, orderID uuid.UUID) {
			defer workers.Done()
			<-start
			_, err := service.CreateReservation(context.Background(), CreateReservationInput{
				OrderID: orderID, RequestHash: fmt.Sprintf("buyer-%d", index),
				Items: []ReservationItemInput{{ProductID: productID, Quantity: 1}},
			})
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrInsufficientStock):
			default:
				errorsByBuyer <- fmt.Errorf("buyer %d: %w", index, err)
			}
		}(index, orderID)
	}
	close(start)
	workers.Wait()
	close(errorsByBuyer)
	for err := range errorsByBuyer {
		t.Error(err)
	}
	if got := successes.Load(); got != 10 {
		t.Fatalf("successful reservations = %d, want exactly 10", got)
	}

	var inventory models.Inventory
	if err := db.Table("inventory.inventories").Where("product_id = ?", productID).First(&inventory).Error; err != nil {
		t.Fatalf("load final inventory: %v", err)
	}
	if inventory.OnHand < 0 || inventory.Reserved != 10 || inventory.OnHand-inventory.Reserved-inventory.Allocated < 0 {
		t.Fatalf("final stock on_hand=%d reserved=%d allocated=%d; want 10/10/0 and never negative availability", inventory.OnHand, inventory.Reserved, inventory.Allocated)
	}
}
