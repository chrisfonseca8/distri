# Phase 05 — Confirm and release transitions

**What is tested:** Confirms one reservation and releases another twice, verifying idempotency and preventing contradictory terminal transitions.

**Test code:** [`tools/correctnesstesting/local/phase05_reservation_transitions.go`](../../../tools/correctnesstesting/local/phase05_reservation_transitions.go)

**Counted result:** passes in 4 of 4 recorded runs (success; decline, default payment window; decline, 3 s window (rerun); timeout).

## Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 0.18 | 8 | 18.85 |
| decline, default payment window | PASS | 0.14 | 8 | 15.567 |
| decline, 3 s window (rerun) | PASS | 0.16 | 8 | 18.883 |
| timeout | PASS | 0.19 | 8 | 16.661 |

## Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| confirm is idempotent | PASS | both calls return CONFIRMED |
| confirm moves stock once | PASS | on_hand decreases and reserved returns |
| confirmed reservation can be compensated | PASS | HTTP status=OK |
| confirmed compensation is idempotent | PASS | stock is restored exactly once |
| release is idempotent | PASS | both calls return RELEASED |
| release restores availability | PASS | all inventory counters return to starting values |

**Raw data:** [`test2-all-runs.json`](../data/cloud-suite/test2-all-runs.json)
