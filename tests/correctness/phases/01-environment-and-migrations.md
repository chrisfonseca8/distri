# Phase 01 — Environment and migrations

**What is tested:** Checks service reachability, database schemas, inventory seed count, and the required empty payment table.

**Test code:** [`tools/correctnesstesting/local/phase01_environment.go`](../../../tools/correctnesstesting/local/phase01_environment.go)

**Counted result:** passes in 4 of 4 recorded runs (success; decline, default payment window; decline, 3 s window (rerun); timeout).

## Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 0.05 | 4 | 5.515 |
| decline, default payment window | PASS | 0.03 | 4 | 5.231 |
| decline, 3 s window (rerun) | PASS | 0.07 | 4 | 5.646 |
| timeout | PASS | 0.04 | 4 | 3.669 |

## Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| order service health | PASS | HTTP status=OK |
| orchestrator service health | PASS | HTTP status=OK |
| payment service health | PASS | HTTP status=OK |
| inventory service and seed | PASS | HTTP status=OK |
| 50 inventory products were seeded | PASS | expected exactly 50 |
| payments start empty | PASS | payment seeding must remain disabled |

**Raw data:** [`test2-all-runs.json`](../data/cloud-suite/test2-all-runs.json)
