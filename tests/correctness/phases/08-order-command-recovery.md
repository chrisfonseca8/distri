# Phase 08 — Order command recovery

**What is tested:** Stops order-service, starts a checkout, waits for the saga sweeper to resend order.create, restarts order-service, and verifies one order is created and the saga completes.

**Test code:** [`tools/correctnesstesting/local/phase08_checkout_recovery.go`](../../../tools/correctnesstesting/local/phase08_checkout_recovery.go)

**Counted result:** passes in 1 of 4 recorded runs (success).

## Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 34.26 | 18 | 9.648 |
| decline, default payment window | SKIP | 0.0 | 0 | 0 |
| decline, 3 s window (rerun) | SKIP | 0.0 | 0 | 0 |
| timeout | SKIP | 0.0 | 0 | 0 |

## Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| checkout is accepted while order-service is down | PASS | HTTP status=Accepted |
| sweeper resends order.create | PASS | CREATE_ORDER attempts must reach 2 |
| no order exists while the participant is down | PASS | order creation is pending |
| saga completes after recovery | PASS | HTTP status=OK |
| resends create exactly one order | PASS | one order row |
| saga is completed | PASS | COMPLETED saga |
| stock moves exactly once | PASS | on_hand -1; reserved restored |

## Skipped in other runs

- decline, 3 s window (rerun): Run with GATEWAY_MODE=success so the recovered saga reaches PAID.
- decline, default payment window: Run with GATEWAY_MODE=success so the recovered saga reaches PAID.
- timeout: Run with GATEWAY_MODE=success so the recovered saga reaches PAID.

**Raw data:** [`test2-all-runs.json`](../data/cloud-suite/test2-all-runs.json)
