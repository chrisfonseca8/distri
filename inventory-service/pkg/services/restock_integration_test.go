package services

import (
	"context"
	"testing"
	"uuid"

	"github.com/chrisfonseca08/distri/inventory-service/database/models"
	"gorm.io/gorm"
)

func stockRow(t *testing.T, db *gorm.DB, productID uuid.UUID) models.Inventory {
	t.Helper()
	var inv models.Inventory
	if err := db.Table("inventory.inventories").Where("product_id = ?", productID).First(&inv).Error; err != nil {
		t.Fatalf("load stock: %v", err)
	}
	return inv
}

// A checkout that was paid, then refunded and cancelled, gives its units back.
func TestReleasingAConfirmedReservationRestocksTheUnitsOnce(t *testing.T) {
	db := openSweeperTestDB(t)
	service := NewReservationService(db)
	productID, orderID := uuid.New(), uuid.New()
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("inventory.products").Create(&models.Product{ID: productID, Name: "restock test", PricePaise: 100, Currency: "INR"}).Error; err != nil {
			return err
		}
		return tx.Table("inventory.inventories").Create(&models.Inventory{ProductID: productID, OnHand: 10}).Error
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM inventory.reservation_items WHERE reservation_id IN (SELECT id FROM inventory.reservations WHERE order_id = ?)`, orderID.String())
		db.Exec(`DELETE FROM inventory.reservations WHERE order_id = ?`, orderID.String())
		db.Exec(`DELETE FROM inventory.inventories WHERE product_id = ?`, productID.String())
		db.Exec(`DELETE FROM inventory.products WHERE id = ?`, productID.String())
	})

	if _, err := service.CreateReservation(context.Background(), CreateReservationInput{
		OrderID: orderID, RequestHash: "h", Items: []ReservationItemInput{{ProductID: productID, Quantity: 3}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ConfirmReservation(context.Background(), orderID); err != nil {
		t.Fatal(err)
	}
	if inv := stockRow(t, db, productID); inv.OnHand != 7 || inv.Reserved != 0 {
		t.Fatalf("after confirm on_hand=%d reserved=%d, want 7 and 0", inv.OnHand, inv.Reserved)
	}

	result, err := service.ReleaseReservation(context.Background(), orderID)
	if err != nil || result.Status != "RELEASED" {
		t.Fatalf("release of a confirmed reservation = %+v, %v; want RELEASED", result, err)
	}
	if inv := stockRow(t, db, productID); inv.OnHand != 10 || inv.Reserved != 0 {
		t.Fatalf("after the release on_hand=%d reserved=%d, want the 3 units back: 10 and 0", inv.OnHand, inv.Reserved)
	}
	// a repeat must not restock twice
	if _, err := service.ReleaseReservation(context.Background(), orderID); err != nil {
		t.Fatal(err)
	}
	if inv := stockRow(t, db, productID); inv.OnHand != 10 {
		t.Fatalf("on_hand = %d after a repeated release, want still 10", inv.OnHand)
	}
	// and a released reservation cannot be confirmed any more
	if _, err := service.ConfirmReservation(context.Background(), orderID); err == nil {
		t.Fatal("confirming a released reservation must still be refused")
	}
}
