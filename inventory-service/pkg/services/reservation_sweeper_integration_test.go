package services

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/inventory-service/database/models"
	"github.com/chrisfonseca08/distri/inventory-service/pkg/config"
	"gorm.io/gorm"
)

func openSweeperTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("RUN_INVENTORY_INTEGRATION") != "1" {
		t.Skip("set RUN_INVENTORY_INTEGRATION=1 to run database integration tests")
	}
	db, err := config.OpenDB()
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get SQL database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func TestListOldReservedAndConcurrentReleaseIsIdempotent(t *testing.T) {
	db := openSweeperTestDB(t)
	service := NewReservationService(db)
	productID := uuid.New()
	orderID := uuid.New()
	product := models.Product{ID: productID, Name: "sweeper test", PricePaise: 100, Currency: "INR"}
	inventory := models.Inventory{ProductID: productID, OnHand: 10}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("inventory.products").Create(&product).Error; err != nil {
			return err
		}
		return tx.Table("inventory.inventories").Create(&inventory).Error
	}); err != nil {
		t.Fatalf("create product fixture: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Exec(`DELETE FROM inventory.reservation_items
			WHERE reservation_id IN (SELECT id FROM inventory.reservations WHERE order_id = ?)`, orderID).Error
		_ = db.Table("inventory.reservations").Where("order_id = ?", orderID).Delete(&models.Reservation{}).Error
		_ = db.Table("inventory.inventories").Where("product_id = ?", productID).Delete(&models.Inventory{}).Error
		_ = db.Table("inventory.products").Where("id = ?", productID).Delete(&models.Product{}).Error
	})

	created, err := service.CreateReservation(context.Background(), CreateReservationInput{
		OrderID: orderID, RequestHash: uuid.New().String(),
		Items: []ReservationItemInput{{ProductID: productID, Quantity: 3}},
	})
	if err != nil {
		t.Fatalf("create reservation: %v", err)
	}
	oldCreatedAt := time.Now().UTC().Add(-20 * time.Minute)
	if err := db.Table("inventory.reservations").
		Where("id = ?", created.ReservationID).
		Update("created_at", oldCreatedAt).Error; err != nil {
		t.Fatalf("age reservation: %v", err)
	}

	listed, err := service.ListReservedReservations(context.Background(), time.Now().Add(-10*time.Minute), nil, 10)
	if err != nil {
		t.Fatalf("list old reservations: %v", err)
	}
	found := false
	for _, reservation := range listed {
		if reservation.ReservationID == created.ReservationID {
			found = true
		}
	}
	if !found {
		t.Fatalf("old reservation %s was not listed", created.ReservationID)
	}

	start := make(chan struct{})
	errors := make([]error, 2)
	results := make([]ChangeReservationResult, 2)
	var waitGroup sync.WaitGroup
	for index := range results {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			<-start
			results[index], errors[index] = service.ReleaseReservation(context.Background(), orderID)
		}(index)
	}
	close(start)
	waitGroup.Wait()
	for index := range results {
		if errors[index] != nil {
			t.Fatalf("release %d: %v", index, errors[index])
		}
		if results[index].Status != "RELEASED" {
			t.Fatalf("release %d status = %q", index, results[index].Status)
		}
	}
	replay, err := service.ReleaseReservation(context.Background(), orderID)
	if err != nil {
		t.Fatalf("release already-released reservation: %v", err)
	}
	if replay.Status != "RELEASED" {
		t.Fatalf("release replay status = %q, want RELEASED", replay.Status)
	}

	var stored models.Inventory
	if err := db.Table("inventory.inventories").Where("product_id = ?", productID).First(&stored).Error; err != nil {
		t.Fatalf("load inventory: %v", err)
	}
	if stored.OnHand != 10 || stored.Reserved != 0 {
		t.Fatalf("inventory = on_hand %d reserved %d, want 10/0", stored.OnHand, stored.Reserved)
	}
}
