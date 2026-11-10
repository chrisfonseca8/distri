# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-09T17:03:44Z`

## C4 rate sweep

| Offered/s | Accepted/s | Completed/s | End-to-end p99 ms | Max in-flight | 5xx | Transport errors |
|---:|---:|---:|---:|---:|---:|---:|
| 80.00 | 80.00 | 80.00 | 16787.18 | 1188 | 0 | 0 |

No knee/saturation candidate was detected in the recorded steps.

## c4-rate-080

**What it does:** an open-loop scheduler offers 80 workflows/s for 3m0s across all 50 products without waiting for earlier workflows

**Result:** PASS — 14400 offered, 14400 accepted, 14400 completed (14400 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999809536 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 1m0s; measurement 3m0s.

**Deployment:** inprovemts-dyn80

**Completion observation:** `poll` every 5s.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 80.00/s |
| Accepted rate | 80.00/s |
| Completed rate | 80.00/s |
| Mean/max in-flight requests | 10.09 / 460 |
| Mean/max in-flight workflows | 768.29 / 1188 |
| Little's law observed / expected | 768.29 / 768.29 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 7.1% |
| Load generator memory at end | 16.1 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 14400 | 32.00 | 110.59 | 1244.42 | 2394.89 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 14400 | 10041.27 | 15056.58 | 16787.18 | 26912.32 |
| Server saga completion | 14400 | 6493.31 | 11552.46 | 14880.96 | 22095.59 |

Checkout status mix: `HTTP 202=14400, transport=0, decode=0, 5xx=0`  
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 41692 | 0 | 460 | 77.70 | 134 | 19578968 | 20/0/20 | 39624 | 21m42.998686217s | 54.76% / 101.66% | 35247483 / 51916800 |
| order | 0 | 0 | 0 | 0.00 | 38 | 5370512 | 8/0/8 | 0 | 0s | 17.06% / 44.50% | 13664606 / 16207872 |
| inventory | 14403 | 0 | 28 | 20.16 | 31 | 6136248 | 10/0/10 | 283 | 2.956338403s | 18.38% / 40.76% | 13994866 / 17776640 |
| payment | 43200 | 0 | 4 | 8.39 | 48 | 7738864 | 10/0/10 | 21910 | 40.738191878s | 31.26% / 56.87% | 14494722 / 19320832 |

### PostgreSQL metrics

Samples: 396. Connections mean/max: 1.00/2. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 4. Deadlocks before/after: 0/0. Database bytes before/after: 8003584/8003584. New PostgreSQL sessions opened during the run: 0.

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

