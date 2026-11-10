# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-09T15:09:51Z`

## C4 rate sweep

| Offered/s | Accepted/s | Completed/s | End-to-end p99 ms | Max in-flight | 5xx | Transport errors |
|---:|---:|---:|---:|---:|---:|---:|
| 30.01 | 30.01 | 30.01 | 16059.31 | 314 | 0 | 0 |

No knee/saturation candidate was detected in the recorded steps.

## c4-rate-030

**What it does:** an open-loop scheduler offers 30 workflows/s for 3m0s across all 50 products without waiting for earlier workflows

**Result:** PASS — 5401 offered, 5401 accepted, 5401 completed (5401 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999809536 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 1m0s; measurement 3m0s.

**Deployment:** inprovemts-s30

**Completion observation:** `poll` every 2s.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 30.01/s |
| Accepted rate | 30.01/s |
| Completed rate | 30.01/s |
| Mean/max in-flight requests | 1.47 / 26 |
| Mean/max in-flight workflows | 231.17 / 314 |
| Little's law observed / expected | 231.17 / 231.17 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 4.7% |
| Load generator memory at end | 9.5 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 5401 | 31.58 | 41.55 | 97.80 | 259.00 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 5401 | 8043.05 | 12058.12 | 16059.31 | 24140.74 |
| Server saga completion | 5401 | 6283.81 | 10951.83 | 14260.09 | 23544.14 |

Checkout status mix: `HTTP 202=5401, transport=0, decode=0, 5xx=0`  
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 26068 | 0 | 26 | 35.83 | 86 | 10764392 | 10/0/10 | 2051 | 8.428443132s | 26.66% / 62.27% | 18506362 / 23085056 |
| order | 0 | 0 | 0 | 0.00 | 34 | 3820656 | 8/0/8 | 0 | 0s | 7.95% / 14.55% | 13103644 / 21172224 |
| inventory | 5404 | 0 | 5 | 17.51 | 27 | 4167264 | 10/0/10 | 0 | 0s | 9.23% / 22.98% | 13575817 / 17371136 |
| payment | 16203 | 0 | 4 | 7.30 | 42 | 6053160 | 10/0/10 | 31 | 19.390716ms | 14.49% / 23.26% | 15222222 / 20029440 |

### PostgreSQL metrics

Samples: 395. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 8003584/8003584. New PostgreSQL sessions opened during the run: 0.

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

