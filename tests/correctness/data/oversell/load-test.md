# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-01T19:18:59Z`

## C5–C7 contention wave

| Case | Offered | Completed/s | Checkout 409 | Checkout p99 ms | End-to-end p99 ms | Lock waiters max | Deadlocks | Order DB waits / duration ms |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| c5a-hot-product-throughput | 2184 | 33.88 | 0 | 3214.26 | 24204.05 | 0 | 0 | 0 / 0.00 |
| c5b-hot-product-sellout | 500 | 0.77 | 490 | 551.93 | 13002.29 | 0 | 0 | 0 / 0.00 |

## c5a-hot-product-throughput

**What it does:** 500 closed-loop clients buy one high-stock product for 1m0s to measure single-row contention

**Result:** FAIL — 2184 offered, 2033 accepted, 2033 completed (2033 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test4-oversell

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 36.40/s |
| Accepted rate | 33.88/s |
| Completed rate | 33.88/s |
| Mean/max in-flight requests | 219.58 / 500 |
| Mean/max in-flight workflows | 548.93 / 500 |
| Little's law observed / expected | 548.93 / 541.07 (1.45% difference) |
| Load generator CPU (percent of one core, measured window) | 19.9% |
| Load generator memory at end | 25.8 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 2184 | 149.96 | 3076.93 | 3214.26 | 3346.01 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 2033 | 16519.33 | 21552.86 | 24204.05 | 31248.95 |
| Server saga completion | 2033 | 16152.67 | 21260.99 | 23845.57 | 30737.54 |

Checkout status mix: `HTTP 202=2033, HTTP 503=151, transport=0, decode=0, 5xx=151`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 199235 | 151 | 500 | 159.79 | 484 | 18661704 | 10/0/10 | 397580 | 3h18m54.913439385s | 94.84% / 137.78% | 48236397 / 73621504 |
| order | 0 | 0 | 0 | 0.00 | 37 | 6072928 | 8/0/8 | 0 | 0s | 10.70% / 22.97% | 11880960 / 15392768 |
| inventory | 2185 | 151 | 468 | 2999.57 | 29 | 6161816 | 10/0/10 | 661 | 16m46.60263696s | 11.56% / 39.99% | 17307623 / 38645760 |
| payment | 6099 | 0 | 4 | 9.23 | 44 | 7684144 | 10/0/10 | 5 | 6.004397ms | 17.55% / 39.08% | 14232966 / 17199104 |

### PostgreSQL metrics

Samples: 169. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 9. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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
- FAIL — one unique completed checkout attempt per offered workflow (violations: 151)

### Telemetry limitations

- PostgreSQL container: Get "http://docker/containers/json?filters=%7B%22label%22%3A%5B%22com.docker.compose.service%3Dpostgres%22%2C%22com.docker.compose.oneoff%3DFalse%22%5D%7D": dial unix /var/run/docker.sock: connect: no such file or directory.

## c5b-hot-product-sellout

**What it does:** 500 clients submit 500 attempts against one product with 10 units, requiring exactly stock-many successes and 409 for the remainder

**Result:** PASS — 500 offered, 10 accepted, 10 completed (10 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 13.002897363s.

**Deployment:** cloud-test4-oversell

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 38.45/s |
| Accepted rate | 0.77/s |
| Completed rate | 0.77/s |
| Mean/max in-flight requests | 17.18 / 500 |
| Mean/max in-flight workflows | 22.33 / 500 |
| Little's law observed / expected | 22.33 / 5.67 (293.83% difference) |
| Load generator CPU (percent of one core, measured window) | 2.0% |
| Load generator memory at end | 40.0 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 500 | 465.17 | 546.11 | 551.93 | 575.98 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 10 | 7424.14 | 13002.29 | 13002.29 | 13002.29 |
| Server saga completion | 10 | 7168.07 | 12526.75 | 12526.75 | 12526.75 |

Checkout status mix: `HTTP 202=10, HTTP 409=490, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 1165 | 0 | 500 | 534.87 | 632 | 22476632 | 10/0/10 | 1538 | 2m53.250067017s | 8.94% / 81.82% | 55798705 / 72708096 |
| order | 0 | 0 | 0 | 0.00 | 45 | 4349632 | 8/0/8 | 0 | 0s | 2.55% / 5.88% | 12944620 / 14286848 |
| inventory | 500 | 0 | 171 | 230.27 | 29 | 6593376 | 10/0/10 | 452 | 37.574048106s | 4.24% / 77.29% | 18797331 / 19636224 |
| payment | 30 | 0 | 4 | 7.63 | 44 | 6007352 | 10/0/10 | 0 | 0s | 3.19% / 12.95% | 14557971 / 15237120 |

### PostgreSQL metrics

Samples: 27. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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
- PASS — C5b consumes exactly available stock without oversell and rejects the remainder with 409 (violations: 0)

### Telemetry limitations

- PostgreSQL container: Get "http://docker/containers/json?filters=%7B%22label%22%3A%5B%22com.docker.compose.service%3Dpostgres%22%2C%22com.docker.compose.oneoff%3DFalse%22%5D%7D": dial unix /var/run/docker.sock: connect: no such file or directory.
