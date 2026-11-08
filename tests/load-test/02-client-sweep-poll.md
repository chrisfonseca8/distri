# Load test 02 — Client sweep, poll completion

**Result:** PASS. Throughput peaks at 300 clients, then falls; the fall is caused by the measurement method.

## What is tested

How completed checkouts per second grow as the number of concurrent clients increases, with the load generator discovering completion by polling the orchestrator's status endpoint every 100 ms.

## How it is run

- Case C3, closed loop: 1, 2, 5, 10, 20, 50, 100, 200 (twice), 300, 400 and 600 clients; 10 s warm-up, 60 s measured per step; one run per point.
- AWS staging stack; gateway simulator in `success` mode with its 5 s pay delay.
- The sweep ran as two invocations on the same build (1-200 and 200-600 clients).

## Results

| Clients | 1 | 2 | 5 | 10 | 20 | 50 | 100 | 200 | 200 (repeat) | 300 | 400 | 600 |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| Accepted = completed per s | 0.17 | 0.30 | 0.85 | 1.63 | 3.32 | 7.85 | 15.8 | 25.1 | 31.2 | **36.9** | 31.1 | 20.9 |
| End-to-end p95 (s) | 8.1 | 10.6 | 10.6 | 10.7 | 9.9 | 10.5 | 11.2 | 15.7 | 11.0 | 13.7 | 20.1 | 43.4 |

Throughput is near-linear up to 100 clients (each checkout spends about 6 s in the simulator, so throughput ≈ clients / 6 s), then bends. The 200-client step ran twice and gave 25.1 and 31.2/s, so **single points vary by about 20%**. Every workflow that was accepted completed in every step; accepted equals completed.

## Why throughput falls after 300 clients

| Orchestrator, 600 clients, poll mode | |
|---|---:|
| CPU mean / max | 98% / 132% |
| Requests during the step | 197,692 |
| Database pool waits (pool of 10) | 401,606 |

The orchestrator is busy only because the clients' own status polling sent it about 200,000 requests and exhausted its connection pool. The fall after 300 clients is the measurement loading the application, not the application's limit. See [load test 03](03-client-sweep-sse.md) for the same stack measured without polling.

## Limits

- One run per point except the 200-client step.
- Simulated gateway with a 5 s pay delay.
- Poll numbers are a lower bound; use SSE numbers for capacity.

**Raw data:** [`sweep-clients-poll-1-200`](data/limit-sweep/sweep-clients-poll-1-200.md) and [`sweep-clients-poll-200-600`](data/limit-sweep/sweep-clients-poll-200-600.md) (`.md` and `.json`)
