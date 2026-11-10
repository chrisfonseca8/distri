# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-09T15:04:25Z`

## C4 rate sweep

| Offered/s | Accepted/s | Completed/s | End-to-end p99 ms | Max in-flight | 5xx | Transport errors |
|---:|---:|---:|---:|---:|---:|---:|
| 20.00 | 20.00 | 20.00 | 21409.01 | 412 | 0 | 0 |

No knee/saturation candidate was detected in the recorded steps.

## c4-rate-020

**What it does:** an open-loop scheduler offers 20 workflows/s for 3m0s across all 50 products without waiting for earlier workflows

**Result:** PASS — 3600 offered, 3600 accepted, 3600 completed (3600 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999809536 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 1m0s; measurement 3m0s.

**Deployment:** inprovemts-s20

**Completion observation:** `poll` every 2s.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 20.00/s |
| Accepted rate | 20.00/s |
| Completed rate | 20.00/s |
| Mean/max in-flight requests | 11.36 / 336 |
| Mean/max in-flight workflows | 172.80 / 412 |
| Little's law observed / expected | 172.80 / 172.80 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 3.6% |
| Load generator memory at end | 10.2 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 3600 | 34.08 | 1008.90 | 4339.05 | 8592.23 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 3600 | 8047.41 | 17594.47 | 21409.01 | 28066.29 |
| Server saga completion | 3600 | 6515.06 | 15767.80 | 19878.12 | 25055.18 |

Checkout status mix: `HTTP 202=3600, transport=0, decode=0, 5xx=0`  
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 18125 | 0 | 336 | 75.21 | 50 | 10842008 | 10/0/10 | 2616 | 30m5.408434498s | 19.18% / 45.25% | 21173741 / 34684928 |
| order | 0 | 0 | 0 | 0.00 | 34 | 5168536 | 8/0/8 | 0 | 0s | 5.99% / 23.68% | 13077148 / 15642624 |
| inventory | 3604 | 0 | 15 | 23.99 | 27 | 5088352 | 10/0/10 | 23 | 158.70464ms | 6.88% / 14.17% | 13741422 / 17915904 |
| payment | 10800 | 0 | 4 | 8.03 | 42 | 6417440 | 10/0/10 | 44 | 12.233425687s | 10.80% / 30.65% | 14984600 / 17371136 |

### PostgreSQL metrics

Samples: 379. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 8003584/8003584. New PostgreSQL sessions opened during the run: 0.

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

