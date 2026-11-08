# Failure case 11 — Overload: 500 clients on one hot product

**Result:** the run **did not pass its own accounting check**; no correctness invariant was violated.

## What is tested

What happens when far more clients than the stack handles hit a single product row at once. The expected behaviour under overload is to refuse some requests cleanly (never to oversell, lose an accepted checkout, or leave sagas pending).

This run was not designed as a failure test. It ran as the first of two cases in one command (`LOAD_CASE=C5` with `LOAD_CLIENTS=500`); the setting meant for the sell-out case also applied to the throughput case, whose normal setting is 50 clients. It is recorded here because it shows how the system behaves past its limit on a hot row. It must not be quoted as a capacity figure.

## How it is run

500 closed-loop clients buy one high-stock product for 60 s after a 10 s warm-up, against the AWS staging stack and the gateway simulator in `success` mode.

## Result

| Measure | Value |
|---|---:|
| Checkouts offered | 2,184 |
| Accepted (HTTP 202) | 2,033 |
| Refused with HTTP 503 | 151 |
| Completed | 2,033 (2,033 paid, 0 cancelled, 0 pending at the drain) |
| Completed rate | 33.88/s |
| Checkout latency p50 / p95 / p99 | 150 ms / 3.08 s / 3.21 s |
| End-to-end p95 | 21.6 s |
| Lock waiters, deadlocks | 0 |
| Failed check | "one unique completed checkout attempt per offered workflow": 151 violations, exactly the refused requests |

## What the metrics suggest (inference, not a traced cause)

- **inventory-service:** 151 errors, up to 468 requests in flight, handler p95 of 3.0 s. A p95 of almost exactly 3 s suggests a timeout, but the limit was not looked up in the code.
- **orchestrator-service:** 151 errors, up to 500 requests in flight and 397,580 database pool waits on a pool of 10 connections.
- Together they point at the inventory step on a single row and database pool waits. About 7% of checkouts were refused with 503.

## What it shows

- Nothing was oversold, and every accepted checkout finished. Overload surfaced as refused requests, not as incorrect state.
- The cause of the 503s was not traced, and whether the `ServiceDown` alert that fired belongs to the same overload was not checked.

**Raw data:** [`load-test.md`](../correctness/data/oversell/load-test.md) (case `c5a-hot-product-throughput`), [`load-test.json`](../correctness/data/oversell/load-test.json)
