# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-01T20:38:56Z`

## C2 run-to-run variance (3 runs recorded)

| Metric | Mean | Min | Max | Std dev | CV |
|---|---:|---:|---:|---:|---:|
| Accepted/s | 42.86 | 41.80 | 44.83 | 1.40 | 3.27% |
| Completed/s | 42.86 | 41.80 | 44.83 | 1.40 | 3.27% |
| Checkout p95 ms | 855.46 | 784.19 | 907.04 | 52.05 | 6.08% |
| Pay p95 ms | 0.00 | 0.00 | 0.00 | 0.00 | 0.00% |
| End-to-end p95 ms | 18125.81 | 17410.48 | 18570.40 | 510.77 | 2.82% |

## c2-baseline-1

**What it does:** 500 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 2508 offered, 2508 accepted, 2508 completed (2508 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test7-baseline

**Completion observation:** `sse`.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 41.80/s |
| Accepted rate | 41.80/s |
| Completed rate | 41.80/s |
| Mean/max in-flight requests | 557.99 / 500 |
| Mean/max in-flight workflows | 557.99 / 500 |
| Little's law observed / expected | 557.99 / 557.99 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 2.0% |
| Load generator memory at end | 37.5 MB |
| SSE attempts / terminal events / polling fallbacks | 2508 / 2508 / 0 |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 2508 | 33.81 | 907.04 | 1001.89 | 1031.41 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 2508 | 13193.83 | 18570.40 | 21714.10 | 29072.82 |
| Server saga completion | 2508 | 13150.49 | 18328.03 | 21684.14 | 29050.48 |

Checkout status mix: `HTTP 202=2508, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 5016 | 0 | 501 | 16823.45 | 571 | 14731672 | 10/0/10 | 6142 | 4m31.837572918s | 29.08% / 97.97% | 60596141 / 67411968 |
| order | 0 | 0 | 0 | 0.00 | 44 | 4847328 | 8/0/8 | 0 | 0s | 12.33% / 51.59% | 12533182 / 17059840 |
| inventory | 2509 | 0 | 246 | 390.66 | 30 | 6570552 | 10/0/10 | 491 | 1m59.181265583s | 15.90% / 84.71% | 16607080 / 28741632 |
| payment | 7524 | 0 | 4 | 10.85 | 45 | 4040944 | 10/1/9 | 286 | 434.405502ms | 23.87% / 59.52% | 14687370 / 16572416 |

### PostgreSQL metrics

Samples: 150. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

**Result:** PASS — 2516 offered, 2516 accepted, 2516 completed (2516 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test7-baseline

**Completion observation:** `sse`.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 41.93/s |
| Accepted rate | 41.93/s |
| Completed rate | 41.93/s |
| Mean/max in-flight requests | 561.36 / 500 |
| Mean/max in-flight workflows | 561.36 / 500 |
| Little's law observed / expected | 561.36 / 561.36 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 2.0% |
| Load generator memory at end | 35.3 MB |
| SSE attempts / terminal events / polling fallbacks | 2516 / 2516 / 0 |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 2516 | 33.22 | 875.14 | 949.35 | 991.96 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 2516 | 13286.62 | 18396.56 | 22078.32 | 26954.13 |
| Server saga completion | 2516 | 13225.69 | 18051.19 | 21980.26 | 26924.38 |

Checkout status mix: `HTTP 202=2516, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 5032 | 0 | 501 | 16864.60 | 558 | 17427592 | 10/0/10 | 4323 | 4m11.468197352s | 28.18% / 95.65% | 68116998 / 83865600 |
| order | 0 | 0 | 0 | 0.00 | 44 | 7447952 | 7/0/7 | 0 | 0s | 12.23% / 40.95% | 12748144 / 18186240 |
| inventory | 2517 | 0 | 255 | 386.50 | 34 | 3902352 | 10/0/10 | 490 | 2m2.699183372s | 14.97% / 85.00% | 16767276 / 29745152 |
| payment | 7548 | 0 | 4 | 9.78 | 49 | 6530232 | 10/0/10 | 393 | 442.629998ms | 23.70% / 59.94% | 14867141 / 17612800 |

### PostgreSQL metrics

Samples: 151. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

**Result:** PASS — 2690 offered, 2690 accepted, 2690 completed (2690 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test7-baseline

**Completion observation:** `sse`.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 44.83/s |
| Accepted rate | 44.83/s |
| Completed rate | 44.83/s |
| Mean/max in-flight requests | 557.76 / 500 |
| Mean/max in-flight workflows | 557.76 / 500 |
| Little's law observed / expected | 557.76 / 557.76 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 1.9% |
| Load generator memory at end | 41.4 MB |
| SSE attempts / terminal events / polling fallbacks | 2690 / 2690 / 0 |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 2690 | 31.15 | 784.19 | 901.26 | 930.68 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 2690 | 12110.17 | 17410.48 | 20441.00 | 29099.90 |
| Server saga completion | 2690 | 12063.24 | 17137.28 | 20133.90 | 29079.09 |

Checkout status mix: `HTTP 202=2690, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 5380 | 0 | 501 | 15765.26 | 562 | 18992624 | 10/0/10 | 2401 | 3m42.109933033s | 27.10% / 99.21% | 64922618 / 85749760 |
| order | 0 | 0 | 0 | 0.00 | 44 | 5825280 | 7/0/7 | 0 | 0s | 11.96% / 36.19% | 12501796 / 14479360 |
| inventory | 2691 | 0 | 222 | 340.26 | 30 | 4577408 | 10/0/10 | 490 | 1m47.376861997s | 13.94% / 92.64% | 16732562 / 28479488 |
| payment | 8070 | 0 | 4 | 9.02 | 45 | 7085712 | 10/1/9 | 178 | 137.924752ms | 21.56% / 43.09% | 14909771 / 19632128 |

### PostgreSQL metrics

Samples: 174. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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
