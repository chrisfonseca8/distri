# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-01T20:10:55Z`

## C3 client sweep

| Clients | Offered/s | Accepted/s | Completed/s | End-to-end p99 ms | Max in-flight | 5xx | Transport errors |
|---:|---:|---:|---:|---:|---:|---:|---:|
| 200 | 31.15 | 31.15 | 31.15 | 14343.75 | 200 | 0 | 0 |
| 300 | 36.93 | 36.93 | 36.93 | 16381.35 | 300 | 0 | 0 |
| 400 | 31.13 | 31.13 | 31.13 | 23141.85 | 400 | 0 | 0 |
| 600 | 20.88 | 20.88 | 20.88 | 45272.67 | 600 | 0 | 0 |

Automatic knee candidate: **300 clients** (first following step with less than 10% additional completed throughput).

## c3-clients-200

**What it does:** 200 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 1869 offered, 1869 accepted, 1869 completed (1869 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test6-clients

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 31.15/s |
| Accepted rate | 31.15/s |
| Completed rate | 31.15/s |
| Mean/max in-flight requests | 19.68 / 200 |
| Mean/max in-flight workflows | 212.80 / 200 |
| Little's law observed / expected | 212.80 / 212.80 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 13.9% |
| Load generator memory at end | 14.4 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 1869 | 43.93 | 317.59 | 396.95 | 458.64 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 1869 | 6391.07 | 10991.48 | 14343.75 | 21872.66 |
| Server saga completion | 1869 | 6297.81 | 10888.91 | 14200.31 | 21739.88 |

Checkout status mix: `HTTP 202=1869, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 117331 | 0 | 200 | 35.00 | 249 | 11674136 | 10/0/10 | 152178 | 12m51.990913396s | 72.73% / 102.25% | 30518355 / 35184640 |
| order | 0 | 0 | 0 | 0.00 | 41 | 4794720 | 6/0/6 | 0 | 0s | 10.98% / 27.86% | 11774339 / 13705216 |
| inventory | 1871 | 0 | 60 | 46.99 | 32 | 6117856 | 10/0/10 | 169 | 6.84047344s | 11.07% / 44.04% | 16303353 / 19361792 |
| payment | 5607 | 0 | 4 | 8.92 | 50 | 5135792 | 10/0/10 | 50 | 32.181486ms | 18.21% / 35.15% | 14629555 / 16736256 |

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

## c3-clients-300

**What it does:** 300 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 2216 offered, 2216 accepted, 2216 completed (2216 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test6-clients

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 36.93/s |
| Accepted rate | 36.93/s |
| Completed rate | 36.93/s |
| Mean/max in-flight requests | 80.99 / 300 |
| Mean/max in-flight workflows | 318.13 / 300 |
| Little's law observed / expected | 318.13 / 318.13 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 16.7% |
| Load generator memory at end | 18.3 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 2216 | 87.06 | 552.71 | 643.25 | 745.01 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 2216 | 8124.34 | 13704.32 | 16381.35 | 24431.35 |
| Server saga completion | 2216 | 7954.19 | 13413.24 | 15838.15 | 23854.04 |

Checkout status mix: `HTTP 202=2216, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 144031 | 0 | 300 | 90.34 | 297 | 19827552 | 10/0/10 | 294475 | 1h15m45.088421739s | 86.23% / 120.70% | 36933767 / 52920320 |
| order | 0 | 0 | 0 | 0.00 | 41 | 4275472 | 8/0/8 | 0 | 0s | 12.09% / 33.49% | 12479264 / 16142336 |
| inventory | 2217 | 0 | 106 | 107.86 | 32 | 8435216 | 10/0/10 | 278 | 26.050122807s | 14.83% / 70.16% | 15678619 / 20697088 |
| payment | 6648 | 0 | 4 | 8.94 | 46 | 5670104 | 10/1/9 | 33 | 18.489653ms | 19.65% / 38.35% | 14845341 / 19451904 |

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

## c3-clients-400

**What it does:** 400 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 1868 offered, 1868 accepted, 1868 completed (1868 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test6-clients

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 31.13/s |
| Accepted rate | 31.13/s |
| Completed rate | 31.13/s |
| Mean/max in-flight requests | 160.73 / 400 |
| Mean/max in-flight workflows | 430.24 / 400 |
| Little's law observed / expected | 430.24 / 430.24 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 17.6% |
| Load generator memory at end | 24.9 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 1868 | 140.09 | 842.75 | 962.22 | 1075.33 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 1868 | 13348.80 | 20075.97 | 23141.85 | 31269.42 |
| Server saga completion | 1868 | 13122.51 | 19553.20 | 22340.74 | 30670.66 |

Checkout status mix: `HTTP 202=1868, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 163068 | 0 | 400 | 149.29 | 372 | 19207808 | 10/0/10 | 332318 | 2h36m42.603956999s | 86.00% / 138.77% | 43922400 / 59707392 |
| order | 0 | 0 | 0 | 0.00 | 37 | 4191680 | 8/0/8 | 0 | 0s | 10.73% / 29.82% | 12122531 / 14413824 |
| inventory | 1869 | 0 | 142 | 167.27 | 36 | 6024616 | 10/0/10 | 361 | 39.52027296s | 11.81% / 80.07% | 16011721 / 22024192 |
| payment | 5604 | 0 | 4 | 8.57 | 50 | 5531544 | 10/0/10 | 6 | 4.009156ms | 16.76% / 34.62% | 14710364 / 16465920 |

### PostgreSQL metrics

Samples: 162. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

## c3-clients-600

**What it does:** 600 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 1253 offered, 1253 accepted, 1253 completed (1253 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test6-clients

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 20.88/s |
| Accepted rate | 20.88/s |
| Completed rate | 20.88/s |
| Mean/max in-flight requests | 345.43 / 600 |
| Mean/max in-flight workflows | 673.77 / 600 |
| Little's law observed / expected | 673.77 / 673.77 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 21.1% |
| Load generator memory at end | 34.3 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 1253 | 351.89 | 1321.09 | 1462.55 | 1660.56 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 1253 | 32830.12 | 43363.98 | 45272.67 | 47762.50 |
| Server saga completion | 1253 | 32464.00 | 42137.19 | 44014.79 | 46425.98 |

Checkout status mix: `HTTP 202=1253, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 197692 | 0 | 600 | 255.48 | 609 | 23903040 | 10/0/10 | 401606 | 5h40m49.800862725s | 97.66% / 131.86% | 56713292 / 80166912 |
| order | 0 | 0 | 0 | 0.00 | 37 | 3776216 | 8/1/7 | 0 | 0s | 8.16% / 22.46% | 11989785 / 16875520 |
| inventory | 1254 | 0 | 174 | 340.00 | 36 | 6481432 | 10/0/10 | 539 | 1m17.838112165s | 7.68% / 93.04% | 16891110 / 25587712 |
| payment | 3759 | 0 | 4 | 9.15 | 46 | 8077328 | 10/0/10 | 7 | 3.169946ms | 12.35% / 41.26% | 14573542 / 15847424 |

### PostgreSQL metrics

Samples: 161. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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
