# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-09T19:29:45Z`

## C4 rate sweep

| Offered/s | Accepted/s | Completed/s | End-to-end p99 ms | Max in-flight | 5xx | Transport errors |
|---:|---:|---:|---:|---:|---:|---:|
| 100.00 | 100.00 | 100.00 | 15080.80 | 1072 | 0 | 0 |

No knee/saturation candidate was detected in the recorded steps.

## c4-rate-100

**What it does:** an open-loop scheduler offers 100 workflows/s for 3m0s across all 50 products without waiting for earlier workflows

**Result:** PASS — 18000 offered, 18000 accepted, 18000 completed (18000 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999809536 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 1m0s; measurement 3m0s.

**Deployment:** inprovemts-dyn2-100

**Completion observation:** `poll` every 5s.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 100.00/s |
| Accepted rate | 100.00/s |
| Completed rate | 100.00/s |
| Mean/max in-flight requests | 4.29 / 49 |
| Mean/max in-flight workflows | 917.82 / 1072 |
| Little's law observed / expected | 917.82 / 917.82 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 7.8% |
| Load generator memory at end | 13.9 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 18000 | 30.44 | 50.44 | 99.65 | 223.95 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 18000 | 10037.88 | 15043.06 | 15080.80 | 25058.87 |
| Server saga completion | 18000 | 6172.31 | 10882.94 | 14005.49 | 23995.54 |

Checkout status mix: `HTTP 202=18000, transport=0, decode=0, 5xx=0`  
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 50883 | 0 | 49 | 40.67 | 132 | 23171552 | 15/0/15 | 65303 | 3m14.641664408s | 66.22% / 105.48% | 26527143 / 35291136 |
| order | 0 | 0 | 0 | 0.00 | 38 | 5942480 | 8/0/8 | 0 | 0s | 18.51% / 35.64% | 13716631 / 19202048 |
| inventory | 18003 | 0 | 14 | 17.94 | 35 | 6589448 | 10/0/10 | 97 | 416.966028ms | 22.93% / 44.70% | 14156494 / 17379328 |
| payment | 54000 | 0 | 4 | 8.68 | 49 | 8611744 | 10/1/9 | 53654 | 1m0.229738671s | 38.06% / 55.84% | 14972604 / 19308544 |

### PostgreSQL metrics

Samples: 400. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 1. Deadlocks before/after: 0/0. Database bytes before/after: 8003584/8003584. New PostgreSQL sessions opened during the run: 0.

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

