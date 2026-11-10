# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-09T16:51:44Z`

## C4 rate sweep

| Offered/s | Accepted/s | Completed/s | End-to-end p99 ms | Max in-flight | 5xx | Transport errors |
|---:|---:|---:|---:|---:|---:|---:|
| 60.01 | 60.01 | 60.01 | 15054.14 | 646 | 0 | 0 |

No knee/saturation candidate was detected in the recorded steps.

## c4-rate-060

**What it does:** an open-loop scheduler offers 60 workflows/s for 3m0s across all 50 products without waiting for earlier workflows

**Result:** PASS — 10801 offered, 10801 accepted, 10801 completed (10801 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999809536 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 1m0s; measurement 3m0s.

**Deployment:** inprovemts-dyn60

**Completion observation:** `poll` every 5s.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 60.01/s |
| Accepted rate | 60.01/s |
| Completed rate | 60.01/s |
| Mean/max in-flight requests | 2.21 / 35 |
| Mean/max in-flight workflows | 563.73 / 646 |
| Little's law observed / expected | 563.73 / 563.73 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 5.8% |
| Load generator memory at end | 10.5 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 10801 | 28.27 | 39.82 | 80.23 | 243.22 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 10801 | 10034.63 | 15038.90 | 15054.14 | 25052.08 |
| Server saga completion | 10801 | 6416.02 | 10926.63 | 13957.34 | 20923.65 |

Checkout status mix: `HTTP 202=10801, transport=0, decode=0, 5xx=0`  
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 31013 | 0 | 35 | 33.50 | 89 | 14191264 | 10/0/10 | 4323 | 13.106403705s | 43.38% / 77.74% | 19525962 / 26177536 |
| order | 0 | 0 | 0 | 0.00 | 38 | 4731328 | 8/2/6 | 0 | 0s | 13.82% / 51.26% | 13281807 / 18853888 |
| inventory | 10805 | 0 | 10 | 16.32 | 39 | 4871416 | 9/0/9 | 9 | 18.095163ms | 15.19% / 28.53% | 13670606 / 18673664 |
| payment | 32403 | 0 | 4 | 6.39 | 44 | 7898352 | 10/0/10 | 1100 | 838.889728ms | 24.95% / 38.56% | 14017722 / 19513344 |

### PostgreSQL metrics

Samples: 397. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 8003584/8003584. New PostgreSQL sessions opened during the run: 0.

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

