# Phase 07 — Checkout idempotency and concurrency

**What is tested:** Checks sequential response replay, canonical item ordering, key/body conflicts, and concurrent retries of one logical checkout.

**Test code:** [`tools/correctnesstesting/local/phase07_checkout_idempotency.go`](../../../tools/correctnesstesting/local/phase07_checkout_idempotency.go)

**Counted result:** passes in 4 of 4 recorded runs (success; decline, default payment window; decline, 3 s window (rerun); timeout).

## Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 0.23 | 17 | 81.262 |
| decline, default payment window | PASS | 0.2 | 17 | 50.95 |
| decline, 3 s window (rerun) | PASS | 0.21 | 17 | 50.893 |
| timeout | PASS | 0.17 | 17 | 51.465 |

## Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| sequential retry replays the stored response | PASS | one stable order ID |
| key reuse with different body is rejected | PASS | HTTP status=Unprocessable Entity |
| item order is canonicalized | PASS | reordered cart is same request |
| concurrent retries share one saga | PASS | every retry returns 202 with the same order ID |
| concurrent retries reserve once | PASS | reserved delta must equal 1 |

**Raw data:** [`test2-all-runs.json`](../data/cloud-suite/test2-all-runs.json)
