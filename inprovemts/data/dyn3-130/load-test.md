# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-09T19:47:00Z`

## C4 rate sweep

| Offered/s | Accepted/s | Completed/s | End-to-end p99 ms | Max in-flight | 5xx | Transport errors |
|---:|---:|---:|---:|---:|---:|---:|
| 130.01 | 130.01 | 130.01 | 30166.82 | 3484 | 0 | 0 |

No knee/saturation candidate was detected in the recorded steps.

## c4-rate-130

**What it does:** an open-loop scheduler offers 130 workflows/s for 3m0s across all 50 products without waiting for earlier workflows

**Result:** PASS — 23401 offered, 23401 accepted, 23401 completed (23401 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999809536 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 1m0s; measurement 3m0s.

**Deployment:** inprovemts-dyn3-130

**Completion observation:** `poll` every 5s.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 130.01/s |
| Accepted rate | 130.01/s |
| Completed rate | 130.01/s |
| Mean/max in-flight requests | 8.79 / 156 |
| Mean/max in-flight workflows | 2433.15 / 3484 |
| Little's law observed / expected | 2433.15 / 2433.15 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 12.6% |
| Load generator memory at end | 25.3 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 23401 | 40.16 | 60.92 | 120.87 | 400.42 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 23401 | 20056.35 | 30079.49 | 30166.82 | 40171.72 |
| Server saga completion | 23401 | 15842.45 | 26244.50 | 29546.50 | 37934.39 |

Checkout status mix: `HTTP 202=23401, transport=0, decode=0, 5xx=0`  
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 110669 | 0 | 155 | 47.21 | 185 | 27573568 | 20/0/20 | 112078 | 4m31.143320178s | 82.14% / 112.90% | 33847956 / 48365568 |
| order | 0 | 0 | 0 | 0.00 | 42 | 4166432 | 9/0/9 | 0 | 0s | 21.27% / 34.85% | 13860740 / 19591168 |
| inventory | 23405 | 0 | 17 | 21.16 | 39 | 6571760 | 10/0/10 | 209 | 829.578876ms | 26.37% / 45.59% | 14420504 / 19288064 |
| payment | 70203 | 0 | 4 | 16.42 | 50 | 7375016 | 10/0/10 | 274854 | 13m21.005067066s | 46.53% / 66.63% | 15800125 / 18567168 |

### PostgreSQL metrics

Samples: 432. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 1. Deadlocks before/after: 0/0. Database bytes before/after: 8003584/8003584. New PostgreSQL sessions opened during the run: 0.

`pg_stat_statements`: unavailable — pg_stat_statements is not enabled in the local PostgreSQL image.

### Invariants

- PASS — inventory counters stay within bounds (violations: 0)
- PASS — one reservation per order (violations: 0)
- PASS — one payment per order (violations: 0)
- PASS — no duplicate product line per order (violations: 0)
- PASS — paid orders have successful payment (violations: 0)
- PASS — cancelled orders have failed payment (violations: 0)
- PASS — paid reservations are confirmed (violations: 0)
- PASS — cancelled reservations are released (violations: 0)
- PASS — unknown payments retain pending orders (violations: 0)
- PASS — payment amounts match order totals in paise (violations: 0)

### Telemetry limitations

- PostgreSQL container: Get "http://docker/containers/json?filters=%7B%22label%22%3A%5B%22com.docker.compose.service%3Dpostgres%22%2C%22com.docker.compose.oneoff%3DFalse%22%5D%7D": dial unix /var/run/docker.sock: connect: no such file or directory.

