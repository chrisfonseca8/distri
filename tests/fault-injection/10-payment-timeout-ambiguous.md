# Failure case 10 — Ambiguous payment (gateway timeout)

**Result:** PASS

## What is tested

The gateway simulator is set to `timeout`: a payment request neither succeeds nor fails visibly. This is an *ambiguous* outcome, not a failure. The system must record the payment as `UNKNOWN`, leave the order and reservation pending/held, and never blindly retry a possibly-successful charge. A later lookup or webhook decides the outcome; after the payment window plus a grace period an unresolved attempt is failed as unconfirmed, and a capture seen later is recorded for refund rather than accepted.

## How the fault is injected

`GATEWAY_MODE=timeout` on the gateway simulator, running the correctness suite phases 09 and 15 against the AWS staging stack.

## Phase 15 — Ambiguous payment timeout

**Test code:** [`phase15_timeout_e2e.go`](../../tools/correctness/phase15_timeout_e2e.go)


| Assertion | Result | Detail |
|---|---|---|
| timeout keeps order pending | PASS | HTTP status=OK |
| payment is unknown | PASS | ambiguous terminal payment |
| reservation remains reserved | PASS | do not release uncertain stock |
| stock remains held | PASS | reserved +2 |

## Phase 09 — Payment gateway outcome (timeout mode)

**Test code:** [`phase09_payment_outcome.go`](../../tools/correctness/phase09_payment_outcome.go)


| Assertion | Result | Detail |
|---|---|---|
| payment attempt is created | PASS | HTTP status=Created |
| gateway result matches mode | PASS | expected UNKNOWN, got UNKNOWN |

## Related unit and integration tests

- [`unconfirmed_integration_test.go`](../../payment-service/pkg/services/unconfirmed_integration_test.go): an unreachable gateway past window + grace fails the attempt; inside the grace period the service keeps asking and never guesses.
- [`refund_after_payment_integration_test.go`](../../payment-service/pkg/services/refund_after_payment_integration_test.go): refund after a payment when a later saga step cannot finish.

## Limits

- The fault-injection scenarios (kills, restarts) did not produce an `UNKNOWN` payment in the cloud; the ambiguous path is exercised here by the simulator's timeout mode and by the tests above.
- Timings of the timeout run were the deployed defaults and were not recorded.

**Raw data:** [`test2-all-runs.json`](../correctness/data/cloud-suite/test2-all-runs.json)
