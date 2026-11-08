# Phase 15 — Ambiguous payment timeout

**What is tested:** Checks that an unknown gateway outcome keeps the order pending and its stock reserved for later reconciliation.

**Test code:** [`tools/correctnesstesting/local/phase15_timeout_e2e.go`](../../../tools/correctnesstesting/local/phase15_timeout_e2e.go)

**Counted result:** passes in 1 of 4 recorded runs (timeout).

## Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | SKIP | 0.0 | 0 | 0 |
| decline, default payment window | SKIP | 0.0 | 0 | 0 |
| decline, 3 s window (rerun) | SKIP | 0.0 | 0 | 0 |
| timeout | PASS | 8.21 | 79 | 5.505 |

## Assertions (recorded in the timeout run)

| Assertion | Result | Detail |
|---|---|---|
| timeout keeps order pending | PASS | HTTP status=OK |
| payment is unknown | PASS | ambiguous terminal payment |
| reservation remains reserved | PASS | do not release uncertain stock |
| stock remains held | PASS | reserved +2 |

## Skipped in other runs

- decline, 3 s window (rerun): Run a fresh disposable stack with GATEWAY_MODE=timeout to execute this phase.
- decline, default payment window: Run a fresh disposable stack with GATEWAY_MODE=timeout to execute this phase.
- success: Run a fresh disposable stack with GATEWAY_MODE=timeout to execute this phase.

**Raw data:** [`test2-all-runs.json`](../data/cloud-suite/test2-all-runs.json)
