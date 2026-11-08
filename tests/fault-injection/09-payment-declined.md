# Failure case 09 — Payment declined by the gateway

**Result:** PASS in the decline-mode rerun (3 s payment window, 1 s reconciliation). The first decline-mode run, with the default payment window, did not pass (see below).

## What is tested

The gateway simulator is set to `decline`: the customer's payment fails. A confirmed failure must be treated differently from an ambiguous one: the saga compensates (the order is cancelled, the reserved stock is released, and no stock is consumed), and the payment is recorded as failed.

## How the fault is injected

`GATEWAY_MODE=decline` on the gateway simulator, running the correctness suite phases 09 and 14 against the AWS staging stack.

## Phase 14 — Declined payment end to end

**Test code:** [`phase14_decline_e2e.go`](../../tools/correctness/phase14_decline_e2e.go)


| Assertion | Result | Detail |
|---|---|---|
| saga finishes its inventory step | PASS | saga status COMPENSATED |
| decline cancels order | PASS | HTTP status=OK |
| payment is failed | PASS | terminal payment |
| reservation is released | PASS | terminal reservation |
| decline consumes no stock | PASS | inventory returns to start |

## Phase 09 — Payment gateway outcome (decline mode)

**Test code:** [`phase09_payment_outcome.go`](../../tools/correctness/phase09_payment_outcome.go)


| Assertion | Result | Detail |
|---|---|---|
| payment attempt is created | PASS | HTTP status=Created |
| gateway result matches mode | PASS | expected FAILED, got FAILED |

## Caveat: payment window

With the default payment window, a `payment.failed` webhook does not cancel the order immediately, because the customer may retry inside the window. In the first decline-mode run the payments therefore stayed in `AWAITING_PAYMENT`, phases 09 and 14 failed, and the worker backlog did not drain in time for phases 21 and 22. The rerun used a 3 s window and 1 s reconciliation interval (the payment timings were made overridable for this) and passed. The decline result therefore holds for those short timings; the default window was not tested in decline mode.

**Raw data:** [`test2-all-runs.json`](../correctness/data/cloud-suite/test2-all-runs.json)
