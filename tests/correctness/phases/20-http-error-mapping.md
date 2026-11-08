# Phase 20 — HTTP error mapping

**What is tested:** Sends malformed IDs, missing resources, invalid payment data, and checkout conflicts to verify stable client-facing status codes.

**Test code:** [`tools/correctnesstesting/local/phase20_http_mapping.go`](../../../tools/correctnesstesting/local/phase20_http_mapping.go)

**Counted result:** passes in 4 of 4 recorded runs (success; decline, default payment window; decline, 3 s window (rerun); timeout).

## Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 0.06 | 8 | 37.642 |
| decline, default payment window | PASS | 0.05 | 8 | 35.021 |
| decline, 3 s window (rerun) | PASS | 0.06 | 8 | 34.681 |
| timeout | PASS | 0.05 | 8 | 29.986 |

## Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| bad order ID | PASS | HTTP status=Bad Request |
| missing order | PASS | HTTP status=Not Found |
| bad payment ID | PASS | HTTP status=Bad Request |
| missing payment | PASS | HTTP status=Not Found |
| invalid payment amount | PASS | HTTP status=Bad Request |
| invalid currency length | PASS | HTTP status=Bad Request |
| idempotency body conflict | PASS | HTTP status=Unprocessable Entity |

**Raw data:** [`test2-all-runs.json`](../data/cloud-suite/test2-all-runs.json)
