# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-01T20:25:14Z`

## C4 rate sweep

| Offered/s | Accepted/s | Completed/s | End-to-end p99 ms | Max in-flight | 5xx | Transport errors |
|---:|---:|---:|---:|---:|---:|---:|
| 10.00 | 10.00 | 10.00 | 13801.17 | 79 | 0 | 0 |
| 20.00 | 20.00 | 20.00 | 13901.92 | 145 | 0 | 0 |
| 30.03 | 30.03 | 30.03 | 13676.80 | 261 | 0 | 0 |
| 40.00 | 40.00 | 40.00 | 51674.99 | 858 | 0 | 0 |
| 50.00 | 50.00 | 50.00 | 130610.38 | 1190 | 0 | 0 |
| 60.03 | 60.03 | 19.03 | 175878.71 | 1641 | 0 | 0 |

Automatic saturation candidate: **60 offered workflows/s** (completed rate below 95% of offered rate, or errors appeared).

## c4-rate-010

**What it does:** an open-loop scheduler offers 10 workflows/s for 30s across all 50 products without waiting for earlier workflows

**Result:** PASS — 300 offered, 300 accepted, 300 completed (300 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 30s.

**Deployment:** cloud-test6-rate

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 10.00/s |
| Accepted rate | 10.00/s |
| Completed rate | 10.00/s |
| Mean/max in-flight requests | 2.76 / 35 |
| Mean/max in-flight workflows | 65.22 / 79 |
| Little's law observed / expected | 65.22 / 65.22 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 5.7% |
| Load generator memory at end | 9.0 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 300 | 31.00 | 43.11 | 67.73 | 76.93 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 300 | 5968.04 | 11338.08 | 13801.17 | 15300.30 |
| Server saga completion | 300 | 5928.32 | 11258.67 | 13755.48 | 15236.06 |

Checkout status mix: `HTTP 202=300, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 18953 | 0 | 33 | 7.33 | 87 | 9430456 | 10/0/10 | 2868 | 4.901729783s | 24.63% / 39.10% | 23569798 / 28672000 |
| order | 0 | 0 | 0 | 0.00 | 38 | 5337184 | 8/0/8 | 0 | 0s | 4.96% / 11.53% | 12646448 / 15921152 |
| inventory | 301 | 0 | 2 | 19.88 | 32 | 7829616 | 10/0/10 | 0 | 0s | 4.78% / 10.81% | 16252294 / 20246528 |
| payment | 900 | 0 | 4 | 9.41 | 46 | 8128096 | 10/0/10 | 0 | 0s | 7.96% / 16.40% | 14459660 / 15929344 |

### PostgreSQL metrics

Samples: 85. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

### Telemetry limitations

- PostgreSQL container: Get "http://docker/containers/json?filters=%7B%22label%22%3A%5B%22com.docker.compose.service%3Dpostgres%22%2C%22com.docker.compose.oneoff%3DFalse%22%5D%7D": dial unix /var/run/docker.sock: connect: no such file or directory.

## c4-rate-020

**What it does:** an open-loop scheduler offers 20 workflows/s for 30s across all 50 products without waiting for earlier workflows

**Result:** PASS — 600 offered, 600 accepted, 600 completed (600 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 30s.

**Deployment:** cloud-test6-rate

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 20.00/s |
| Accepted rate | 20.00/s |
| Completed rate | 20.00/s |
| Mean/max in-flight requests | 6.01 / 69 |
| Mean/max in-flight workflows | 132.99 / 145 |
| Little's law observed / expected | 132.99 / 132.99 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 9.6% |
| Load generator memory at end | 10.1 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 600 | 34.25 | 52.40 | 73.77 | 107.31 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 600 | 6224.73 | 11307.68 | 13901.92 | 17157.56 |
| Server saga completion | 600 | 6156.55 | 11228.64 | 13853.03 | 17064.93 |

Checkout status mix: `HTTP 202=600, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 38543 | 0 | 69 | 10.48 | 114 | 11110200 | 10/0/10 | 18206 | 32.226758175s | 44.57% / 69.09% | 24791360 / 28647424 |
| order | 0 | 0 | 0 | 0.00 | 38 | 2700568 | 8/0/8 | 0 | 0s | 7.70% / 14.48% | 12393311 / 13455360 |
| inventory | 601 | 0 | 2 | 22.66 | 32 | 4887168 | 10/0/10 | 0 | 0s | 8.59% / 17.20% | 15828572 / 17301504 |
| payment | 1800 | 0 | 4 | 9.93 | 46 | 6269224 | 10/0/10 | 0 | 0s | 12.55% / 20.52% | 14668762 / 17489920 |

### PostgreSQL metrics

Samples: 84. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

### Telemetry limitations

- PostgreSQL container: Get "http://docker/containers/json?filters=%7B%22label%22%3A%5B%22com.docker.compose.service%3Dpostgres%22%2C%22com.docker.compose.oneoff%3DFalse%22%5D%7D": dial unix /var/run/docker.sock: connect: no such file or directory.

## c4-rate-030

**What it does:** an open-loop scheduler offers 30 workflows/s for 30s across all 50 products without waiting for earlier workflows

**Result:** PASS — 901 offered, 901 accepted, 901 completed (901 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 30s.

**Deployment:** cloud-test6-rate

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 30.03/s |
| Accepted rate | 30.03/s |
| Completed rate | 30.03/s |
| Mean/max in-flight requests | 26.97 / 179 |
| Mean/max in-flight workflows | 218.59 / 261 |
| Little's law observed / expected | 218.59 / 218.59 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 12.4% |
| Load generator memory at end | 14.8 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 901 | 41.70 | 92.50 | 143.60 | 172.06 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 901 | 6881.64 | 11614.62 | 13676.80 | 19870.59 |
| Server saga completion | 901 | 6795.45 | 11563.43 | 13552.56 | 19741.81 |

Checkout status mix: `HTTP 202=901, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 58178 | 0 | 179 | 49.45 | 236 | 16082232 | 10/0/10 | 87878 | 10m5.985183356s | 61.66% / 105.30% | 29661725 / 33808384 |
| order | 0 | 0 | 0 | 0.00 | 43 | 4671504 | 8/2/6 | 0 | 0s | 9.87% / 22.20% | 12172229 / 17321984 |
| inventory | 901 | 0 | 3 | 22.19 | 32 | 4884240 | 10/0/10 | 0 | 0s | 11.84% / 25.45% | 15645966 / 18014208 |
| payment | 2703 | 0 | 4 | 10.89 | 46 | 5440048 | 10/0/10 | 2 | 2.07332ms | 16.04% / 27.85% | 14587645 / 16965632 |

### PostgreSQL metrics

Samples: 88. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

### Telemetry limitations

- PostgreSQL container: Get "http://docker/containers/json?filters=%7B%22label%22%3A%5B%22com.docker.compose.service%3Dpostgres%22%2C%22com.docker.compose.oneoff%3DFalse%22%5D%7D": dial unix /var/run/docker.sock: connect: no such file or directory.

## c4-rate-040

**What it does:** an open-loop scheduler offers 40 workflows/s for 30s across all 50 products without waiting for earlier workflows

**Result:** PASS — 1200 offered, 1200 accepted, 1200 completed (1200 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 30s.

**Deployment:** cloud-test6-rate

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 40.00/s |
| Accepted rate | 40.00/s |
| Completed rate | 40.00/s |
| Mean/max in-flight requests | 812.15 / 715 |
| Mean/max in-flight workflows | 1411.17 / 858 |
| Little's law observed / expected | 1411.17 / 1411.17 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 19.1% |
| Load generator memory at end | 42.5 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 1200 | 115.98 | 450.88 | 705.29 | 1129.42 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 1200 | 44585.74 | 50750.17 | 51674.99 | 54316.04 |
| Server saga completion | 1200 | 44349.48 | 50503.18 | 51471.17 | 53715.92 |

Checkout status mix: `HTTP 202=1200, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 180349 | 0 | 714 | 370.64 | 776 | 19279648 | 10/0/10 | 361657 | 6h42m19.392845273s | 89.87% / 126.49% | 56336727 / 71540736 |
| order | 0 | 0 | 0 | 0.00 | 39 | 2753304 | 5/0/5 | 0 | 0s | 7.73% / 18.38% | 11870036 / 16093184 |
| inventory | 1201 | 0 | 4 | 20.80 | 44 | 6275472 | 10/0/10 | 0 | 0s | 8.34% / 49.42% | 15743576 / 17334272 |
| payment | 3600 | 0 | 4 | 8.08 | 46 | 4794112 | 10/0/10 | 0 | 0s | 12.14% / 29.94% | 14462853 / 16912384 |

### PostgreSQL metrics

Samples: 168. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

### Telemetry limitations

- PostgreSQL container: Get "http://docker/containers/json?filters=%7B%22label%22%3A%5B%22com.docker.compose.service%3Dpostgres%22%2C%22com.docker.compose.oneoff%3DFalse%22%5D%7D": dial unix /var/run/docker.sock: connect: no such file or directory.

## c4-rate-050

**What it does:** an open-loop scheduler offers 50 workflows/s for 30s across all 50 products without waiting for earlier workflows

**Result:** PASS — 1500 offered, 1500 accepted, 1500 completed (1500 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 30s.

**Deployment:** cloud-test6-rate

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 50.00/s |
| Accepted rate | 50.00/s |
| Completed rate | 50.00/s |
| Mean/max in-flight requests | 3051.99 / 1058 |
| Mean/max in-flight workflows | 4458.59 / 1190 |
| Little's law observed / expected | 4458.59 / 4458.59 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 23.3% |
| Load generator memory at end | 50.1 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 1500 | 150.29 | 718.55 | 1107.84 | 1827.83 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 1500 | 118207.59 | 129691.12 | 130610.38 | 131535.99 |
| Server saga completion | 1500 | 117748.52 | 129304.03 | 130224.97 | 131342.01 |

Checkout status mix: `HTTP 202=1500, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 422250 | 0 | 1000 | 579.00 | 885 | 25247808 | 10/1/9 | 846657 | 25h18m20.113818973s | 110.80% / 130.78% | 82812544 / 99565568 |
| order | 0 | 0 | 0 | 0.00 | 40 | 4044760 | 6/0/6 | 0 | 0s | 6.18% / 21.42% | 12048925 / 17170432 |
| inventory | 1502 | 0 | 4 | 20.24 | 43 | 6851048 | 8/0/8 | 0 | 0s | 5.86% / 47.78% | 16435384 / 22159360 |
| payment | 4500 | 0 | 4 | 8.62 | 45 | 6164264 | 10/0/10 | 0 | 0s | 9.82% / 34.31% | 14591378 / 19615744 |

### PostgreSQL metrics

Samples: 311. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

### Telemetry limitations

- PostgreSQL container: Get "http://docker/containers/json?filters=%7B%22label%22%3A%5B%22com.docker.compose.service%3Dpostgres%22%2C%22com.docker.compose.oneoff%3DFalse%22%5D%7D": dial unix /var/run/docker.sock: connect: no such file or directory.

## c4-rate-060

**What it does:** an open-loop scheduler offers 60 workflows/s for 30s across all 50 products without waiting for earlier workflows

**Result:** FAIL — 1801 offered, 1801 accepted, 571 completed (571 paid, 0 cancelled, 1230 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 30s.

**Deployment:** cloud-test6-rate

**Completion observation:** `poll` every 100ms.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 60.03/s |
| Accepted rate | 60.03/s |
| Completed rate | 19.03/s |
| Mean/max in-flight requests | 6937.26 / 1571 |
| Mean/max in-flight workflows | 8948.00 / 1641 |
| Little's law observed / expected | 8948.00 / 1567.99 (470.67% difference) |
| Load generator CPU (percent of one core, measured window) | 24.8% |
| Load generator memory at end | 49.6 MB |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 1801 | 238.02 | 903.25 | 1225.01 | 2534.12 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 571 | 102970.60 | 158943.90 | 175878.71 | 179182.38 |
| Server saga completion | 965 | 135117.83 | 193453.33 | 194062.55 | 195487.00 |

Checkout status mix: `HTTP 202=1801, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 602971 | 540 | 1001 | 756.61 | 572 | 22295760 | 8/0/8 | 1212677 | 50h3m46.419437119s | 115.21% / 141.60% | 99172166 / 114896896 |
| order | 0 | 0 | 0 | 0.00 | 52 | 5037704 | 8/2/6 | 0 | 0s | 5.65% / 46.50% | 12155426 / 15556608 |
| inventory | 1804 | 0 | 5 | 21.53 | 32 | 3504480 | 2/2/0 | 0 | 0s | 4.31% / 25.08% | 15598367 / 21303296 |
| payment | 5403 | 0 | 4 | 9.18 | 46 | 5613048 | 10/0/10 | 0 | 0s | 9.16% / 35.34% | 14423098 / 19079168 |

### PostgreSQL metrics

Samples: 421. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

`pg_stat_statements`: unavailable — pg_stat_statements is not enabled in the local PostgreSQL image.

### Invariants

- PASS — inventory counters stay within bounds (violations: 0)
- PASS — one reservation per order (violations: 0)
- PASS — one payment per order (violations: 0)
- PASS — no duplicate product line per order (violations: 0)
- PASS — paid orders have successful payment (violations: 0)
- PASS — cancelled orders have failed payment (violations: 0)
- FAIL — paid reservations are confirmed (violations: 794)
- PASS — cancelled reservations are released (violations: 0)
- PASS — unknown payments retain pending orders (violations: 0)
- PASS — payment amounts match order totals in paise (violations: 0)

### Telemetry limitations

- PostgreSQL container: Get "http://docker/containers/json?filters=%7B%22label%22%3A%5B%22com.docker.compose.service%3Dpostgres%22%2C%22com.docker.compose.oneoff%3DFalse%22%5D%7D": dial unix /var/run/docker.sock: connect: no such file or directory.
