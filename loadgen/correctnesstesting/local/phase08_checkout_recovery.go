package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"
)

// Phase 8 verifies both recoverable in_progress checkout windows: inventory was
// unavailable before reservation, and inventory committed before order creation.
func phase08() phaseDefinition {
	return phaseDefinition{8, "Checkout crash recovery", "Stops inventory to leave a claimed checkout in progress, then retries it; also fixtures an in-progress attempt with an existing reservation to model a crash after reserve.", func(p *phaseContext) {
		if !p.suite.canControlServices() {
			p.skip("Run the correctness binary from the host with COMPOSE_FILE set so it can stop and restart inventory-service deterministically.")
			return
		}

		// Window A: checkout claim committed, inventory was unavailable, no
		// reservation or order exists. A same-key retry must resume the attempt.
		product := p.productID(18)
		key := "phase08-before-reserve-" + newUUID()
		if err := p.suite.compose("stop", "inventory-service"); err != nil {
			p.fail("stop inventory for checkout outage", err.Error())
			return
		}
		inventoryStopped := true
		defer func() {
			if inventoryStopped {
				_ = p.suite.compose("start", "inventory-service")
			}
		}()
		status, _, _ := p.checkout(key, []map[string]any{item(product, 1)})
		p.check("inventory outage returns service unavailable", status == 503, statusDetail(status, nil))
		p.check("attempt remains in progress", p.text("SELECT status FROM orders.checkout_attempts WHERE idempotency_key=$1", key) == "in_progress", "retryable persisted claim")
		p.check("outage creates no order", p.scalar("SELECT count(*) FROM orders.orders o JOIN orders.checkout_attempts a ON a.order_id=o.id WHERE a.idempotency_key=$1", key) == 0, "no partial order")
		if err := p.suite.compose("start", "inventory-service"); err != nil {
			p.fail("restart inventory after checkout outage", err.Error())
			return
		}
		inventoryStopped = false
		if !waitForInventory(p, product) {
			p.fail("inventory becomes ready", "inventory did not respond after restart")
			return
		}
		status, resumed, err := p.checkout(key, []map[string]any{item(product, 1)})
		p.check("same-key retry completes checkout", err == nil && status == 201 && resumed.Status == "PENDING_PAYMENT", statusDetail(status, err))
		p.check("resumed checkout has one order and reservation", p.scalar("SELECT count(*) FROM orders.orders WHERE id=$1", resumed.OrderID) == 1 && p.scalar("SELECT count(*) FROM inventory.reservations WHERE order_id=$1", resumed.OrderID) == 1, "no duplicates")

		// Window B: model a crash after inventory committed but before the order
		// transaction. The retry replays the reservation and finishes the order.
		reservedProduct := p.productID(19)
		reservedBefore := p.inventory(reservedProduct)
		reservedKey, orderID := "phase08-after-reserve-"+newUUID(), newUUID()
		items := []map[string]any{item(reservedProduct, 2)}
		hash := checkoutHash(items)
		_, insertErr := p.suite.db.Exec(p.suite.ctx, `INSERT INTO orders.checkout_attempts
			(idempotency_key, request_hash, status, order_id, created_at, updated_at)
			VALUES ($1, $2, 'in_progress', $3, now(), now())`, reservedKey, hash, orderID)
		p.check("in-progress crash fixture is inserted", insertErr == nil, "attempt owns a stable order ID")
		reserveStatus, reservation, reserveErr := p.reserve(orderID, hash, items)
		p.check("inventory committed before simulated crash", reserveErr == nil && reserveStatus == 201 && reservation.Status == "RESERVED", statusDetail(reserveStatus, reserveErr))
		status, recovered, err := p.checkout(reservedKey, items)
		reservedAfter := p.inventory(reservedProduct)
		p.check("retry after reservation creates order", err == nil && status == 201 && recovered.OrderID == orderID, statusDetail(status, err))
		p.check("pre-existing reservation is reused", p.scalar("SELECT count(*) FROM inventory.reservations WHERE order_id=$1", orderID) == 1, "one reservation")
		p.check("stock was reserved once", reservedAfter.Reserved-reservedBefore.Reserved == 2, "reserved delta must equal 2")
	}}
}

func checkoutHash(items []map[string]any) string {
	canonical, err := json.Marshal(items)
	if err != nil {
		panic(err)
	}
	hash := sha256.Sum256(canonical)
	return hex.EncodeToString(hash[:])
}

func waitForInventory(p *phaseContext, productID string) bool {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		status, _, err := p.request(http.MethodGet, p.suite.inventoryURL+"/v1/products/"+productID, nil, nil, nil)
		if err == nil && status == 200 {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}
