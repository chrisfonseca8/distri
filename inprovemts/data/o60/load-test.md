# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-09T15:25:23Z`

## C4 rate sweep

| Offered/s | Accepted/s | Completed/s | End-to-end p99 ms | Max in-flight | 5xx | Transport errors |
|---:|---:|---:|---:|---:|---:|---:|
| 60.01 | 60.01 | 60.01 | 86284.22 | 3436 | 0 | 0 |

No knee/saturation candidate was detected in the recorded steps.

## c4-rate-060

**What it does:** an open-loop scheduler offers 60 workflows/s for 3m0s across all 50 products without waiting for earlier workflows

**Result:** PASS — 10801 offered, 10801 accepted, 10801 completed (10801 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999809536 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 1m0s; measurement 3m0s.

**Deployment:** inprovemts-o60

**Completion observation:** `poll` every 2s.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 60.01/s |
| Accepted rate | 60.01/s |
| Completed rate | 60.01/s |
| Mean/max in-flight requests | 14.19 / 225 |
| Mean/max in-flight workflows | 2701.01 / 3436 |
| Little's law observed / expected | 2701.01 / 2701.01 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 17.6% |
| Load generator memory at end | 23.0 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 10801 | 30.05 | 75.17 | 143.28 | 545.84 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 10801 | 44269.74 | 80440.67 | 86284.22 | 92381.91 |
| Server saga completion | 10801 | 43630.06 | 80060.37 | 85062.32 | 92263.64 |

Checkout status mix: `HTTP 202=10801, transport=0, decode=0, 5xx=0`  
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 252561 | 0 | 224 | 39.23 | 134 | 17021424 | 10/0/10 | 235827 | 26m44.357236147s | 59.60% / 107.49% | 28081600 / 38653952 |
| order | 0 | 0 | 0 | 0.00 | 34 | 6927176 | 8/0/8 | 0 | 0s | 9.90% / 22.90% | 13279646 / 16683008 |
| inventory | 10805 | 0 | 6 | 17.77 | 30 | 4214072 | 10/0/10 | 0 | 0s | 12.57% / 23.26% | 13801004 / 19111936 |
| payment | 32403 | 0 | 4 | 7.50 | 38 | 6170504 | 10/0/10 | 776 | 697.618786ms | 19.94% / 35.43% | 15800534 / 20586496 |

### PostgreSQL metrics

Samples: 535. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 8003584/8003584. New PostgreSQL sessions opened during the run: 0.

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

