# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-09T19:57:45Z`

## C4 rate sweep

| Offered/s | Accepted/s | Completed/s | End-to-end p99 ms | Max in-flight | 5xx | Transport errors |
|---:|---:|---:|---:|---:|---:|---:|
| 160.00 | 160.00 | 67.38 | 116909.83 | 16969 | 0 | 0 |

Automatic saturation candidate: **160 offered workflows/s** (completed rate below 95% of offered rate, or errors appeared).

## c4-rate-160

**What it does:** an open-loop scheduler offers 160 workflows/s for 3m0s across all 50 products without waiting for earlier workflows

**Result:** PASS — 28800 offered, 28800 accepted, 12129 completed (12129 paid, 0 cancelled, 16671 pending at drain).

**Environment:** 2 logical CPUs, 3999809536 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 1m0s; measurement 3m0s.

**Deployment:** inprovemts-dyn3-160

**Completion observation:** `poll` every 5s.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 160.00/s |
| Accepted rate | 160.00/s |
| Completed rate | 67.38/s |
| Mean/max in-flight requests | 184.51 / 3043 |
| Mean/max in-flight workflows | 13622.25 / 16969 |
| Little's law observed / expected | 13622.25 / 2508.21 (443.11% difference) |
| Load generator CPU (percent of one core, measured window) | 24.8% |
| Load generator memory at end | 92.0 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 28800 | 51.42 | 461.62 | 946.77 | 1953.68 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 12129 | 30186.77 | 85832.78 | 116909.83 | 118769.11 |
| Server saga completion | 20909 | 59411.81 | 172282.29 | 174443.14 | 182980.69 |

Checkout status mix: `HTTP 202=28800, transport=0, decode=0, 5xx=0`  
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 502063 | 0 | 1000 | 268.89 | 370 | 51106720 | 40/18/22 | 729529 | 6h0m40.667217862s | 99.98% / 158.43% | 61307958 / 123686912 |
| order | 0 | 0 | 0 | 0.00 | 40 | 3306432 | 9/3/6 | 0 | 0s | 18.25% / 33.79% | 13830239 / 18497536 |
| inventory | 28805 | 0 | 16 | 25.93 | 41 | 5976024 | 10/4/6 | 226 | 1.523881104s | 21.11% / 44.28% | 15027172 / 21942272 |
| payment | 80184 | 0 | 4 | 20.35 | 64 | 8439528 | 10/10/0 | 319211 | 19m41.614132742s | 39.56% / 57.74% | 15738265 / 19619840 |

### PostgreSQL metrics

Samples: 601. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 3. Deadlocks before/after: 0/0. Database bytes before/after: 8003584/8003584. New PostgreSQL sessions opened during the run: 0.

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

