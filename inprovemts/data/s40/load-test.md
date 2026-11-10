# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-09T15:15:07Z`

## C4 rate sweep

| Offered/s | Accepted/s | Completed/s | End-to-end p99 ms | Max in-flight | 5xx | Transport errors |
|---:|---:|---:|---:|---:|---:|---:|
| 40.00 | 40.00 | 40.00 | 16055.55 | 443 | 0 | 0 |

No knee/saturation candidate was detected in the recorded steps.

## c4-rate-040

**What it does:** an open-loop scheduler offers 40 workflows/s for 3m0s across all 50 products without waiting for earlier workflows

**Result:** PASS — 7200 offered, 7200 accepted, 7200 completed (7200 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999809536 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 1m0s; measurement 3m0s.

**Deployment:** inprovemts-s40

**Completion observation:** `poll` every 2s.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 40.00/s |
| Accepted rate | 40.00/s |
| Completed rate | 40.00/s |
| Mean/max in-flight requests | 1.95 / 29 |
| Mean/max in-flight workflows | 323.38 / 443 |
| Little's law observed / expected | 323.38 / 323.38 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 6.0% |
| Load generator memory at end | 9.7 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 7200 | 29.62 | 43.04 | 111.11 | 269.55 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 7200 | 8043.70 | 12062.91 | 16055.55 | 28095.76 |
| Server saga completion | 7200 | 6617.41 | 11330.91 | 14451.32 | 27899.90 |

Checkout status mix: `HTTP 202=7200, transport=0, decode=0, 5xx=0`  
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 36120 | 0 | 29 | 33.95 | 93 | 12907744 | 10/0/10 | 10262 | 40.572281104s | 33.97% / 73.91% | 19707348 / 27201536 |
| order | 0 | 0 | 0 | 0.00 | 34 | 6416352 | 8/0/8 | 0 | 0s | 9.61% / 16.63% | 13180472 / 17625088 |
| inventory | 7203 | 0 | 5 | 17.68 | 27 | 5386008 | 9/0/9 | 0 | 0s | 11.66% / 23.38% | 13642654 / 19988480 |
| payment | 21600 | 0 | 4 | 7.35 | 42 | 7438168 | 10/0/10 | 172 | 164.663877ms | 18.31% / 30.99% | 15412200 / 17084416 |

### PostgreSQL metrics

Samples: 388. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 8003584/8003584. New PostgreSQL sessions opened during the run: 0.

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

