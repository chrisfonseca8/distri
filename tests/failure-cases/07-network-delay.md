# Failure case 07 — Network delay (200 ms ± 50 ms, 40 s)

**Scenario id:** `network-delay`  
**Result:** PASS

## What is tested

Slow network between the payment service and its dependencies (database, broker, gateway). Timeouts must not turn into duplicate charges or lost sagas.

## How the fault is injected

On the payment host, add 200 ms ± 50 ms delay to all outgoing traffic with `tc` for 40 s.

## Load during the test

- 200 checkouts offered at 50 per second from 30 concurrent clients, against the AWS staging stack and the gateway simulator in `success` mode.
- The fault starts about 1.5 s into the burst; 57 sagas were in flight when it started.
- The harness then waits for every accepted saga to reach a final state and re-checks the database invariants.

## Expected behaviour

Other hosts were not delayed, so no checkout is refused.

## Result

| Measure | Value |
|---|---:|
| Checkouts accepted (HTTP 202) | 200 |
| Refused while the service was down | 0 |
| Sagas in flight when the fault started | 57 |
| Final saga status | 200 COMPLETED |
| Unfinished sagas | 0 |
| Unfinished with an unknown payment | 0 |
| Payments beyond one per order | 0 |
| Reservations beyond one per order | 0 |
| Accepted sagas without an order | 0 |
| Dead-queue growth | 0 |
| Fault duration (s) | 43.0 |
| Time for the last saga to finish after the burst (s) | 16.6 |
| Scenario duration (s) | 70.3 |

## Database invariants (violations found, before / after the fault)

| Invariant | Before | After |
|---|---:|---:|
| inventory counters within bounds | 0 | 0 |
| one reservation per order | 0 | 0 |
| one payment per order | 0 | 0 |
| one order row per saga | 0 | 0 |
| paid orders have a succeeded payment | 0 | 0 |
| paid orders have a confirmed reservation | 0 | 0 |
| cancelled orders have a released reservation | 0 | 0 |
| unknown payments keep the order pending | 0 | 0 |

## Limits of this test

- Run once; no repeats or variance figure.
- Only checkouts the orchestrator accepted are judged. A refused request was never promised.
- The gateway is the simulator; no ambiguous (UNKNOWN) payment arose in this run, so the "outcome unknown, never retried blindly" path was exercised by tests, not by this scenario.
- Single replica of every service, one broker, one database.

**Raw data:** [`chaos-test.json`](data/fault-injection/chaos-test.json), [`audit.json`](data/fault-injection/audit.json) (observability audit after the run)
