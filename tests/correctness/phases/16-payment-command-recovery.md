# Phase 16 — Payment command recovery

**What is tested:** Stops payment-service, starts a checkout, waits for the saga sweeper to resend payment.initiate, restarts payment-service, and verifies one payment and a completed saga.

**Test code:** [`tools/correctness/phase16_settlement_recovery.go`](../../../tools/correctness/phase16_settlement_recovery.go)

**Counted result:** passes in 1 of 4 recorded runs (success).

## Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 35.96 | 24 | 9.483 |
| decline, default payment window | SKIP | 0.0 | 0 | 0 |
| decline, 3 s window (rerun) | SKIP | 0.0 | 0 | 0 |
| timeout | SKIP | 0.0 | 0 | 0 |

## Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| checkout is accepted while payment-service is down | PASS | HTTP status=Accepted |
| sweeper resends payment.initiate | PASS | TAKE_PAYMENT attempts must reach 2 |
| order stays pending while payment is unavailable | PASS | never paid without a confirmed payment |
| stock is held but not consumed | PASS | reserved +2; on_hand unchanged |
| saga completes after recovery | PASS | HTTP status=OK |
| resends create one successful payment | PASS | exactly one payment attempt |
| reservation becomes confirmed | PASS | settlement repaired |
| stock moves exactly once | PASS | on_hand -2; reserved restored |

## Skipped in other runs

- decline, 3 s window (rerun): Run with GATEWAY_MODE=success so the recovered saga reaches PAID.
- decline, default payment window: Run with GATEWAY_MODE=success so the recovered saga reaches PAID.
- timeout: Run with GATEWAY_MODE=success so the recovered saga reaches PAID.

**Raw data:** [`test2-all-runs.json`](../data/cloud-suite/test2-all-runs.json)
