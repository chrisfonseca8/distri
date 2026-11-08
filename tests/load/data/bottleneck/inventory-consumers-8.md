# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-01T21:18:56Z`

## C2 run-to-run variance (3 runs recorded)

| Metric | Mean | Min | Max | Std dev | CV |
|---|---:|---:|---:|---:|---:|
| Accepted/s | 44.23 | 41.47 | 47.20 | 2.35 | 5.30% |
| Completed/s | 44.23 | 41.47 | 47.20 | 2.35 | 5.30% |
| Checkout p95 ms | 1105.26 | 815.12 | 1610.85 | 358.80 | 32.46% |
| Pay p95 ms | 0.00 | 0.00 | 0.00 | 0.00 | 0.00% |
| End-to-end p95 ms | 18157.49 | 16737.85 | 19855.76 | 1288.04 | 7.09% |

## c2-baseline-1

**What it does:** 500 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 2488 offered, 2488 accepted, 2488 completed (2488 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test7-inventory-consumers-8

**Completion observation:** `sse`.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 41.47/s |
| Accepted rate | 41.47/s |
| Completed rate | 41.47/s |
| Mean/max in-flight requests | 546.80 / 500 |
| Mean/max in-flight workflows | 546.80 / 500 |
| Little's law observed / expected | 546.80 / 546.80 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 2.1% |
| Load generator memory at end | 37.4 MB |
| SSE attempts / terminal events / polling fallbacks | 2488 / 2488 / 0 |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 2488 | 37.45 | 1610.85 | 1711.17 | 1759.57 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 2488 | 12522.84 | 19855.76 | 24542.67 | 30205.20 |
| Server saga completion | 2488 | 12438.51 | 18933.45 | 23235.99 | 30179.49 |

Checkout status mix: `HTTP 202=2488, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 4976 | 0 | 501 | 16877.31 | 566 | 16708248 | 10/0/10 | 6907 | 9m19.7620963s | 31.76% / 67.32% | 54601272 / 62492672 |
| order | 0 | 0 | 0 | 0.00 | 42 | 5796816 | 9/0/9 | 0 | 0s | 14.09% / 34.33% | 12903338 / 15482880 |
| inventory | 2489 | 0 | 203 | 525.97 | 31 | 5310792 | 10/0/10 | 496 | 2m38.90794471s | 15.85% / 60.69% | 14373290 / 24813568 |
| payment | 7464 | 0 | 4 | 11.52 | 42 | 6468400 | 10/1/9 | 306 | 547.891016ms | 24.05% / 50.75% | 13996714 / 15593472 |

### PostgreSQL metrics

Samples: 145. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 1. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

**What it does:** 500 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 2641 offered, 2641 accepted, 2641 completed (2641 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test7-inventory-consumers-8

**Completion observation:** `sse`.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 44.02/s |
| Accepted rate | 44.02/s |
| Completed rate | 44.02/s |
| Mean/max in-flight requests | 549.10 / 500 |
| Mean/max in-flight workflows | 549.10 / 500 |
| Little's law observed / expected | 549.10 / 549.10 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 2.1% |
| Load generator memory at end | 30.5 MB |
| SSE attempts / terminal events / polling fallbacks | 2641 / 2641 / 0 |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 2641 | 34.46 | 889.81 | 995.88 | 1025.01 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 2641 | 12118.51 | 17878.86 | 21190.22 | 34349.88 |
| Server saga completion | 2641 | 12007.78 | 17521.19 | 20971.51 | 34333.62 |

Checkout status mix: `HTTP 202=2641, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 5282 | 0 | 500 | 15957.71 | 549 | 12641984 | 10/0/10 | 6098 | 4m24.065739466s | 30.85% / 102.05% | 62220766 / 80093184 |
| order | 0 | 0 | 0 | 0.00 | 42 | 2765608 | 9/0/9 | 0 | 0s | 13.33% / 49.44% | 12685258 / 14770176 |
| inventory | 2642 | 0 | 238 | 357.39 | 28 | 4282904 | 10/0/10 | 491 | 1m57.246333493s | 15.80% / 82.51% | 14682697 / 26509312 |
| payment | 7923 | 0 | 4 | 9.10 | 42 | 6339080 | 10/0/10 | 89 | 189.178042ms | 22.77% / 43.53% | 14169101 / 19156992 |

### PostgreSQL metrics

Samples: 155. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

**What it does:** 500 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 2832 offered, 2832 accepted, 2832 completed (2832 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test7-inventory-consumers-8

**Completion observation:** `sse`.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 47.20/s |
| Accepted rate | 47.20/s |
| Completed rate | 47.20/s |
| Mean/max in-flight requests | 555.44 / 500 |
| Mean/max in-flight workflows | 555.45 / 500 |
| Little's law observed / expected | 555.45 / 555.45 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 2.4% |
| Load generator memory at end | 29.4 MB |
| SSE attempts / terminal events / polling fallbacks | 2832 / 2832 / 0 |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 2832 | 31.88 | 815.12 | 975.48 | 1002.88 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 2832 | 11372.24 | 16737.85 | 19993.52 | 32408.31 |
| Server saga completion | 2832 | 11306.96 | 16483.10 | 19741.15 | 32325.94 |

Checkout status mix: `HTTP 202=2832, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 5664 | 0 | 500 | 15101.80 | 550 | 16426648 | 10/0/10 | 2768 | 3m34.594591951s | 35.70% / 105.82% | 62453913 / 83562496 |
| order | 0 | 0 | 0 | 0.00 | 43 | 3175536 | 8/0/8 | 0 | 0s | 14.28% / 49.34% | 12834049 / 18137088 |
| inventory | 2834 | 0 | 270 | 429.49 | 28 | 4298088 | 10/0/10 | 508 | 2m14.416826024s | 17.39% / 89.40% | 14582038 / 29188096 |
| payment | 8496 | 0 | 4 | 8.45 | 46 | 4063656 | 10/0/10 | 255 | 259.554838ms | 25.11% / 47.11% | 14306018 / 19013632 |

### PostgreSQL metrics

Samples: 148. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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
