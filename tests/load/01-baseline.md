# Load test 01 — Idle baseline (10 clients)

**Result:** PASS. Not a target: a reference point for later tests.

## What is tested

How the deployed stack behaves at low load: how long one checkout takes when nothing is saturated, and whether any invariant fails.

## How it is run

- Case C2: 10 closed-loop clients, 10 s warm-up, 60 s measured, repeated 3 times.
- Between repetitions the runner resets transactional data and inventory. Purchases are spread round-robin over all 50 products.
- Run from the load-generator host against the AWS staging services; gateway simulator in `success` mode with 200 ms API latency and a 5 s pay delay.

## Results

| | Run 1 | Run 2 | Run 3 | Mean | Spread (CV) |
|---|---:|---:|---:|---:|---:|
| Offered = accepted = completed | 94 | 97 | 88 | 93 | – |
| Accepted = completed per second | 1.57 | 1.62 | 1.47 | **1.55** | 4.0% |
| End-to-end p95 | 11.1 s | 10.2 s | 12.6 s | **11.3 s** | 8.5% |
| Pending at the drain | 0 | 0 | 0 | – | – |

Run 1 in detail: checkout p50 / p95 / p99 = 38 / 54 / 84 ms; end-to-end p50 / p95 / p99 = 6.9 / 11.1 / 15.7 s. All 94 checkouts returned HTTP 202 with no 5xx, no transport errors and no cancelled orders. Little's law holds (10.9 workflows in flight observed against 10.9 expected), so the measurement is internally consistent.

## Where the time goes

Mean saga step durations over the three repetitions (342 sagas, Prometheus):

| Step | Mean |
|---|---:|
| create order | 0.16 s |
| confirm stock | 0.33 s |
| mark order paid | 0.14 s |
| **take payment** | **6.16 s** |
| saga total | 6.75 s |

The simulator's configured 5 s pay delay (`SIM_PAY_DELAY`) accounts for most of the payment step. About 1.2 s of the payment step is not explained by what was measured. The accept call is fast (about 66 ms p95 including a database write), so database distance is not the explanation for the latency.

| Service (run 1) | CPU mean / max | DB pool waits |
|---|---|---:|
| orchestrator | 7% / 13% | 65 (1.1 s total) |
| order | 2.5% / 7.8% | 0 |
| inventory | 1.4% / 6.8% | 0 |
| payment | 3.5% / 9.4% | 0 |

No service is busy. Throughput here is clients divided by latency (10 / 6.9 s ≈ 1.5 per second).

## Invariants and audit

Stock bounds, one reservation and payment per order, paid orders have a successful payment and a confirmed reservation, cancelled orders have failed payments: all pass in every repetition. The phase-22 audit afterwards passes. The observability audit shows 0 stuck sagas, 0 dead outbox rows, 0 unknown or unconfirmed payments, 0 stock violations.

## Limits

- Simulated gateway; the 7 s median is mostly the simulator's pay delay.
- 10 clients only: this says nothing about behaviour near the limit.
- Three short repetitions (88-97 workflows each).
- The `DatabasePoolSaturated` and `ServiceDown` alerts fired during this run (not diagnosed).
- Git commit, image tag and Neon branch were not recorded.

**Raw data:** [`load-test.md`](data/baseline/load-test.md), [`load-test.json`](data/baseline/load-test.json), [`phase22-audit.md`](data/baseline/phase22-audit.md), [`audit.json`](data/baseline/audit.json)
