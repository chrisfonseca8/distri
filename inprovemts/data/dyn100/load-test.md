# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-09T17:10:01Z`

## C4 rate sweep

| Offered/s | Accepted/s | Completed/s | End-to-end p99 ms | Max in-flight | 5xx | Transport errors |
|---:|---:|---:|---:|---:|---:|---:|
| 100.00 | 100.00 | 100.00 | 15072.48 | 1129 | 0 | 0 |

No knee/saturation candidate was detected in the recorded steps.

## c4-rate-100

**What it does:** an open-loop scheduler offers 100 workflows/s for 3m0s across all 50 products without waiting for earlier workflows

**Result:** PASS — 18000 offered, 18000 accepted, 18000 completed (18000 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999809536 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 1m0s; measurement 3m0s.

**Deployment:** inprovemts-dyn100

**Completion observation:** `poll` every 5s.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 100.00/s |
| Accepted rate | 100.00/s |
| Completed rate | 100.00/s |
| Mean/max in-flight requests | 4.19 / 76 |
| Mean/max in-flight workflows | 935.68 / 1129 |
| Little's law observed / expected | 935.68 / 935.68 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 7.8% |
| Load generator memory at end | 13.3 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 18000 | 31.05 | 46.26 | 89.41 | 353.42 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 18000 | 10038.45 | 15044.38 | 15072.48 | 25070.82 |
| Server saga completion | 18000 | 6342.80 | 11189.79 | 14086.01 | 24238.42 |

Checkout status mix: `HTTP 202=18000, transport=0, decode=0, 5xx=0`  
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 51530 | 0 | 76 | 38.86 | 100 | 19816792 | 15/0/15 | 40014 | 1m23.218514937s | 66.26% / 104.45% | 26409934 / 34177024 |
| order | 0 | 0 | 0 | 0.00 | 38 | 4974952 | 8/0/8 | 0 | 0s | 20.51% / 40.93% | 13553313 / 19066880 |
| inventory | 18004 | 0 | 14 | 17.93 | 27 | 7360984 | 10/0/10 | 31 | 88.860805ms | 22.27% / 38.11% | 14034763 / 19791872 |
| payment | 54000 | 0 | 4 | 8.07 | 45 | 6546192 | 10/0/10 | 41428 | 40.192509819s | 38.76% / 57.10% | 14629993 / 19963904 |

### PostgreSQL metrics

Samples: 398. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 1. Deadlocks before/after: 0/0. Database bytes before/after: 8003584/8003584. New PostgreSQL sessions opened during the run: 0.

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

