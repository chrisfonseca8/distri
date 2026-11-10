# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-09T14:44:17Z`

## C4 rate sweep

| Offered/s | Accepted/s | Completed/s | End-to-end p99 ms | Max in-flight | 5xx | Transport errors |
|---:|---:|---:|---:|---:|---:|---:|
| 20.00 | 20.00 | 20.00 | 14070.05 | 161 | 0 | 0 |

No knee/saturation candidate was detected in the recorded steps.

## c4-rate-020

**What it does:** an open-loop scheduler offers 20 workflows/s for 30s across all 50 products without waiting for earlier workflows

**Result:** PASS — 600 offered, 600 accepted, 600 completed (600 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999809536 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 1m0s; measurement 30s.

**Deployment:** inprovemts-s20

**Completion observation:** `poll` every 2s.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 20.00/s |
| Accepted rate | 20.00/s |
| Completed rate | 20.00/s |
| Mean/max in-flight requests | 0.92 / 5 |
| Mean/max in-flight workflows | 149.37 / 161 |
| Little's law observed / expected | 149.37 / 149.37 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 2.4% |
| Load generator memory at end | 9.3 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 600 | 32.42 | 46.27 | 62.96 | 73.11 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 600 | 8038.31 | 12055.20 | 14070.05 | 16086.84 |
| Server saga completion | 600 | 6065.68 | 10306.66 | 13942.79 | 14561.96 |

Checkout status mix: `HTTP 202=600, transport=0, decode=0, 5xx=0`  
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 2826 | 0 | 5 | 37.70 | 46 | 8512728 | 10/0/10 | 0 | 0s | 15.19% / 28.79% | 13918928 / 16334848 |
| order | 0 | 0 | 0 | 0.00 | 38 | 5115856 | 6/0/6 | 0 | 0s | 4.92% / 11.23% | 12089861 / 13582336 |
| inventory | 600 | 0 | 2 | 22.98 | 27 | 4351184 | 4/0/4 | 0 | 0s | 5.51% / 13.30% | 12073027 / 15683584 |
| payment | 1800 | 0 | 4 | 6.99 | 42 | 5626416 | 10/0/10 | 0 | 0s | 8.51% / 18.01% | 14682134 / 15654912 |

### PostgreSQL metrics

Samples: 92. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 8003584/8003584. New PostgreSQL sessions opened during the run: 0.

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

