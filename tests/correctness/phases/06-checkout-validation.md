# Phase 06 — Checkout validation

**What is tested:** Validates a normal checkout and rejects missing idempotency keys, invalid quantities, duplicates, unknown products, and insufficient stock.

**Test code:** [`tools/correctnesstesting/local/phase06_checkout_validation.go`](../../../tools/correctnesstesting/local/phase06_checkout_validation.go)

**Counted result:** passes in 4 of 4 recorded runs (success; decline, default payment window; decline, 3 s window (rerun); timeout).

## Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 0.15 | 8 | 49.032 |
| decline, default payment window | PASS | 0.1 | 8 | 40.137 |
| decline, 3 s window (rerun) | PASS | 0.12 | 8 | 39.231 |
| timeout | PASS | 0.09 | 8 | 22.561 |

## Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| valid checkout is accepted by the saga | PASS | HTTP status=Accepted |
| missing idempotency key is rejected | PASS | HTTP status=Bad Request |
| empty cart mapping | PASS | HTTP status=Bad Request |
| zero quantity mapping | PASS | HTTP status=Bad Request |
| negative quantity mapping | PASS | HTTP status=Bad Request |
| duplicate product mapping | PASS | HTTP status=Bad Request |
| unknown product mapping | PASS | HTTP status=Not Found |
| insufficient stock mapping | PASS | HTTP status=Conflict |

**Raw data:** [`test2-all-runs.json`](../data/cloud-suite/test2-all-runs.json)
