# Phase 04 — Inventory idempotency

**What is tested:** Repeats the same order/hash, then changes the hash, checking response reuse, conflict detection, and a single stock mutation.

**Test code:** [`tools/correctnesstesting/local/phase04_inventory_idempotency.go`](../../../tools/correctnesstesting/local/phase04_inventory_idempotency.go)

**Counted result:** passes in 4 of 4 recorded runs (success; decline, default payment window; decline, 3 s window (rerun); timeout).

## Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 0.11 | 4 | 36.153 |
| decline, default payment window | PASS | 0.06 | 4 | 18.017 |
| decline, 3 s window (rerun) | PASS | 0.08 | 4 | 17.012 |
| timeout | PASS | 0.08 | 4 | 14.849 |

## Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| first request creates reservation | PASS | HTTP status=Created |
| same request reuses reservation | PASS | HTTP status=OK |
| different hash conflicts | PASS | HTTP status=Conflict |
| stock reserved once | PASS | reserved delta must equal 2 |

**Raw data:** [`test2-all-runs.json`](../data/cloud-suite/test2-all-runs.json)
