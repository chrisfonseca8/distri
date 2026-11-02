# Test 2 — correctness suite on the cloud stack (cumulative result)

**Date:** 1 Nov 2026 · **Environment:** `staging` (AWS, services on separate hosts) · **Raw data:** [`test2-all-runs.json`](test2-all-runs.json) (all four runs, every phase and assertion, audits, dead-queue snapshots)

## Verdict

**PASS, with caveats.** Every one of the 20 active phases passed at least once. The database audit was clean after all four runs. It was **not** one clean run: the first decline attempt failed on timing (see Problems, item 1) and was rerun with a short payment window, and no single run covers every phase (some phases only apply to one gateway mode).

The suite has 20 active phases. Phases 11 and 12 were retired when `/pay` was removed.

## The four runs

| # | Mode | Result | Phases (pass / fail / skip) | Time (UTC) | Counts? |
|---|---|---|---|---|---|
| 1 | success | PASS | 18 / 0 / 2 | 15:03–15:04 | yes (an earlier attempt is not in the repo) |
| 2 | decline | **FAIL** | 11 / 4 / 5 | 15:13–15:16 | no, default payment window (see Problems 1) |
| 3 | decline (retry) | PASS | 15 / 0 / 5 | 15:21–15:21 | yes, 3 s window, 1 s reconciliation |
| 4 | timeout | PASS | 15 / 0 / 5 | 15:30–15:31 | yes |

Skips are by design: a phase that does not apply to the gateway mode reports SKIP with a note, not PASS.

## Phase by phase

Status per run. Request counts are in the JSON.

| Phase | Test | success | decline (failed) | decline (retry) | timeout |
|---:|---|:---:|:---:|:---:|:---:|
| 01 | Environment and migrations | PASS | PASS | PASS | PASS |
| 02 | Inventory validation and pricing | PASS | PASS | PASS | PASS |
| 03 | Atomic inventory rollback | PASS | PASS | PASS | PASS |
| 04 | Inventory idempotency | PASS | PASS | PASS | PASS |
| 05 | Confirm and release transitions | PASS | PASS | PASS | PASS |
| 06 | Checkout validation | PASS | PASS | PASS | PASS |
| 07 | Checkout idempotency and concurrency | PASS | PASS | PASS | PASS |
| 08 | Order command recovery | PASS | skip | skip | skip |
| 09 | Payment gateway outcome | PASS | **FAIL** | PASS | PASS |
| 10 | Payment idempotency and concurrency | PASS | PASS | PASS | PASS |
| 13 | Successful checkout end to end | PASS | skip | skip | skip |
| 14 | Declined payment end to end | skip | **FAIL** | PASS | skip |
| 15 | Ambiguous payment timeout | skip | skip | skip | PASS |
| 16 | Payment command recovery | PASS | skip | skip | skip |
| 17 | Concurrent limited stock (50 buyers, 10 units) | PASS | PASS | PASS | PASS |
| 18 | Multi-item deadlock resistance | PASS | PASS | PASS | PASS |
| 19 | Terminal order idempotency | PASS | skip | skip | skip |
| 20 | HTTP error mapping | PASS | PASS | PASS | PASS |
| 21 | Transaction boundary | PASS | **FAIL** | PASS | PASS |
| 22 | Database invariants | PASS | **FAIL** | PASS | PASS |

Which mode proves which phase:
- **Success only:** 08, 13, 16, 19.
- **Decline only:** 14.
- **Timeout only:** 15.
- **All three modes:** every other phase.

## Speed (p99 HTTP latency per request, from the passing runs)

Concurrency phases are the slowest, as expected: phase 17 took 139–226 ms and phase 18 took 209–257 ms. Every other phase stayed under 100 ms. This suite checks correctness, not capacity, so these numbers are not a throughput result.

## Database audit after each run

All four runs got an audit verdict of PASS. Counts of `saga_stuck`, `outbox_publish_failed`, `outbox_message_marked_dead`, `payment_unconfirmed`, `late_capture_refund_required` and `stock_invariant_failed` were all 0. In the three counted runs every phase-22 invariant passed: stock within bounds, one reservation and one payment per order, PAID orders have a successful payment, cancelled orders have failed payments and released stock, amounts match in paise.

## Problems to know about

1. **The first decline run failed because of the test setup, not a defect** (cause supplied by the owner; it is not visible in the run's own files, whose logs were empty).
   - In the custom gateway, a `payment.failed` webhook does not cancel the order at once, because the customer may retry inside the payment window. With the default window, payments stayed in AWAITING_PAYMENT, so phases 09 and 14 failed, and the worker backlog could not drain, so phases 21 and 22 timed out.
   - The retry passed because the Payment timing settings were made overrideable and the run used a **3-second payment window with 1-second reconciliation**, still on the same custom simulator.
   - Consequence: the decline result holds for those short timings. The default window was not tested in decline mode. The timeout and success runs' timings are not recorded.
2. **The dead queue grew.** The plan expects it unchanged, but each of the decline and timeout runs left 2 new rejected `reply.payment` messages in `distri.commands.dead`; the earlier 4 were cleared before the decline runs. I suspect phase 10, which creates payments without a saga. That is unverified.
3. **Alerts fired in every run:** `DatabasePoolSaturated`, `DeadLetterQueueGrowing`, `DeadLettersPublished`. A `ServiceDown` alert also fired in the success and decline-retry runs, so a service was restarted between attempts. The pool saturation is undiagnosed.
4. **Run records are missing.** Git commit, image tag, instance types and Neon branch were not recorded for these runs, and neither were the payment window and reconciliation settings for the success and timeout runs. Record them next time, because they decide the outcome.

## Not covered by this test

Fault injection under load, the 500-buyer oversell run, and everything after test 2 in `final/README.md`.
