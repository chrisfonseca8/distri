# Load test 03 — Client sweep, SSE completion (peak)

**Result:** PASS. Peak **50.8 completed/s at 1000 clients** (single run, tested up to 1000 clients). 1000 is where the test stopped, not a measured ceiling.

## What is tested

How far throughput scales when the orchestrator pushes completion to clients over Server-Sent Events, so the load generator does not load the application with polling.

## How it is run

Case C3, closed loop, 500 and 1000 clients; 10 s warm-up, 60 s measured; one run per point. AWS staging stack; gateway simulator with its 5 s pay delay.

## Results

| Clients | 500 | 1000 |
|---|---:|---:|
| Accepted = completed per s | 37.6 | **50.8** |
| End-to-end p95 / p99 (s) | 20.0 / 23.8 | 31.6 / 36.4 |
| Checkout p50 / p95 (ms) | 33 / 1503 | 37 / 1845 |
| Completed; pending at drain | 2,258; 0 | 3,045; 0 |
| SSE attempts / terminal events / polling fallbacks (1000 clients) | – | 3,045 / 3,045 / 0 |

At 500 clients the same configuration gave 37.6/s here and 42.9/s in the [bottleneck baseline](05-bottleneck-experiment.md) (three repetitions, spread 3.3%). Both are reported; the gap is within the roughly 20% noise of single points.

## What saturates (1000 clients, SSE)

| Service | |
|---|---|
| orchestrator CPU mean / max | 30% / 104% |
| orchestrator requests during the step | 6,090 |
| orchestrator DB pool waits | 8,015 |
| inventory CPU mean / max | 16% / **95%** |
| inventory requests in flight (max) | 369 |
| payment, order | idle (payment 24% mean) |

Inventory-service is the service nearest its limit. That is an inference from metrics, not a profile of the code. Postgres showed no lock waits or deadlocks.

## Limits

- Single runs; an earlier run of 29 October on an older build reached 60/s at 1000 clients, and no reason for the difference was established.
- No SSE run above 1000 clients, so where SSE throughput flattens is unknown.
- Latency includes the simulator's 5 s pay delay.
- The load generator host was not checked for saturation at 1000 clients.

**Raw data:** [`sweep-clients-sse-500-1000.md`](data/limit-sweep/sweep-clients-sse-500-1000.md), [`.json`](data/limit-sweep/sweep-clients-sse-500-1000.json)
