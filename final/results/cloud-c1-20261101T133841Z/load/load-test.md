# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-01T13:41:53Z`

## c1-smoke

**What it does:** one client performs 20 checkout → pay → terminal-order workflows sequentially, then verifies all invariants

**Result:** PASS — 20 offered, 20 accepted, 20 completed (20 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 0s; measurement 2m24.31217728s.

**Deployment:** cloud-staging

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 0.14/s |
| Accepted rate | 0.14/s |
| Completed rate | 0.14/s |
| Mean/max in-flight requests | 0.05 / 1 |
| Mean/max in-flight workflows | 1.00 / 1 |
| Little's law observed / expected | 1.00 / 1.00 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 0.7% |
| Load generator memory at end | 8.4 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 20 | 42.76 | 60.92 | 75.13 | 75.13 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 20 | 7503.35 | 10641.43 | 13601.31 | 13601.31 |
| Server saga completion | 20 | 7427.41 | 10523.26 | 13480.83 | 13480.83 |

Checkout status mix: `HTTP 202=20, transport=0, decode=0, 5xx=0`  
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 1386 | 0 | 1 | 4.61 | 40 | 4149464 | 5/0/5 | 0 | 0s | 2.73% / 7.84% | 11635021 / 16203776 |
| order | 0 | 0 | 0 | 0.00 | 35 | 5544776 | 5/0/5 | 0 | 0s | 2.17% / 7.27% | 11442962 / 17432576 |
| inventory | 22 | 0 | 1 | 27.12 | 26 | 7382104 | 2/0/2 | 0 | 0s | 0.75% / 5.85% | 12475409 / 17420288 |
| payment | 60 | 0 | 3 | 10.29 | 39 | 2434952 | 5/0/5 | 0 | 0s | 2.55% / 7.59% | 11988595 / 16728064 |

### PostgreSQL metrics

Samples: 290. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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
- PASS — one order per accepted checkout (violations: 0)
- PASS — one confirmed reservation per paid order (violations: 0)
- PASS — one successful payment per paid order (violations: 0)
- PASS — one released reservation per cancelled order (violations: 0)
- PASS — one failed payment per cancelled order (violations: 0)
- PASS — one unique completed checkout attempt per offered workflow (violations: 0)

### Telemetry limitations

- PostgreSQL container: Get "http://docker/containers/json?filters=%7B%22label%22%3A%5B%22com.docker.compose.service%3Dpostgres%22%2C%22com.docker.compose.oneoff%3DFalse%22%5D%7D": dial unix /var/run/docker.sock: connect: no such file or directory.

