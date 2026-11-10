# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-10T20:01:29Z`

## C4 rate sweep

| Offered/s | Accepted/s | Completed/s | End-to-end p99 ms | Max in-flight | 5xx | Transport errors |
|---:|---:|---:|---:|---:|---:|---:|
| 130.01 | 130.01 | 29.18 | 116168.90 | 15602 | 0 | 0 |

Automatic saturation candidate: **130 offered workflows/s** (completed rate below 95% of offered rate, or errors appeared).

## c4-rate-130

**What it does:** an open-loop scheduler offers 130 workflows/s for 3m0s across all 50 products without waiting for earlier workflows

**Result:** FAIL — 23401 offered, 23401 accepted, 5252 completed (5252 paid, 0 cancelled, 18149 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 1m0s; measurement 3m0s.

**Deployment:** inprovemts-r130

**Completion observation:** `poll` every 5s.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 130.01/s |
| Accepted rate | 130.01/s |
| Completed rate | 29.18/s |
| Mean/max in-flight requests | 180.19 / 1816 |
| Mean/max in-flight workflows | 13337.17 / 15602 |
| Little's law observed / expected | 13337.17 / 1237.80 (977.49% difference) |
| Load generator CPU (percent of one core, measured window) | 18.1% |
| Load generator memory at end | 115.5 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 23401 | 81.53 | 344.19 | 590.12 | 1565.61 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 5252 | 35185.71 | 101096.43 | 116168.90 | 117479.69 |
| Server saga completion | 8545 | 66246.05 | 225134.51 | 232467.13 | 235377.68 |

Checkout status mix: `HTTP 202=23401, transport=0, decode=0, 5xx=0`  
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 484957 | 0 | 1000 | 242.38 | 688 | 42095080 | 20/8/12 | 927560 | 7h57m1.025128986s | 86.02% / 166.48% | 62943389 / 111554560 |
| order | 0 | 0 | 0 | 0.00 | 43 | 5506456 | 8/1/7 | 0 | 0s | 13.50% / 27.37% | 17482909 / 22884352 |
| inventory | 23406 | 0 | 33 | 34.29 | 39 | 8966968 | 10/1/9 | 363 | 5.585775991s | 11.30% / 26.57% | 17979487 / 22577152 |
| payment | 47493 | 0 | 4 | 17.97 | 74 | 13697416 | 15/15/0 | 39600 | 1m27.029015215s | 34.43% / 65.85% | 21672120 / 26439680 |

### PostgreSQL metrics

Samples: 601. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 2. Deadlocks before/after: 0/0. Database bytes before/after: 8003584/8003584. New PostgreSQL sessions opened during the run: 0.

`pg_stat_statements`: unavailable — pg_stat_statements is not enabled in the local PostgreSQL image.

### Invariants

- PASS — inventory counters stay within bounds (violations: 0)
- PASS — one reservation per order (violations: 0)
- PASS — one payment per order (violations: 0)
- PASS — no duplicate product line per order (violations: 0)
- PASS — paid orders have successful payment (violations: 0)
- PASS — cancelled orders have failed payment (violations: 0)
- FAIL — paid reservations are confirmed (violations: 4181)
- PASS — cancelled reservations are released (violations: 0)
- PASS — unknown payments retain pending orders (violations: 0)
- PASS — payment amounts match order totals in paise (violations: 0)

### Telemetry limitations

- PostgreSQL container: Get "http://docker/containers/json?filters=%7B%22label%22%3A%5B%22com.docker.compose.service%3Dpostgres%22%2C%22com.docker.compose.oneoff%3DFalse%22%5D%7D": dial unix /var/run/docker.sock: connect: no such file or directory.

