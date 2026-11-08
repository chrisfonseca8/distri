# Phase 17 — Concurrent limited-stock checkout

**What is tested:** Runs 50 distinct checkouts against ten units and verifies exactly ten winners, no oversell, and no negative counters.

**Test code:** [`tools/correctness/phase17_limited_stock.go`](../../../tools/correctness/phase17_limited_stock.go)

**Counted result:** passes in 4 of 4 recorded runs (success; decline, default payment window; decline, 3 s window (rerun); timeout).

## Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 0.39 | 50 | 222.112 |
| decline, default payment window | PASS | 0.33 | 50 | 186.981 |
| decline, 3 s window (rerun) | PASS | 0.31 | 50 | 149.268 |
| timeout | PASS | 0.3 | 50 | 138.295 |

## Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| exactly available stock wins | PASS | expected 10 created and 40 conflicts |
| stock is not oversold | PASS | on_hand=10 reserved=10 allocated=0 |
| ten orders were created | PASS | one order per unit |

**Raw data:** [`test2-all-runs.json`](../data/cloud-suite/test2-all-runs.json)
