package main

import (
	"fmt"
	"time"
)

func (p *phaseContext) scalar(query string, args ...any) int64 {
	var value int64
	if err := p.suite.db.QueryRow(p.suite.ctx, query, args...).Scan(&value); err != nil {
		panic(fmt.Errorf("database scalar query: %w", err))
	}
	return value
}

func (p *phaseContext) text(query string, args ...any) string {
	var value string
	if err := p.suite.db.QueryRow(p.suite.ctx, query, args...).Scan(&value); err != nil {
		panic(fmt.Errorf("database text query: %w", err))
	}
	return value
}

func (p *phaseContext) productID(offset int) string {
	var id string
	err := p.suite.db.QueryRow(p.suite.ctx,
		"SELECT id::text FROM inventory.products ORDER BY created_at, id OFFSET $1 LIMIT 1", offset).Scan(&id)
	if err != nil {
		panic(fmt.Errorf("load seeded product %d: %w", offset, err))
	}
	return id
}

func (p *phaseContext) inventory(productID string) inventorySnapshot {
	var value inventorySnapshot
	err := p.suite.db.QueryRow(p.suite.ctx,
		"SELECT on_hand, reserved, allocated FROM inventory.inventories WHERE product_id=$1", productID).
		Scan(&value.OnHand, &value.Reserved, &value.Allocated)
	if err != nil {
		panic(fmt.Errorf("load inventory: %w", err))
	}
	return value
}

func (p *phaseContext) setStock(productID string, onHand int64) {
	_, err := p.suite.db.Exec(p.suite.ctx,
		"UPDATE inventory.inventories SET on_hand=$2, reserved=0, allocated=0, updated_at=now() WHERE product_id=$1",
		productID, onHand)
	if err != nil {
		panic(fmt.Errorf("set stock fixture: %w", err))
	}
}

// waitForActionableJobs waits until no payment job, outbox row or saga (other
// than one parked on an ambiguous payment) still has work to do.
func (p *phaseContext) waitForActionableJobs(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		remaining := p.scalar(`SELECT
			(SELECT count(*) FROM payments.jobs WHERE status IN ('queued','running'))
			+ (SELECT count(*) FROM orchestrator.outbox WHERE published_at IS NULL)
			+ (SELECT count(*)
				FROM orchestrator.sagas s
				LEFT JOIN payments.payment_attempts payment ON payment.order_id=s.id
				WHERE s.status IN ('RUNNING','COMPENSATING')
					AND COALESCE(payment.status, '') <> 'UNKNOWN')`)
		if remaining == 0 {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func item(productID string, quantity int64) map[string]any {
	return map[string]any{"product_id": productID, "quantity": quantity}
}

// waitStepAttempts waits until the saga has sent a step's command at least
// atLeast times, which proves the sweeper resent it.
func (p *phaseContext) waitStepAttempts(orderID, step string, atLeast int64, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		attempts := p.scalar(`SELECT COALESCE(max(attempts), 0) FROM orchestrator.saga_steps WHERE saga_id=$1 AND step=$2`, orderID, step)
		if attempts >= atLeast {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(500 * time.Millisecond)
	}
}
