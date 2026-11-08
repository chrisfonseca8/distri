# Phase 09 — Payment gateway outcome

**What is tested:** Creates a payment, observes asynchronous processing, and checks the terminal status for the configured simulator mode.

**Test code:** [`tools/correctness/phase09_payment_outcome.go`](../../../tools/correctness/phase09_payment_outcome.go)

**Counted result:** passes in 3 of 4 recorded runs (success; decline, 3 s window (rerun); timeout).

## Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 1.07 | 22 | 4.649 |
| decline, default payment window | FAIL | 7.05 | 136 | 3.053 |
| decline, 3 s window (rerun) | PASS | 3.72 | 72 | 3.736 |
| timeout | PASS | 3.05 | 59 | 4.694 |

## Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| payment attempt is created | PASS | HTTP status=Created |
| gateway result matches mode | PASS | expected SUCCEEDED, got SUCCEEDED |

## Run that did not pass: decline, default payment window

Assertions that failed:

- **gateway result matches mode** — expected FAILED, got AWAITING_PAYMENT

In this run the payment stayed in `AWAITING_PAYMENT` because the default payment window had not elapsed, so the decline outcome did not become visible in time. The phase passed on the rerun with a 3 s payment window and 1 s reconciliation interval, so the decline result holds for those timings only.

**Raw data:** [`test2-all-runs.json`](../data/cloud-suite/test2-all-runs.json)
