# Phase 10 — Payment idempotency and concurrency

**What is tested:** Sends concurrent create-payment requests for one order and verifies one database row, one payment ID, and one asynchronous result.

**Test code:** [`tools/correctnesstesting/local/phase10_payment_idempotency.go`](../../../tools/correctnesstesting/local/phase10_payment_idempotency.go)

**Counted result:** passes in 4 of 4 recorded runs (success; decline, default payment window; decline, 3 s window (rerun); timeout).

## Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 1.42 | 43 | 18.483 |
| decline, default payment window | PASS | 7.07 | 150 | 14.426 |
| decline, 3 s window (rerun) | PASS | 3.97 | 91 | 14.586 |
| timeout | PASS | 3.27 | 78 | 16.784 |

## Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| one request creates and retries reuse | PASS | expected one 201, remaining 200, one ID |
| one payment row exists | PASS | database uniqueness |
| payment leaves CREATED/PENDING | PASS | SUCCEEDED |

**Raw data:** [`test2-all-runs.json`](../data/cloud-suite/test2-all-runs.json)
