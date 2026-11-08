# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-01T20:52:20Z`

## C2 run-to-run variance (3 runs recorded)

| Metric | Mean | Min | Max | Std dev | CV |
|---|---:|---:|---:|---:|---:|
| Accepted/s | 39.80 | 39.10 | 40.20 | 0.50 | 1.25% |
| Completed/s | 39.80 | 39.10 | 40.20 | 0.50 | 1.25% |
| Checkout p95 ms | 1045.28 | 794.89 | 1462.77 | 297.16 | 28.43% |
| Pay p95 ms | 0.00 | 0.00 | 0.00 | 0.00 | 0.00% |
| End-to-end p95 ms | 19199.89 | 18796.51 | 19729.18 | 391.03 | 2.04% |

## c2-baseline-1

**What it does:** 500 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 2346 offered, 2346 accepted, 2346 completed (2346 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test7-orch-dbpool-20

**Completion observation:** `sse`.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 39.10/s |
| Accepted rate | 39.10/s |
| Completed rate | 39.10/s |
| Mean/max in-flight requests | 563.32 / 500 |
| Mean/max in-flight workflows | 563.32 / 500 |
| Little's law observed / expected | 563.32 / 563.32 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 1.9% |
| Load generator memory at end | 38.2 MB |
| SSE attempts / terminal events / polling fallbacks | 2346 / 2346 / 0 |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 2346 | 36.81 | 878.19 | 1139.37 | 1181.16 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 2346 | 14547.37 | 19729.18 | 22408.84 | 32832.32 |
| Server saga completion | 2346 | 14328.47 | 19461.29 | 22333.76 | 32800.90 |

Checkout status mix: `HTTP 202=2346, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 4692 | 0 | 501 | 18235.40 | 565 | 17599232 | 20/0/20 | 2089 | 1m21.412013387s | 26.50% / 112.39% | 59385164 / 74207232 |
| order | 0 | 0 | 0 | 0.00 | 44 | 4158248 | 7/0/7 | 0 | 0s | 11.58% / 43.08% | 12450266 / 13578240 |
| inventory | 2347 | 0 | 335 | 704.30 | 35 | 4672888 | 10/0/10 | 492 | 3m26.867048649s | 14.49% / 74.37% | 16619316 / 33574912 |
| payment | 7038 | 0 | 4 | 10.25 | 46 | 5492656 | 10/0/10 | 210 | 191.748177ms | 22.42% / 60.02% | 14843524 / 16982016 |

### PostgreSQL metrics

Samples: 152. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 1. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

**Result:** PASS — 2406 offered, 2406 accepted, 2406 completed (2406 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test7-orch-dbpool-20

**Completion observation:** `sse`.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 40.10/s |
| Accepted rate | 40.10/s |
| Completed rate | 40.10/s |
| Mean/max in-flight requests | 549.61 / 500 |
| Mean/max in-flight workflows | 549.62 / 500 |
| Little's law observed / expected | 549.62 / 549.62 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 2.1% |
| Load generator memory at end | 28.0 MB |
| SSE attempts / terminal events / polling fallbacks | 2406 / 2406 / 0 |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 2406 | 37.28 | 1462.77 | 1733.26 | 1792.83 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 2406 | 13615.16 | 18796.51 | 22442.19 | 31065.61 |
| Server saga completion | 2406 | 13494.58 | 18552.42 | 22087.97 | 29621.54 |

Checkout status mix: `HTTP 202=2406, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 4812 | 0 | 500 | 17142.34 | 557 | 14493216 | 20/0/20 | 2103 | 3m36.624405614s | 29.13% / 87.58% | 67059541 / 85815296 |
| order | 0 | 0 | 0 | 0.00 | 44 | 3528128 | 8/0/8 | 0 | 0s | 12.50% / 48.63% | 12624867 / 15384576 |
| inventory | 2407 | 0 | 326 | 1104.98 | 35 | 6975920 | 10/0/10 | 492 | 5m45.150526451s | 14.95% / 54.09% | 16674389 / 32444416 |
| payment | 7218 | 0 | 4 | 9.98 | 50 | 4620120 | 10/0/10 | 155 | 289.430273ms | 23.09% / 52.53% | 14849308 / 18894848 |

### PostgreSQL metrics

Samples: 145. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 2. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

**Result:** PASS — 2412 offered, 2412 accepted, 2412 completed (2412 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test7-orch-dbpool-20

**Completion observation:** `sse`.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 40.20/s |
| Accepted rate | 40.20/s |
| Completed rate | 40.20/s |
| Mean/max in-flight requests | 561.77 / 500 |
| Mean/max in-flight workflows | 561.77 / 500 |
| Little's law observed / expected | 561.77 / 561.77 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 2.1% |
| Load generator memory at end | 41.8 MB |
| SSE attempts / terminal events / polling fallbacks | 2412 / 2412 / 0 |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 2412 | 33.04 | 794.89 | 1023.70 | 1136.50 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 2412 | 13967.79 | 19073.98 | 22639.97 | 28784.49 |
| Server saga completion | 2412 | 13925.72 | 18921.67 | 22259.58 | 28752.69 |

Checkout status mix: `HTTP 202=2412, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 4824 | 0 | 501 | 17544.58 | 549 | 19883832 | 20/0/20 | 1592 | 1m19.520151184s | 28.13% / 109.15% | 66959807 / 87916544 |
| order | 0 | 0 | 0 | 0.00 | 40 | 4328072 | 8/0/8 | 0 | 0s | 12.25% / 45.10% | 12793327 / 16183296 |
| inventory | 2414 | 0 | 340 | 655.46 | 31 | 5055480 | 10/0/10 | 492 | 3m8.609737251s | 14.30% / 85.51% | 16524755 / 35356672 |
| payment | 7236 | 0 | 4 | 10.18 | 46 | 6676176 | 10/3/7 | 72 | 84.549688ms | 22.12% / 41.73% | 15093298 / 16617472 |

### PostgreSQL metrics

Samples: 152. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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
