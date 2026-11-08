# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-01T21:10:34Z`

## C2 run-to-run variance (3 runs recorded)

| Metric | Mean | Min | Max | Std dev | CV |
|---|---:|---:|---:|---:|---:|
| Accepted/s | 44.02 | 42.77 | 45.17 | 0.98 | 2.23% |
| Completed/s | 44.02 | 42.77 | 45.17 | 0.98 | 2.23% |
| Checkout p95 ms | 908.19 | 824.70 | 1051.28 | 101.64 | 11.19% |
| Pay p95 ms | 0.00 | 0.00 | 0.00 | 0.00 | 0.00% |
| End-to-end p95 ms | 17480.09 | 17198.03 | 18018.41 | 380.79 | 2.18% |

## c2-baseline-1

**What it does:** 500 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 2648 offered, 2648 accepted, 2648 completed (2648 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test7-payment-workers-8

**Completion observation:** `sse`.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 44.13/s |
| Accepted rate | 44.13/s |
| Completed rate | 44.13/s |
| Mean/max in-flight requests | 546.34 / 500 |
| Mean/max in-flight workflows | 546.34 / 500 |
| Little's law observed / expected | 546.34 / 546.34 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 2.1% |
| Load generator memory at end | 38.8 MB |
| SSE attempts / terminal events / polling fallbacks | 2648 / 2648 / 0 |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 2648 | 31.59 | 824.70 | 927.90 | 976.65 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 2648 | 12086.35 | 17223.83 | 19815.49 | 27533.30 |
| Server saga completion | 2648 | 11987.23 | 17042.49 | 19688.50 | 27511.90 |

Checkout status mix: `HTTP 202=2648, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 5296 | 0 | 500 | 15776.31 | 565 | 17012992 | 10/0/10 | 5337 | 4m2.665800687s | 31.36% / 105.88% | 53567930 / 61218816 |
| order | 0 | 0 | 0 | 0.00 | 41 | 3667088 | 7/1/6 | 0 | 0s | 14.05% / 50.60% | 12709251 / 16302080 |
| inventory | 2649 | 0 | 226 | 359.95 | 36 | 7528448 | 10/0/10 | 502 | 1m48.187479808s | 15.83% / 83.40% | 16532535 / 28127232 |
| payment | 7944 | 0 | 4 | 9.57 | 50 | 5026760 | 10/0/10 | 1250 | 1.409609139s | 24.30% / 53.13% | 13979122 / 17960960 |

### PostgreSQL metrics

Samples: 149. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 1. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

**Result:** PASS — 2710 offered, 2710 accepted, 2710 completed (2710 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test7-payment-workers-8

**Completion observation:** `sse`.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 45.17/s |
| Accepted rate | 45.17/s |
| Completed rate | 45.17/s |
| Mean/max in-flight requests | 551.36 / 500 |
| Mean/max in-flight workflows | 551.36 / 500 |
| Little's law observed / expected | 551.36 / 551.36 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 2.1% |
| Load generator memory at end | 35.3 MB |
| SSE attempts / terminal events / polling fallbacks | 2710 / 2710 / 0 |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 2710 | 32.82 | 1051.28 | 1249.09 | 1278.93 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 2710 | 11938.68 | 17198.03 | 20343.55 | 27873.70 |
| Server saga completion | 2710 | 11860.10 | 16822.27 | 20082.34 | 26670.17 |

Checkout status mix: `HTTP 202=2710, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 5420 | 0 | 501 | 15533.85 | 553 | 17759272 | 10/0/10 | 7602 | 4m48.825894518s | 29.39% / 86.84% | 60992259 / 78487552 |
| order | 0 | 0 | 0 | 0.00 | 53 | 6187480 | 9/0/9 | 0 | 0s | 13.77% / 41.26% | 12760077 / 17227776 |
| inventory | 2711 | 0 | 261 | 470.03 | 32 | 7276184 | 10/0/10 | 492 | 2m30.893006644s | 15.74% / 91.55% | 17076702 / 29270016 |
| payment | 8130 | 0 | 4 | 9.55 | 50 | 7846752 | 10/1/9 | 5204 | 6.117152514s | 23.45% / 70.14% | 14392971 / 18501632 |

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

**Result:** PASS — 2566 offered, 2566 accepted, 2566 completed (2566 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test7-payment-workers-8

**Completion observation:** `sse`.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 42.77/s |
| Accepted rate | 42.77/s |
| Completed rate | 42.77/s |
| Mean/max in-flight requests | 554.32 / 500 |
| Mean/max in-flight workflows | 554.33 / 500 |
| Little's law observed / expected | 554.33 / 554.33 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 2.2% |
| Load generator memory at end | 27.5 MB |
| SSE attempts / terminal events / polling fallbacks | 2566 / 2566 / 0 |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 2566 | 30.70 | 848.61 | 936.02 | 967.14 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 2566 | 12717.27 | 18018.41 | 21511.65 | 26055.85 |
| Server saga completion | 2566 | 12563.44 | 17774.41 | 21044.66 | 26024.25 |

Checkout status mix: `HTTP 202=2566, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 5132 | 0 | 501 | 16374.69 | 545 | 17037056 | 10/0/10 | 2582 | 4m3.518768925s | 29.39% / 90.63% | 61635269 / 80904192 |
| order | 0 | 0 | 0 | 0.00 | 48 | 4316864 | 9/0/9 | 0 | 0s | 13.92% / 39.52% | 12796613 / 15040512 |
| inventory | 2567 | 0 | 256 | 376.00 | 32 | 5001776 | 10/0/10 | 490 | 1m54.141327591s | 15.78% / 79.76% | 16774048 / 30601216 |
| payment | 7698 | 0 | 4 | 8.62 | 50 | 5727680 | 10/1/9 | 2280 | 2.347262593s | 23.80% / 49.27% | 14363006 / 16326656 |

### PostgreSQL metrics

Samples: 151. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 1. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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
