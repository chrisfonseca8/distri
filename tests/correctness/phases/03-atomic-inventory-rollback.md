# Phase 03 — Atomic inventory rollback

**What is tested:** Attempts a two-product reservation whose second line lacks stock and verifies no stock or reservation row is partially committed.

**Test code:** [`tools/correctnesstesting/local/phase03_inventory_rollback.go`](../../../tools/correctnesstesting/local/phase03_inventory_rollback.go)

**Counted result:** passes in 4 of 4 recorded runs (success; decline, default payment window; decline, 3 s window (rerun); timeout).

## Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 0.19 | 1 | 12.161 |
| decline, default payment window | PASS | 0.11 | 1 | 11.424 |
| decline, 3 s window (rerun) | PASS | 0.16 | 1 | 10.622 |
| timeout | PASS | 0.17 | 1 | 11.525 |

## Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| insufficient stock returns conflict | PASS | HTTP status=Conflict |
| first stock update rolled back | PASS | first product inventory must be unchanged |
| second stock is unchanged | PASS | second product inventory must be unchanged |
| reservation claim rolled back | PASS | no partial reservation |

**Raw data:** [`test2-all-runs.json`](../data/cloud-suite/test2-all-runs.json)
