package main

import (
	"sync"
	"time"
)

// Phase 21 samples PostgreSQL while external payment I/O is in progress.
func phase21() phaseDefinition {
	return phaseDefinition{21, "Transaction boundary", "Samples pg_stat_activity during the checkout saga and fails if an application connection waits on network I/O while idle inside a database transaction.", func(p *phaseContext) {
		drained := p.waitForActionableJobs(10 * time.Second)
		p.check("actionable background jobs drain before sampling", drained, "timed out waiting for the worker backlog")
		_, checkout, _ := p.checkout("phase21-"+newUUID(), []map[string]any{item(p.productID(25), 1)})
		var wg sync.WaitGroup
		wg.Add(1)
		go func() { defer wg.Done(); p.awaitOrder(checkout.OrderID, 15*time.Second) }()
		idleSamples := int64(0)
		for i := 0; i < 120; i++ {
			idleSamples += p.scalar(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND state='idle in transaction' AND pid <> pg_backend_pid()`)
			time.Sleep(5 * time.Millisecond)
		}
		wg.Wait()
		p.check("no idle transaction crosses service calls", idleSamples == 0, "observed idle-in-transaction samples="+fmtInt(idleSamples))
	}}
}

func fmtInt(value int64) string {
	if value == 0 {
		return "0"
	}
	result := ""
	for value > 0 {
		result = string(rune('0'+value%10)) + result
		value /= 10
	}
	return result
}
