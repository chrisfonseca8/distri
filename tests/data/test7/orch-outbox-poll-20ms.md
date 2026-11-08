# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-01T21:02:42Z`

## C2 run-to-run variance (3 runs recorded)

| Metric | Mean | Min | Max | Std dev | CV |
|---|---:|---:|---:|---:|---:|
| Accepted/s | 43.01 | 40.35 | 45.03 | 1.96 | 4.57% |
| Completed/s | 43.01 | 40.35 | 45.03 | 1.96 | 4.57% |
| Checkout p95 ms | 1044.07 | 1033.50 | 1049.87 | 7.48 | 0.72% |
| Pay p95 ms | 0.00 | 0.00 | 0.00 | 0.00 | 0.00% |
| End-to-end p95 ms | 18151.44 | 17519.78 | 19207.20 | 751.33 | 4.14% |

## c2-baseline-1

**What it does:** 500 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 2421 offered, 2421 accepted, 2421 completed (2421 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test7-orch-outbox-poll-20ms

**Completion observation:** `sse`.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 40.35/s |
| Accepted rate | 40.35/s |
| Completed rate | 40.35/s |
| Mean/max in-flight requests | 541.49 / 500 |
| Mean/max in-flight workflows | 541.49 / 500 |
| Little's law observed / expected | 541.49 / 541.49 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 1.9% |
| Load generator memory at end | 28.5 MB |
| SSE attempts / terminal events / polling fallbacks | 2421 / 2421 / 0 |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 2421 | 36.14 | 1033.50 | 1112.88 | 1155.50 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 2421 | 13237.35 | 19207.20 | 21639.41 | 28788.87 |
| Server saga completion | 2421 | 13149.18 | 18658.53 | 21328.50 | 28763.29 |

Checkout status mix: `HTTP 202=2421, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 4842 | 0 | 501 | 16980.26 | 553 | 13581336 | 10/0/10 | 4146 | 6m24.603183427s | 28.58% / 87.20% | 50382519 / 55971840 |
| order | 0 | 0 | 0 | 0.00 | 40 | 4646032 | 8/0/8 | 0 | 0s | 11.81% / 41.08% | 12826244 / 16146432 |
| inventory | 2422 | 0 | 160 | 225.44 | 31 | 6568992 | 10/0/10 | 455 | 1m6.581819203s | 13.09% / 87.85% | 16315606 / 24875008 |
| payment | 7263 | 0 | 4 | 11.31 | 46 | 4945304 | 9/0/9 | 189 | 230.426644ms | 19.94% / 58.26% | 14772628 / 16039936 |

### PostgreSQL metrics

Samples: 163. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

**Result:** PASS — 2702 offered, 2702 accepted, 2702 completed (2702 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test7-orch-outbox-poll-20ms

**Completion observation:** `sse`.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 45.03/s |
| Accepted rate | 45.03/s |
| Completed rate | 45.03/s |
| Mean/max in-flight requests | 564.07 / 500 |
| Mean/max in-flight workflows | 564.08 / 500 |
| Little's law observed / expected | 564.08 / 564.08 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 2.2% |
| Load generator memory at end | 41.5 MB |
| SSE attempts / terminal events / polling fallbacks | 2702 / 2702 / 0 |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 2702 | 38.29 | 1048.83 | 1155.41 | 1226.77 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 2702 | 12318.13 | 17519.78 | 20564.40 | 29925.07 |
| Server saga completion | 2702 | 12144.72 | 17438.49 | 20455.80 | 29187.38 |

Checkout status mix: `HTTP 202=2702, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 5404 | 0 | 501 | 16142.22 | 559 | 11886808 | 10/0/10 | 10537 | 7m16.068322192s | 32.61% / 82.69% | 58713911 / 79040512 |
| order | 0 | 0 | 0 | 0.00 | 44 | 3027368 | 8/0/8 | 0 | 0s | 13.75% / 43.75% | 12833571 / 15372288 |
| inventory | 2704 | 0 | 178 | 211.25 | 30 | 5096736 | 10/0/10 | 451 | 1m8.508819727s | 16.03% / 86.32% | 16128843 / 26292224 |
| payment | 8106 | 0 | 4 | 8.94 | 47 | 5881480 | 10/1/9 | 289 | 309.282801ms | 23.01% / 42.47% | 14998588 / 16904192 |

### PostgreSQL metrics

Samples: 154. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

**Result:** PASS — 2619 offered, 2619 accepted, 2619 completed (2619 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test7-orch-outbox-poll-20ms

**Completion observation:** `sse`.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 43.65/s |
| Accepted rate | 43.65/s |
| Completed rate | 43.65/s |
| Mean/max in-flight requests | 552.45 / 500 |
| Mean/max in-flight workflows | 552.45 / 500 |
| Little's law observed / expected | 552.45 / 552.45 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 2.2% |
| Load generator memory at end | 44.1 MB |
| SSE attempts / terminal events / polling fallbacks | 2619 / 2619 / 0 |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 2619 | 38.94 | 1049.87 | 1149.41 | 1189.58 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 2619 | 12451.35 | 17727.33 | 21092.24 | 28944.54 |
| Server saga completion | 2619 | 12354.87 | 17364.02 | 20633.14 | 28166.40 |

Checkout status mix: `HTTP 202=2619, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 5238 | 0 | 500 | 15966.81 | 549 | 11762312 | 10/0/10 | 8404 | 6m53.524088055s | 32.50% / 85.25% | 59035952 / 76955648 |
| order | 0 | 0 | 0 | 0.00 | 40 | 4088776 | 8/0/8 | 0 | 0s | 14.14% / 35.37% | 12634361 / 14893056 |
| inventory | 2620 | 0 | 151 | 209.33 | 31 | 6494528 | 10/0/10 | 459 | 1m8.481084965s | 16.62% / 87.72% | 16206322 / 23547904 |
| payment | 7857 | 0 | 4 | 9.06 | 47 | 5884688 | 10/0/10 | 287 | 381.72025ms | 25.03% / 56.58% | 14932493 / 17129472 |

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
