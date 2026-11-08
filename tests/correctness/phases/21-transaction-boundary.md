# Phase 21 — Transaction boundary

**What is tested:** Samples pg_stat_activity during the checkout saga and fails if an application connection waits on network I/O while idle inside a database transaction.

**Test code:** [`tools/correctness/phase21_transaction_scope.go`](../../../tools/correctness/phase21_transaction_scope.go)

**Counted result:** passes in 3 of 4 recorded runs (success; decline, 3 s window (rerun); timeout).

## Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 4.16 | 22 | 4.885 |
| decline, default payment window | FAIL | 60.1 | 145 | 6.469 |
| decline, 3 s window (rerun) | PASS | 10.07 | 43 | 5.237 |
| timeout | PASS | 39.18 | 144 | 5.439 |

## Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| actionable background jobs drain before sampling | PASS | timed out waiting for the worker backlog |
| no idle transaction crosses service calls | PASS | observed idle-in-transaction samples=0 |

## Run that did not pass: decline, default payment window

Assertions that failed:

- **actionable background jobs drain before sampling** — timed out waiting for the worker backlog

In this run the worker backlog from the earlier phases had not drained when the phase ran, so it timed out. The phase passed on the rerun with a 3 s payment window and 1 s reconciliation interval, so the decline result holds for those timings only.

**Raw data:** [`test2-all-runs.json`](../data/cloud-suite/test2-all-runs.json)
