# Phase 18 — Multi-item deadlock resistance

**What is tested:** Submits concurrent two-item carts in opposite input order and verifies canonical locking prevents deadlocks and timeouts.

**Test code:** [`tools/correctnesstesting/local/phase18_deadlock.go`](../../../tools/correctnesstesting/local/phase18_deadlock.go)

**Counted result:** passes in 4 of 4 recorded runs (success; decline, default payment window; decline, 3 s window (rerun); timeout).

## Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 0.33 | 20 | 244.987 |
| decline, default payment window | PASS | 0.28 | 20 | 215.73 |
| decline, 3 s window (rerun) | PASS | 0.28 | 20 | 202.104 |
| timeout | PASS | 0.32 | 20 | 203.804 |

## Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| all opposite-order carts complete | PASS | no deadlocks, transport timeouts, or conflicts |
| both products reserved exactly | PASS | each product reserved 20 |

**Raw data:** [`test2-all-runs.json`](../data/cloud-suite/test2-all-runs.json)
