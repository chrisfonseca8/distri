# Failure case 06 — Database connection drop (20 s)

**Scenario id:** `db-connection-drop`  
**Result:** PASS

## What is tested

The payment service loses its database for 20 s while serving traffic. Pools reconnect; commands that failed are redelivered or retried.

## How the fault is injected

On the payment host, reset TCP connections to port 5432 with `iptables` for 20 s.

## Load during the test

- 200 checkouts offered at 50 per second from 30 concurrent clients, against the AWS staging stack and the gateway simulator in `success` mode.
- The fault starts about 1.5 s into the burst; 57 sagas were in flight when it started.
- The harness then waits for every accepted saga to reach a final state and re-checks the database invariants.

## Expected behaviour

Other services stay up, so no checkout is refused.

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
| Fault duration (s) | 20.5 |
| Time for the last saga to finish after the burst (s) | 46.5 |
| Scenario duration (s) | 77.4 |

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
