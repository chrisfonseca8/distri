# Phase 13 — Successful checkout end to end

**What is tested:** Runs the checkout saga through payment and inventory confirmation, then cross-checks order, payment, reservation, and stock records.

**Test code:** [`tools/correctnesstesting/local/phase13_success_e2e.go`](../../../tools/correctnesstesting/local/phase13_success_e2e.go)

**Counted result:** passes in 1 of 4 recorded runs (success).

## Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 1.59 | 12 | 5.656 |
| decline, default payment window | SKIP | 0.0 | 0 | 0 |
| decline, 3 s window (rerun) | SKIP | 0.0 | 0 | 0 |
| timeout | SKIP | 0.0 | 0 | 0 |

## Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| saga finishes its inventory step | PASS | saga status COMPLETED |
| order becomes paid | PASS | HTTP status=OK |
| one successful payment exists | PASS | payment must succeed |
| reservation is confirmed | PASS | reservation terminal state |
| confirmed stock is accounted once | PASS | on_hand -2, reserved restored |

## Skipped in other runs

- decline, 3 s window (rerun): Run with GATEWAY_MODE=success to exercise the deterministic success path.
- decline, default payment window: Run with GATEWAY_MODE=success to exercise the deterministic success path.
- timeout: Run with GATEWAY_MODE=success to exercise the deterministic success path.

**Raw data:** [`test2-all-runs.json`](../data/cloud-suite/test2-all-runs.json)
