# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-09T16:45:16Z`

## C4 rate sweep

| Offered/s | Accepted/s | Completed/s | End-to-end p99 ms | Max in-flight | 5xx | Transport errors |
|---:|---:|---:|---:|---:|---:|---:|
| 50.00 | 50.00 | 50.00 | 15056.34 | 490 | 0 | 0 |

No knee/saturation candidate was detected in the recorded steps.

## c4-rate-050

**What it does:** an open-loop scheduler offers 50 workflows/s for 3m0s across all 50 products without waiting for earlier workflows

**Result:** PASS — 9000 offered, 9000 accepted, 9000 completed (9000 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999809536 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 1m0s; measurement 3m0s.

**Deployment:** inprovemts-dyn50

**Completion observation:** `poll` every 5s.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 50.00/s |
| Accepted rate | 50.00/s |
| Completed rate | 50.00/s |
| Mean/max in-flight requests | 1.93 / 33 |
| Mean/max in-flight workflows | 458.54 / 490 |
| Little's law observed / expected | 458.54 / 458.54 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 4.8% |
| Load generator memory at end | 10.0 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 9000 | 30.23 | 40.92 | 89.01 | 336.56 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 9000 | 10036.40 | 15040.04 | 15056.34 | 25052.81 |
| Server saga completion | 9000 | 6183.45 | 10790.81 | 13663.64 | 22360.00 |

Checkout status mix: `HTTP 202=9000, transport=0, decode=0, 5xx=0`  
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 25436 | 0 | 33 | 36.25 | 63 | 11852160 | 10/0/10 | 5255 | 14.66975539s | 36.45% / 57.48% | 18910658 / 26779648 |
| order | 0 | 0 | 0 | 0.00 | 38 | 4060224 | 8/0/8 | 0 | 0s | 11.60% / 25.63% | 13315092 / 17465344 |
| inventory | 9003 | 0 | 7 | 17.07 | 27 | 6200712 | 9/0/9 | 0 | 0s | 13.87% / 25.26% | 13496698 / 18857984 |
| payment | 27000 | 0 | 4 | 6.99 | 44 | 5558088 | 10/0/10 | 659 | 534.075916ms | 21.40% / 35.10% | 14060789 / 19402752 |

### PostgreSQL metrics

Samples: 401. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 8003584/8003584. New PostgreSQL sessions opened during the run: 0.

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

