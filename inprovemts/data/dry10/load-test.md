# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-09T14:40:30Z`

## C4 rate sweep

| Offered/s | Accepted/s | Completed/s | End-to-end p99 ms | Max in-flight | 5xx | Transport errors |
|---:|---:|---:|---:|---:|---:|---:|
| 10.00 | 10.00 | 10.00 | 16069.30 | 83 | 0 | 0 |

No knee/saturation candidate was detected in the recorded steps.

## c4-rate-010

**What it does:** an open-loop scheduler offers 10 workflows/s for 30s across all 50 products without waiting for earlier workflows

**Result:** PASS — 300 offered, 300 accepted, 300 completed (300 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999809536 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 30s.

**Deployment:** inprovemts-dry10

**Completion observation:** `poll` every 2s.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 10.00/s |
| Accepted rate | 10.00/s |
| Completed rate | 10.00/s |
| Mean/max in-flight requests | 0.47 / 5 |
| Mean/max in-flight workflows | 76.76 / 83 |
| Little's law observed / expected | 76.76 / 76.76 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 1.5% |
| Load generator memory at end | 10.9 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 300 | 31.94 | 44.70 | 65.65 | 126.19 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 300 | 8040.45 | 12055.57 | 16069.30 | 28093.65 |
| Server saga completion | 300 | 6258.52 | 10683.89 | 14090.35 | 26196.48 |

Checkout status mix: `HTTP 202=300, transport=0, decode=0, 5xx=0`  
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 1444 | 0 | 5 | 34.79 | 43 | 5952720 | 9/0/9 | 0 | 0s | 9.58% / 18.86% | 12602833 / 14680064 |
| order | 0 | 0 | 0 | 0.00 | 34 | 4789168 | 6/0/6 | 0 | 0s | 3.55% / 8.59% | 12139752 / 13242368 |
| inventory | 301 | 0 | 1 | 17.81 | 26 | 4258192 | 3/0/3 | 0 | 0s | 3.29% / 8.75% | 12005376 / 13352960 |
| payment | 900 | 0 | 4 | 6.49 | 42 | 5555368 | 8/0/8 | 0 | 0s | 5.60% / 12.30% | 13712244 / 15024128 |

### PostgreSQL metrics

Samples: 89. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 8003584/8003584. New PostgreSQL sessions opened during the run: 0.

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

