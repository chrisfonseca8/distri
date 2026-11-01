# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-01T14:11:40Z`

## C2 run-to-run variance (3 runs recorded)

| Metric | Mean | Min | Max | Std dev | CV |
|---|---:|---:|---:|---:|---:|
| Accepted/s | 1.59 | 1.57 | 1.63 | 0.03 | 1.78% |
| Completed/s | 1.59 | 1.57 | 1.63 | 0.03 | 1.78% |
| Checkout p95 ms | 59.75 | 50.07 | 67.63 | 7.28 | 12.18% |
| Pay p95 ms | 0.00 | 0.00 | 0.00 | 0.00 | 0.00% |
| End-to-end p95 ms | 11174.58 | 11122.62 | 11256.35 | 58.52 | 0.52% |

## c2-baseline-1

**What it does:** 10 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 95 offered, 95 accepted, 95 completed (95 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-staging

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 1.58/s |
| Accepted rate | 1.58/s |
| Completed rate | 1.58/s |
| Mean/max in-flight requests | 0.47 / 10 |
| Mean/max in-flight workflows | 10.60 / 10 |
| Little's law observed / expected | 10.60 / 10.60 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 2.2% |
| Load generator memory at end | 8.6 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 95 | 36.90 | 67.63 | 77.26 | 77.54 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 95 | 6571.08 | 11144.77 | 12712.43 | 13655.94 |
| Server saga completion | 95 | 6511.29 | 11054.61 | 12661.34 | 13587.25 |

Checkout status mix: `HTTP 202=95, transport=0, decode=0, 5xx=0`  
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 6142 | 0 | 10 | 4.54 | 49 | 5984768 | 10/0/10 | 0 | 0s | 7.87% / 12.77% | 13612872 / 16977920 |
| order | 0 | 0 | 0 | 0.00 | 36 | 4598584 | 5/0/5 | 0 | 0s | 2.61% / 6.86% | 10918499 / 16277504 |
| inventory | 96 | 0 | 10 | 32.83 | 27 | 5708352 | 10/0/10 | 0 | 0s | 1.50% / 6.37% | 14441854 / 19853312 |
| payment | 285 | 0 | 4 | 8.26 | 42 | 5784032 | 6/0/6 | 0 | 0s | 3.59% / 9.98% | 11996297 / 16986112 |

### PostgreSQL metrics

Samples: 135. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

## c2-baseline-2

**What it does:** 10 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 98 offered, 98 accepted, 98 completed (98 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-staging

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 1.63/s |
| Accepted rate | 1.63/s |
| Completed rate | 1.63/s |
| Mean/max in-flight requests | 0.48 / 10 |
| Mean/max in-flight workflows | 10.60 / 10 |
| Little's law observed / expected | 10.60 / 10.60 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 2.2% |
| Load generator memory at end | 8.8 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 98 | 38.02 | 50.07 | 51.53 | 52.41 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 98 | 6111.58 | 11122.62 | 13225.23 | 17808.44 |
| Server saga completion | 98 | 5997.50 | 11055.37 | 13142.38 | 17746.56 |

Checkout status mix: `HTTP 202=98, transport=0, decode=0, 5xx=0`  
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 6133 | 0 | 10 | 4.71 | 51 | 4559456 | 10/0/10 | 28 | 60.726373ms | 7.84% / 13.24% | 14017536 / 18497536 |
| order | 0 | 0 | 0 | 0.00 | 36 | 3953096 | 5/0/5 | 0 | 0s | 2.63% / 7.46% | 11023179 / 13041664 |
| inventory | 99 | 0 | 10 | 21.94 | 27 | 5635656 | 10/0/10 | 0 | 0s | 1.53% / 6.64% | 13240922 / 13877248 |
| payment | 294 | 0 | 4 | 8.35 | 42 | 2868096 | 6/0/6 | 0 | 0s | 3.64% / 9.47% | 12064888 / 17518592 |

### PostgreSQL metrics

Samples: 137. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

## c2-baseline-3

**What it does:** 10 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 94 offered, 94 accepted, 94 completed (94 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-staging

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 1.57/s |
| Accepted rate | 1.57/s |
| Completed rate | 1.57/s |
| Mean/max in-flight requests | 0.49 / 10 |
| Mean/max in-flight workflows | 10.81 / 10 |
| Little's law observed / expected | 10.81 / 10.81 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 2.2% |
| Load generator memory at end | 8.9 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 94 | 37.28 | 61.55 | 69.26 | 86.25 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 94 | 6606.82 | 11256.35 | 12278.08 | 12746.50 |
| Server saga completion | 94 | 6527.13 | 11148.26 | 12146.05 | 12618.97 |

Checkout status mix: `HTTP 202=94, transport=0, decode=0, 5xx=0`  
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 6253 | 0 | 10 | 4.94 | 49 | 5504816 | 10/2/8 | 8 | 12.160026ms | 7.65% / 13.72% | 14239715 / 16953344 |
| order | 0 | 0 | 0 | 0.00 | 36 | 4738520 | 5/0/5 | 0 | 0s | 2.59% / 7.24% | 11357429 / 12357632 |
| inventory | 95 | 0 | 10 | 29.16 | 27 | 3052224 | 10/0/10 | 0 | 0s | 1.42% / 6.38% | 12800519 / 18563072 |
| payment | 282 | 0 | 3 | 7.49 | 42 | 3081656 | 6/0/6 | 0 | 0s | 3.51% / 11.14% | 11718223 / 13115392 |

### PostgreSQL metrics

Samples: 143. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

