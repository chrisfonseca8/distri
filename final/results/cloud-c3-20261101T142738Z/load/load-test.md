# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-01T14:36:58Z`

## C3 client sweep

| Clients | Offered/s | Accepted/s | Completed/s | End-to-end p99 ms | Max in-flight | 5xx | Transport errors |
|---:|---:|---:|---:|---:|---:|---:|---:|
| 1 | 0.17 | 0.17 | 0.17 | 11153.57 | 1 | 0 | 0 |
| 2 | 0.33 | 0.33 | 0.33 | 10589.63 | 2 | 0 | 0 |
| 5 | 0.83 | 0.83 | 0.83 | 11732.42 | 5 | 0 | 0 |
| 10 | 1.52 | 1.52 | 1.52 | 12159.56 | 10 | 0 | 0 |
| 20 | 3.25 | 3.25 | 3.25 | 12865.16 | 20 | 0 | 0 |
| 50 | 8.03 | 8.03 | 8.03 | 14069.81 | 50 | 0 | 0 |

No knee/saturation candidate was detected in the recorded steps.

## c3-clients-01

**What it does:** 1 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 10 offered, 10 accepted, 10 completed (10 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-staging

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 0.17/s |
| Accepted rate | 0.17/s |
| Completed rate | 0.17/s |
| Mean/max in-flight requests | 0.05 / 1 |
| Mean/max in-flight workflows | 1.06 / 1 |
| Little's law observed / expected | 1.06 / 1.06 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 0.7% |
| Load generator memory at end | 8.5 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 10 | 37.86 | 45.99 | 45.99 | 45.99 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 10 | 5590.64 | 11153.57 | 11153.57 | 11153.57 |
| Server saga completion | 10 | 5502.66 | 11021.80 | 11021.80 | 11021.80 |

Checkout status mix: `HTTP 202=10, transport=0, decode=0, 5xx=0`  
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 609 | 0 | 1 | 4.87 | 41 | 6165752 | 6/0/6 | 0 | 0s | 2.69% / 6.90% | 12982610 / 14057472 |
| order | 0 | 0 | 0 | 0.00 | 34 | 4238624 | 5/0/5 | 0 | 0s | 2.10% / 7.24% | 11298283 / 17182720 |
| inventory | 11 | 0 | 1 | 20.09 | 29 | 5176968 | 9/0/9 | 0 | 0s | 0.76% / 5.89% | 14864900 / 19693568 |
| payment | 30 | 0 | 3 | 8.64 | 41 | 4051912 | 6/0/6 | 0 | 0s | 2.41% / 7.88% | 12664025 / 13778944 |

### PostgreSQL metrics

Samples: 128. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

**Result:** PASS — 20 offered, 20 accepted, 20 completed (20 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-staging

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 0.33/s |
| Accepted rate | 0.33/s |
| Completed rate | 0.33/s |
| Mean/max in-flight requests | 0.10 / 2 |
| Mean/max in-flight workflows | 2.11 / 2 |
| Little's law observed / expected | 2.11 / 2.11 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 0.9% |
| Load generator memory at end | 8.1 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 20 | 38.80 | 46.63 | 49.66 | 49.66 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 20 | 6461.88 | 9131.68 | 10589.63 | 10589.63 |
| Server saga completion | 20 | 6408.83 | 9087.80 | 10526.68 | 10526.68 |

Checkout status mix: `HTTP 202=20, transport=0, decode=0, 5xx=0`  
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 1217 | 0 | 2 | 5.58 | 44 | 6103288 | 6/0/6 | 0 | 0s | 3.45% / 9.01% | 13090692 / 18145280 |
| order | 0 | 0 | 0 | 0.00 | 34 | 4721440 | 5/0/5 | 0 | 0s | 2.18% / 6.94% | 11056366 / 16998400 |
| inventory | 21 | 0 | 2 | 23.47 | 30 | 7619704 | 9/0/9 | 0 | 0s | 0.83% / 4.85% | 14579203 / 16556032 |
| payment | 60 | 0 | 3 | 16.96 | 41 | 4055864 | 7/0/7 | 0 | 0s | 2.54% / 6.96% | 12914349 / 16908288 |

### PostgreSQL metrics

Samples: 134. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

**Result:** PASS — 50 offered, 50 accepted, 50 completed (50 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-staging

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 0.83/s |
| Accepted rate | 0.83/s |
| Completed rate | 0.83/s |
| Mean/max in-flight requests | 0.23 / 5 |
| Mean/max in-flight workflows | 5.24 / 5 |
| Little's law observed / expected | 5.24 / 5.24 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 1.5% |
| Load generator memory at end | 8.8 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 50 | 36.40 | 40.69 | 43.40 | 43.40 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 50 | 6073.55 | 10111.25 | 11732.42 | 11732.42 |
| Server saga completion | 50 | 5990.24 | 10071.76 | 11606.32 | 11606.32 |

Checkout status mix: `HTTP 202=50, transport=0, decode=0, 5xx=0`  
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 3040 | 0 | 5 | 4.50 | 46 | 5163416 | 7/0/7 | 0 | 0s | 5.38% / 10.25% | 13242306 / 16404480 |
| order | 0 | 0 | 0 | 0.00 | 34 | 5118872 | 5/0/5 | 0 | 0s | 2.53% / 7.02% | 12069710 / 13045760 |
| inventory | 51 | 0 | 5 | 17.78 | 30 | 5772144 | 9/0/9 | 0 | 0s | 1.13% / 5.70% | 12918260 / 14467072 |
| payment | 150 | 0 | 3 | 8.19 | 43 | 5587880 | 6/0/6 | 0 | 0s | 3.08% / 8.02% | 12151107 / 13512704 |

### PostgreSQL metrics

Samples: 134. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

**Result:** PASS — 91 offered, 91 accepted, 91 completed (91 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-staging

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 1.52/s |
| Accepted rate | 1.52/s |
| Completed rate | 1.52/s |
| Mean/max in-flight requests | 0.44 / 10 |
| Mean/max in-flight workflows | 10.43 / 10 |
| Little's law observed / expected | 10.43 / 10.43 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 2.3% |
| Load generator memory at end | 8.9 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 91 | 36.01 | 77.52 | 86.33 | 115.80 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 91 | 6613.11 | 10613.78 | 12159.56 | 15266.79 |
| Server saga completion | 91 | 6526.59 | 10560.85 | 12043.61 | 15169.77 |

Checkout status mix: `HTTP 202=91, transport=0, decode=0, 5xx=0`  
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 6051 | 0 | 10 | 4.49 | 50 | 4141392 | 10/0/10 | 0 | 0s | 7.94% / 14.43% | 13773472 / 15437824 |
| order | 0 | 0 | 0 | 0.00 | 34 | 5232696 | 5/0/5 | 0 | 0s | 2.62% / 7.44% | 11293516 / 15351808 |
| inventory | 92 | 0 | 10 | 32.31 | 30 | 6424928 | 9/0/9 | 0 | 0s | 1.45% / 6.28% | 13163324 / 15085568 |
| payment | 273 | 0 | 4 | 9.90 | 44 | 4267080 | 7/0/7 | 0 | 0s | 3.59% / 9.05% | 12118500 / 16326656 |

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

## c3-clients-20

**What it does:** 20 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 195 offered, 195 accepted, 195 completed (195 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-staging

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 3.25/s |
| Accepted rate | 3.25/s |
| Completed rate | 3.25/s |
| Mean/max in-flight requests | 0.76 / 20 |
| Mean/max in-flight workflows | 21.20 / 20 |
| Little's law observed / expected | 21.20 / 21.20 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 3.4% |
| Load generator memory at end | 9.2 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 195 | 33.25 | 111.15 | 169.32 | 176.55 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 195 | 6152.56 | 10706.67 | 12865.16 | 15902.27 |
| Server saga completion | 195 | 6069.75 | 10671.15 | 12751.62 | 15829.53 |

Checkout status mix: `HTTP 202=195, transport=0, decode=0, 5xx=0`  
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 12397 | 0 | 20 | 4.68 | 63 | 5515760 | 10/0/10 | 157 | 928.491068ms | 12.48% / 23.80% | 14284861 / 19369984 |
| order | 0 | 0 | 0 | 0.00 | 34 | 5083736 | 5/0/5 | 0 | 0s | 3.20% / 8.84% | 11375936 / 13606912 |
| inventory | 196 | 0 | 16 | 39.92 | 30 | 6070288 | 7/0/7 | 6 | 142.760564ms | 2.39% / 11.86% | 12390613 / 16244736 |
| payment | 585 | 0 | 3 | 8.45 | 43 | 6573088 | 7/0/7 | 0 | 0s | 4.78% / 12.74% | 12981018 / 18354176 |

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

## c3-clients-50

**What it does:** 50 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 482 offered, 482 accepted, 482 completed (482 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-staging

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 8.03/s |
| Accepted rate | 8.03/s |
| Completed rate | 8.03/s |
| Mean/max in-flight requests | 1.76 / 50 |
| Mean/max in-flight workflows | 52.84 / 50 |
| Little's law observed / expected | 52.84 / 52.84 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 5.6% |
| Load generator memory at end | 9.8 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 482 | 30.58 | 153.41 | 178.41 | 199.44 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 482 | 5904.49 | 10942.56 | 14069.81 | 17146.60 |
| Server saga completion | 482 | 5745.39 | 10867.69 | 13985.85 | 17115.65 |

Checkout status mix: `HTTP 202=482, transport=0, decode=0, 5xx=0`  
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 30994 | 0 | 50 | 4.77 | 92 | 9433864 | 10/0/10 | 4335 | 7.65020051s | 23.03% / 38.73% | 16540145 / 21610496 |
| order | 0 | 0 | 0 | 0.00 | 34 | 5508176 | 5/0/5 | 0 | 0s | 4.76% / 11.85% | 11326025 / 16842752 |
| inventory | 483 | 0 | 38 | 59.85 | 29 | 5615304 | 10/0/10 | 40 | 1.608093988s | 4.13% / 14.16% | 13488040 / 16285696 |
| payment | 1446 | 0 | 4 | 7.92 | 44 | 5400184 | 8/0/8 | 0 | 0s | 7.39% / 18.12% | 13003220 / 14303232 |

### PostgreSQL metrics

Samples: 141. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

