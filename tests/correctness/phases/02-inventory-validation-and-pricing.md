# Phase 02 — Inventory validation and pricing

**What is tested:** Exercises valid reservation pricing plus empty, zero, negative, excessive, duplicate, and unknown-product requests.

**Test code:** [`tools/correctness/phase02_inventory_validation.go`](../../../tools/correctness/phase02_inventory_validation.go)

**Counted result:** passes in 4 of 4 recorded runs (success; decline, default payment window; decline, 3 s window (rerun); timeout).

## Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 0.07 | 8 | 21.675 |
| decline, default payment window | PASS | 0.06 | 8 | 19.902 |
| decline, 3 s window (rerun) | PASS | 0.06 | 8 | 17.08 |
| timeout | PASS | 0.06 | 8 | 17.295 |

## Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| valid reservation is created | PASS | HTTP status=Created |
| price and total come from inventory | PASS | client cannot choose price |
| empty cart is rejected | PASS | HTTP status=Bad Request |
| zero quantity is rejected | PASS | HTTP status=Bad Request |
| negative quantity is rejected | PASS | HTTP status=Bad Request |
| quantity over 100 is rejected | PASS | HTTP status=Bad Request |
| duplicate product is rejected | PASS | HTTP status=Bad Request |
| unknown product is rejected | PASS | HTTP status=Not Found |

**Raw data:** [`test2-all-runs.json`](../data/cloud-suite/test2-all-runs.json)
