# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-01T19:31:28Z`

## C2 run-to-run variance (3 runs recorded)

| Metric | Mean | Min | Max | Std dev | CV |
|---|---:|---:|---:|---:|---:|
| Accepted/s | 1.55 | 1.47 | 1.62 | 0.06 | 4.02% |
| Completed/s | 1.55 | 1.47 | 1.62 | 0.06 | 4.02% |
| Checkout p95 ms | 66.09 | 54.31 | 85.24 | 13.66 | 20.68% |
| Pay p95 ms | 0.00 | 0.00 | 0.00 | 0.00 | 0.00% |
| End-to-end p95 ms | 11309.34 | 10235.32 | 12566.06 | 960.25 | 8.49% |

## c2-baseline-1

**What it does:** 10 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 94 offered, 94 accepted, 94 completed (94 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test5-baseline

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 1.57/s |
| Accepted rate | 1.57/s |
| Completed rate | 1.57/s |
| Mean/max in-flight requests | 0.52 / 10 |
| Mean/max in-flight workflows | 10.86 / 10 |
| Little's law observed / expected | 10.86 / 10.86 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 2.0% |
| Load generator memory at end | 8.5 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 94 | 37.72 | 54.31 | 83.50 | 126.26 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 94 | 6927.01 | 11126.64 | 15683.44 | 16337.06 |
| Server saga completion | 94 | 6828.31 | 11076.26 | 15579.83 | 16266.73 |

Checkout status mix: `HTTP 202=94, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 6264 | 0 | 10 | 5.10 | 54 | 8988664 | 10/0/10 | 65 | 1.078031597s | 7.38% / 13.44% | 24143512 / 24997888 |
| order | 0 | 0 | 0 | 0.00 | 36 | 6139968 | 7/0/7 | 0 | 0s | 2.49% / 7.75% | 12434930 / 17768448 |
| inventory | 95 | 0 | 10 | 27.23 | 29 | 7287232 | 10/0/10 | 0 | 0s | 1.36% / 6.81% | 18598912 / 19742720 |
| payment | 282 | 0 | 4 | 9.38 | 44 | 5811008 | 7/0/7 | 0 | 0s | 3.45% / 9.37% | 14124253 / 18198528 |

### PostgreSQL metrics

Samples: 149. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

**Result:** PASS — 97 offered, 97 accepted, 97 completed (97 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test5-baseline

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 1.62/s |
| Accepted rate | 1.62/s |
| Completed rate | 1.62/s |
| Mean/max in-flight requests | 0.40 / 10 |
| Mean/max in-flight workflows | 10.52 / 10 |
| Little's law observed / expected | 10.52 / 10.52 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 2.3% |
| Load generator memory at end | 8.6 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 97 | 35.88 | 58.71 | 59.41 | 59.89 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 97 | 6083.90 | 10235.32 | 11724.44 | 13747.28 |
| Server saga completion | 97 | 6013.98 | 10124.66 | 11606.53 | 13677.77 |

Checkout status mix: `HTTP 202=97, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 6132 | 0 | 10 | 4.29 | 53 | 6656208 | 10/0/10 | 4 | 3.712428ms | 7.99% / 12.91% | 23965352 / 25120768 |
| order | 0 | 0 | 0 | 0.00 | 36 | 3764120 | 7/0/7 | 0 | 0s | 2.69% / 8.80% | 12343686 / 17039360 |
| inventory | 98 | 0 | 10 | 32.00 | 29 | 7115456 | 10/0/10 | 0 | 0s | 1.56% / 7.35% | 17970934 / 19066880 |
| payment | 291 | 0 | 4 | 8.11 | 43 | 5367520 | 7/0/7 | 0 | 0s | 3.76% / 9.67% | 13906764 / 16228352 |

### PostgreSQL metrics

Samples: 132. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

**Result:** PASS — 88 offered, 88 accepted, 88 completed (88 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test5-baseline

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 1.47/s |
| Accepted rate | 1.47/s |
| Completed rate | 1.47/s |
| Mean/max in-flight requests | 1.28 / 10 |
| Mean/max in-flight workflows | 10.69 / 10 |
| Little's law observed / expected | 10.69 / 10.69 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 2.0% |
| Load generator memory at end | 8.6 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 88 | 39.14 | 85.24 | 224.15 | 367.61 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 88 | 7050.71 | 12566.06 | 13075.75 | 15739.74 |
| Server saga completion | 88 | 7002.43 | 12480.45 | 13021.06 | 15666.89 |

Checkout status mix: `HTTP 202=88, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 5703 | 0 | 10 | 16.74 | 54 | 9239760 | 10/0/10 | 263 | 21.611004569s | 7.28% / 14.99% | 23956472 / 25456640 |
| order | 0 | 0 | 0 | 0.00 | 36 | 2677144 | 7/0/7 | 0 | 0s | 2.53% / 8.22% | 11626864 / 12460032 |
| inventory | 90 | 0 | 10 | 33.86 | 29 | 5952392 | 10/0/10 | 0 | 0s | 1.40% / 5.75% | 16745891 / 22282240 |
| payment | 264 | 0 | 4 | 243.82 | 44 | 7215272 | 10/0/10 | 5 | 83.282697ms | 3.36% / 10.39% | 14075182 / 17440768 |

### PostgreSQL metrics

Samples: 140. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 1.

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
