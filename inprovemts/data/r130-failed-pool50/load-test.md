# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-10T19:33:01Z`

## C4 rate sweep

| Offered/s | Accepted/s | Completed/s | End-to-end p99 ms | Max in-flight | 5xx | Transport errors |
|---:|---:|---:|---:|---:|---:|---:|
| 130.01 | 130.01 | 37.61 | 111595.31 | 15602 | 0 | 0 |

Automatic saturation candidate: **130 offered workflows/s** (completed rate below 95% of offered rate, or errors appeared).

## c4-rate-130

**What it does:** an open-loop scheduler offers 130 workflows/s for 3m0s across all 50 products without waiting for earlier workflows

**Result:** FAIL — 23401 offered, 23401 accepted, 6770 completed (6770 paid, 0 cancelled, 16631 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 1m0s; measurement 3m0s.

**Deployment:** inprovemts-r130

**Completion observation:** `poll` every 5s.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 130.01/s |
| Accepted rate | 130.01/s |
| Completed rate | 37.61/s |
| Mean/max in-flight requests | 111.76 / 1614 |
| Mean/max in-flight workflows | 12675.93 / 15602 |
| Little's law observed / expected | 12675.93 / 1588.56 (697.95% difference) |
| Load generator CPU (percent of one core, measured window) | 24.1% |
| Load generator memory at end | 80.4 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 23401 | 75.77 | 290.13 | 562.06 | 1496.26 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 6770 | 35129.50 | 100864.43 | 111595.31 | 116791.51 |
| Server saga completion | 10091 | 58251.15 | 213072.32 | 220080.70 | 223619.12 |

Checkout status mix: `HTTP 202=23401, transport=0, decode=0, 5xx=0`  
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 462745 | 0 | 1000 | 174.53 | 526 | 35057272 | 50/9/41 | 479701 | 3h32m58.603712623s | 87.09% / 174.16% | 64874475 / 114745344 |
| order | 0 | 0 | 0 | 0.00 | 38 | 5943832 | 8/1/7 | 0 | 0s | 13.72% / 28.67% | 17143377 / 21889024 |
| inventory | 23406 | 0 | 46 | 50.72 | 27 | 6251592 | 10/1/9 | 1384 | 31.573569446s | 11.94% / 25.97% | 17977344 / 22269952 |
| payment | 52898 | 0 | 4 | 22.08 | 70 | 13567720 | 20/18/2 | 11920 | 25.478570037s | 41.72% / 83.25% | 23299624 / 28299264 |

### PostgreSQL metrics

Samples: 601. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 4. Deadlocks before/after: 0/0. Database bytes before/after: 8003584/8003584. New PostgreSQL sessions opened during the run: 0.

`pg_stat_statements`: unavailable — pg_stat_statements is not enabled in the local PostgreSQL image.

### Invariants

- PASS — inventory counters stay within bounds (violations: 0)
- PASS — one reservation per order (violations: 0)
- PASS — one payment per order (violations: 0)
- PASS — no duplicate product line per order (violations: 0)
- PASS — paid orders have successful payment (violations: 0)
- PASS — cancelled orders have failed payment (violations: 0)
- FAIL — paid reservations are confirmed (violations: 4109)
- PASS — cancelled reservations are released (violations: 0)
- PASS — unknown payments retain pending orders (violations: 0)
- PASS — payment amounts match order totals in paise (violations: 0)

### Telemetry limitations

- PostgreSQL container: Get "http://docker/containers/json?filters=%7B%22label%22%3A%5B%22com.docker.compose.service%3Dpostgres%22%2C%22com.docker.compose.oneoff%3DFalse%22%5D%7D": dial unix /var/run/docker.sock: connect: no such file or directory.

