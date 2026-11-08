# Phase 19 — Terminal order state idempotency

**What is tested:** Replays the checkout of a PAID order and verifies the same order ID, stable state, one payment, and one inventory movement.

**Test code:** [`tools/correctnesstesting/local/phase19_terminal_states.go`](../../../tools/correctnesstesting/local/phase19_terminal_states.go)

**Counted result:** passes in 1 of 4 recorded runs (success).

## Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 4.74 | 27 | 8.484 |
| decline, default payment window | SKIP | 0.0 | 0 | 0 |
| decline, 3 s window (rerun) | SKIP | 0.0 | 0 | 0 |
| timeout | SKIP | 0.0 | 0 | 0 |

## Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| order reaches paid | PASS | HTTP status=OK |
| replay returns the same order | PASS | stable order ID |
| replay leaves the order paid | PASS | stable terminal state |
| replay creates no payment | PASS | one payment attempt |
| replay moves stock once | PASS | single-unit movement |

## Skipped in other runs

- decline, 3 s window (rerun): The default terminal-state assertion is deterministic under GATEWAY_MODE=success; decline is covered by phase 14.
- decline, default payment window: The default terminal-state assertion is deterministic under GATEWAY_MODE=success; decline is covered by phase 14.
- timeout: The default terminal-state assertion is deterministic under GATEWAY_MODE=success; decline is covered by phase 14.

**Raw data:** [`test2-all-runs.json`](../data/cloud-suite/test2-all-runs.json)
