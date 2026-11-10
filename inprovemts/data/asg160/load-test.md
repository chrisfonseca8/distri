# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-10T18:17:14Z`

## C4 rate sweep

| Offered/s | Accepted/s | Completed/s | End-to-end p99 ms | Max in-flight | 5xx | Transport errors |
|---:|---:|---:|---:|---:|---:|---:|
| 160.00 | 160.00 | 90.12 | 115492.52 | 14097 | 0 | 0 |

Automatic saturation candidate: **160 offered workflows/s** (completed rate below 95% of offered rate, or errors appeared).

## c4-rate-160

**What it does:** an open-loop scheduler offers 160 workflows/s for 3m0s across all 50 products without waiting for earlier workflows

**Result:** PASS — 28800 offered, 28800 accepted, 16221 completed (16221 paid, 0 cancelled, 12579 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 1m0s; measurement 3m0s.

**Deployment:** inprovemts-asg160

**Completion observation:** `poll` every 5s.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 160.00/s |
| Accepted rate | 160.00/s |
| Completed rate | 90.12/s |
| Mean/max in-flight requests | 55.04 / 646 |
| Mean/max in-flight workflows | 11702.56 / 14097 |
| Little's law observed / expected | 11702.56 / 3316.53 (252.86% difference) |
| Load generator CPU (percent of one core, measured window) | 17.1% |
| Load generator memory at end | 84.5 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 28800 | 47.80 | 100.89 | 155.40 | 335.73 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 16221 | 25139.80 | 100549.03 | 115492.52 | 115948.82 |
| Server saga completion | 27526 | 66318.43 | 144914.72 | 146412.73 | 152893.17 |

Checkout status mix: `HTTP 202=28800, transport=0, decode=0, 5xx=0`  
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 436955 | 0 | 560 | 68.22 | 261 | 41535160 | 20/3/17 | 817199 | 2h0m19.664216209s | 105.15% / 156.26% | 47344428 / 82235392 |
| order | 0 | 0 | 0 | 0.00 | 46 | 6809352 | 8/0/8 | 0 | 0s | 18.65% / 41.16% | 18048225 / 23056384 |
| inventory | 28805 | 0 | 19 | 21.99 | 42 | 6425056 | 6/5/1 | 126 | 699.176288ms | 17.99% / 43.70% | 18374184 / 23564288 |
| payment | 86400 | 0 | 4 | 12.07 | 55 | 13278224 | 15/0/15 | 78972 | 1m30.417379831s | 57.94% / 93.66% | 22047839 / 28446720 |

### PostgreSQL metrics

Samples: 601. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 2. Deadlocks before/after: 0/0. Database bytes before/after: 8028160/8028160. New PostgreSQL sessions opened during the run: 0.

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

