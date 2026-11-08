# Phase 14 — Declined payment end to end

**What is tested:** Checks that a declined payment cancels the order and releases, rather than consumes, reserved inventory.

**Test code:** [`tools/correctnesstesting/local/phase14_decline_e2e.go`](../../../tools/correctnesstesting/local/phase14_decline_e2e.go)

**Counted result:** passes in 1 of 4 recorded runs (decline, 3 s window (rerun)).

## Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | SKIP | 0.0 | 0 | 0 |
| decline, default payment window | FAIL | 60.18 | 289 | 5.498 |
| decline, 3 s window (rerun) | PASS | 4.64 | 41 | 4.883 |
| timeout | SKIP | 0.0 | 0 | 0 |

## Assertions (recorded in the decline, 3 s window (rerun) run)

| Assertion | Result | Detail |
|---|---|---|
| saga finishes its inventory step | PASS | saga status COMPENSATED |
| decline cancels order | PASS | HTTP status=OK |
| payment is failed | PASS | terminal payment |
| reservation is released | PASS | terminal reservation |
| decline consumes no stock | PASS | inventory returns to start |

## Run that did not pass: decline, default payment window

Assertions that failed:

- **saga finishes its inventory step** — saga status COMPENSATED
- **decline cancels order** — HTTP status=OK
- **payment is failed** — terminal payment
- **reservation is released** — terminal reservation
- **decline consumes no stock** — inventory returns to start

In this run the payment stayed in `AWAITING_PAYMENT` because the default payment window had not elapsed, so the decline outcome did not become visible in time. The phase passed on the rerun with a 3 s payment window and 1 s reconciliation interval, so the decline result holds for those timings only.

## Skipped in other runs

- success: Run a fresh disposable stack with GATEWAY_MODE=decline to execute this phase.
- timeout: Run a fresh disposable stack with GATEWAY_MODE=decline to execute this phase.

**Raw data:** [`test2-all-runs.json`](../data/cloud-suite/test2-all-runs.json)
