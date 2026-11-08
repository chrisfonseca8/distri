# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-01T20:01:46Z`

## C3 client sweep

| Clients | Offered/s | Accepted/s | Completed/s | End-to-end p99 ms | Max in-flight | 5xx | Transport errors |
|---:|---:|---:|---:|---:|---:|---:|---:|
| 1 | 0.17 | 0.17 | 0.17 | 8093.94 | 1 | 0 | 0 |
| 2 | 0.30 | 0.30 | 0.30 | 11598.17 | 2 | 0 | 0 |
| 5 | 0.85 | 0.85 | 0.85 | 18086.00 | 5 | 0 | 0 |
| 10 | 1.63 | 1.63 | 1.63 | 12097.52 | 10 | 0 | 0 |
| 20 | 3.32 | 3.32 | 3.32 | 13773.34 | 20 | 0 | 0 |
| 50 | 7.85 | 7.85 | 7.85 | 13549.29 | 50 | 0 | 0 |
| 100 | 15.78 | 15.78 | 15.78 | 13892.64 | 100 | 0 | 0 |
| 200 | 25.12 | 25.12 | 25.12 | 19083.09 | 200 | 0 | 0 |

No knee/saturation candidate was detected in the recorded steps.

## c3-clients-01

**What it does:** 1 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 10 offered, 10 accepted, 10 completed (10 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test6-clients

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 0.17/s |
| Accepted rate | 0.17/s |
| Completed rate | 0.17/s |
| Mean/max in-flight requests | 0.05 / 1 |
| Mean/max in-flight workflows | 1.05 / 1 |
| Little's law observed / expected | 1.05 / 1.05 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 0.7% |
| Load generator memory at end | 8.2 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 10 | 31.64 | 36.98 | 36.98 | 36.98 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 10 | 7501.04 | 8093.94 | 8093.94 | 8093.94 |
| Server saga completion | 10 | 7401.84 | 8030.79 | 8030.79 | 8030.79 |

Checkout status mix: `HTTP 202=10, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 605 | 0 | 1 | 5.26 | 43 | 5576112 | 5/0/5 | 0 | 0s | 2.63% / 7.67% | 21452669 / 21946368 |
| order | 0 | 0 | 0 | 0.00 | 37 | 6879288 | 4/0/4 | 0 | 0s | 2.11% / 6.32% | 11962172 / 16461824 |
| inventory | 11 | 0 | 1 | 15.61 | 30 | 5946824 | 2/0/2 | 0 | 0s | 0.72% / 5.30% | 17108341 / 22384640 |
| payment | 30 | 0 | 3 | 9.05 | 43 | 6126128 | 6/0/6 | 0 | 0s | 2.59% / 7.13% | 14252259 / 18382848 |

### PostgreSQL metrics

Samples: 127. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

## c3-clients-02

**What it does:** 2 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 18 offered, 18 accepted, 18 completed (18 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test6-clients

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 0.30/s |
| Accepted rate | 0.30/s |
| Completed rate | 0.30/s |
| Mean/max in-flight requests | 0.10 / 2 |
| Mean/max in-flight workflows | 2.16 / 2 |
| Little's law observed / expected | 2.16 / 2.16 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 0.9% |
| Load generator memory at end | 8.1 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 18 | 38.22 | 46.76 | 53.30 | 53.30 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 18 | 7155.27 | 10607.29 | 11598.17 | 11598.17 |
| Server saga completion | 18 | 7020.73 | 10535.58 | 11472.89 | 11472.89 |

Checkout status mix: `HTTP 202=18, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 1246 | 0 | 2 | 4.51 | 44 | 4921008 | 6/1/5 | 0 | 0s | 3.41% / 8.76% | 20359388 / 21155840 |
| order | 0 | 0 | 0 | 0.00 | 37 | 5382832 | 4/0/4 | 0 | 0s | 2.15% / 6.89% | 11896359 / 13201408 |
| inventory | 19 | 0 | 2 | 22.86 | 30 | 7933912 | 2/0/2 | 0 | 0s | 0.83% / 5.24% | 16924924 / 17428480 |
| payment | 54 | 0 | 3 | 7.83 | 43 | 6074512 | 6/0/6 | 0 | 0s | 2.54% / 7.57% | 14239239 / 15486976 |

### PostgreSQL metrics

Samples: 131. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

## c3-clients-05

**What it does:** 5 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 51 offered, 51 accepted, 51 completed (51 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test6-clients

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 0.85/s |
| Accepted rate | 0.85/s |
| Completed rate | 0.85/s |
| Mean/max in-flight requests | 0.26 / 5 |
| Mean/max in-flight workflows | 5.39 / 5 |
| Little's law observed / expected | 5.39 / 5.39 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 1.5% |
| Load generator memory at end | 8.5 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 51 | 38.51 | 45.65 | 58.92 | 58.92 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 51 | 6024.65 | 10571.23 | 18086.00 | 18086.00 |
| Server saga completion | 51 | 5946.28 | 10526.45 | 17999.94 | 17999.94 |

Checkout status mix: `HTTP 202=51, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 3109 | 0 | 5 | 4.94 | 49 | 10874288 | 8/0/8 | 0 | 0s | 5.47% / 9.96% | 20604912 / 25411584 |
| order | 0 | 0 | 0 | 0.00 | 37 | 4310728 | 5/0/5 | 0 | 0s | 2.42% / 8.45% | 11564160 / 16703488 |
| inventory | 52 | 0 | 5 | 20.60 | 31 | 7545216 | 5/0/5 | 0 | 0s | 1.11% / 6.81% | 17193733 / 22224896 |
| payment | 153 | 0 | 3 | 18.15 | 45 | 5968096 | 7/1/6 | 0 | 0s | 3.01% / 9.69% | 13912503 / 14733312 |

### PostgreSQL metrics

Samples: 136. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

## c3-clients-10

**What it does:** 10 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 98 offered, 98 accepted, 98 completed (98 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test6-clients

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 1.63/s |
| Accepted rate | 1.63/s |
| Completed rate | 1.63/s |
| Mean/max in-flight requests | 0.69 / 10 |
| Mean/max in-flight workflows | 10.81 / 10 |
| Little's law observed / expected | 10.81 / 10.81 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 2.0% |
| Load generator memory at end | 8.7 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 98 | 38.51 | 59.89 | 91.34 | 97.25 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 98 | 6114.24 | 10696.14 | 12097.52 | 14190.72 |
| Server saga completion | 98 | 6045.25 | 10618.36 | 12059.26 | 14142.73 |

Checkout status mix: `HTTP 202=98, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 6136 | 0 | 10 | 14.74 | 54 | 6741136 | 10/0/10 | 207 | 3.631621773s | 7.52% / 13.61% | 21295489 / 22609920 |
| order | 0 | 0 | 0 | 0.00 | 37 | 3552024 | 5/0/5 | 0 | 0s | 2.62% / 7.25% | 10938338 / 11911168 |
| inventory | 100 | 0 | 10 | 43.29 | 31 | 6652016 | 10/0/10 | 0 | 0s | 1.57% / 6.48% | 16539232 / 21626880 |
| payment | 294 | 0 | 3 | 57.02 | 45 | 4503616 | 8/0/8 | 0 | 0s | 3.61% / 10.08% | 13303065 / 14565376 |

### PostgreSQL metrics

Samples: 139. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

## c3-clients-100

**What it does:** 100 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 947 offered, 947 accepted, 947 completed (947 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test6-clients

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 15.78/s |
| Accepted rate | 15.78/s |
| Completed rate | 15.78/s |
| Mean/max in-flight requests | 5.18 / 100 |
| Mean/max in-flight workflows | 105.53 / 100 |
| Little's law observed / expected | 105.53 / 105.53 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 8.6% |
| Load generator memory at end | 10.6 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 947 | 34.83 | 294.28 | 340.67 | 352.05 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 947 | 6356.87 | 11202.33 | 13892.64 | 17681.78 |
| Server saga completion | 947 | 6285.37 | 11129.05 | 13815.39 | 17600.57 |

Checkout status mix: `HTTP 202=947, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 60910 | 0 | 100 | 10.03 | 110 | 9849048 | 10/0/10 | 23567 | 1m4.313741343s | 40.80% / 59.10% | 25473934 / 29556736 |
| order | 0 | 0 | 0 | 0.00 | 37 | 5586368 | 5/0/5 | 0 | 0s | 7.20% / 18.92% | 11533795 / 14372864 |
| inventory | 948 | 0 | 50 | 75.80 | 31 | 5582352 | 10/0/10 | 89 | 5.70376257s | 6.75% / 27.31% | 15979918 / 21286912 |
| payment | 2841 | 0 | 4 | 9.74 | 46 | 6143840 | 10/0/10 | 0 | 0s | 11.80% / 22.87% | 14821603 / 17104896 |

### PostgreSQL metrics

Samples: 145. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

## c3-clients-20

**What it does:** 20 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 199 offered, 199 accepted, 199 completed (199 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test6-clients

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 3.32/s |
| Accepted rate | 3.32/s |
| Completed rate | 3.32/s |
| Mean/max in-flight requests | 1.06 / 20 |
| Mean/max in-flight workflows | 21.50 / 20 |
| Little's law observed / expected | 21.50 / 21.50 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 3.1% |
| Load generator memory at end | 8.8 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 199 | 31.99 | 82.07 | 93.17 | 298.27 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 199 | 6068.22 | 9875.33 | 13773.34 | 14322.65 |
| Server saga completion | 199 | 5982.13 | 9743.39 | 13744.18 | 14203.38 |

Checkout status mix: `HTTP 202=199, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 12398 | 0 | 20 | 7.38 | 64 | 9745936 | 10/0/10 | 1055 | 5.049719211s | 11.91% / 20.71% | 21767945 / 26214400 |
| order | 0 | 0 | 0 | 0.00 | 37 | 3400760 | 5/0/5 | 0 | 0s | 3.31% / 8.79% | 11379065 / 12558336 |
| inventory | 200 | 0 | 19 | 43.43 | 31 | 6082696 | 10/0/10 | 10 | 213.720499ms | 2.17% / 7.33% | 15667112 / 17551360 |
| payment | 597 | 0 | 4 | 35.83 | 47 | 6015800 | 9/0/9 | 0 | 0s | 4.75% / 11.72% | 14435988 / 18956288 |

### PostgreSQL metrics

Samples: 142. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

## c3-clients-200

**What it does:** 200 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 1507 offered, 1507 accepted, 1507 completed (1507 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test6-clients

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 25.12/s |
| Accepted rate | 25.12/s |
| Completed rate | 25.12/s |
| Mean/max in-flight requests | 48.11 / 200 |
| Mean/max in-flight workflows | 211.78 / 200 |
| Little's law observed / expected | 211.78 / 211.78 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 12.4% |
| Load generator memory at end | 16.8 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 1507 | 53.98 | 415.87 | 527.29 | 927.51 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 1507 | 7473.62 | 15715.34 | 19083.09 | 24078.16 |
| Server saga completion | 1507 | 7328.32 | 15559.39 | 18835.28 | 23863.33 |

Checkout status mix: `HTTP 202=1507, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 99340 | 0 | 200 | 125.39 | 256 | 17629312 | 10/0/10 | 155359 | 41m39.379138538s | 62.53% / 101.38% | 31678208 / 36253696 |
| order | 0 | 0 | 0 | 0.00 | 41 | 3611576 | 6/0/6 | 0 | 0s | 9.63% / 29.52% | 11770282 / 14229504 |
| inventory | 1508 | 0 | 84 | 88.65 | 31 | 6691856 | 10/0/10 | 176 | 11.254416699s | 10.27% / 47.47% | 15580103 / 18788352 |
| payment | 4521 | 0 | 4 | 29.60 | 46 | 7869176 | 10/0/10 | 10 | 205.81084ms | 16.04% / 31.76% | 14936888 / 17133568 |

### PostgreSQL metrics

Samples: 145. Connections mean/max: 1.01/2. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

## c3-clients-50

**What it does:** 50 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 471 offered, 471 accepted, 471 completed (471 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test6-clients

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 7.85/s |
| Accepted rate | 7.85/s |
| Completed rate | 7.85/s |
| Mean/max in-flight requests | 2.45 / 50 |
| Mean/max in-flight workflows | 52.66 / 50 |
| Little's law observed / expected | 52.66 / 52.66 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 5.4% |
| Load generator memory at end | 9.9 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 471 | 35.06 | 193.02 | 211.04 | 220.72 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 471 | 6290.59 | 10546.97 | 13549.29 | 14587.67 |
| Server saga completion | 471 | 6216.52 | 10423.90 | 13457.17 | 14504.02 |

Checkout status mix: `HTTP 202=471, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 30462 | 0 | 50 | 7.68 | 91 | 9597608 | 10/0/10 | 5564 | 15.12075483s | 23.52% / 41.46% | 23160497 / 27381760 |
| order | 0 | 0 | 0 | 0.00 | 37 | 4861336 | 5/0/5 | 0 | 0s | 4.86% / 12.27% | 11304524 / 12828672 |
| inventory | 473 | 0 | 38 | 65.89 | 31 | 7782248 | 10/0/10 | 40 | 1.938595039s | 3.82% / 12.37% | 15931028 / 20312064 |
| payment | 1413 | 0 | 4 | 10.12 | 46 | 5166912 | 9/0/9 | 0 | 0s | 7.79% / 17.36% | 14420708 / 19816448 |

### PostgreSQL metrics

Samples: 142. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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
