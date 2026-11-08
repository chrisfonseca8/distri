# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-01T20:30:10Z`

## C3 client sweep

| Clients | Offered/s | Accepted/s | Completed/s | End-to-end p99 ms | Max in-flight | 5xx | Transport errors |
|---:|---:|---:|---:|---:|---:|---:|---:|
| 500 | 37.63 | 37.63 | 37.63 | 23837.88 | 500 | 0 | 0 |
| 1000 | 50.75 | 50.75 | 50.75 | 36400.97 | 1000 | 0 | 0 |

No knee/saturation candidate was detected in the recorded steps.

## c3-clients-1000

**What it does:** 1000 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 3045 offered, 3045 accepted, 3045 completed (3045 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test6-sse

**Completion observation:** `sse`.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 50.75/s |
| Accepted rate | 50.75/s |
| Completed rate | 50.75/s |
| Mean/max in-flight requests | 1227.43 / 1000 |
| Mean/max in-flight workflows | 1227.43 / 1000 |
| Little's law observed / expected | 1227.43 / 1227.43 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 2.1% |
| Load generator memory at end | 86.9 MB |
| SSE attempts / terminal events / polling fallbacks | 3045 / 3045 / 0 |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 3045 | 36.84 | 1844.91 | 1915.17 | 1936.24 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 3045 | 25504.45 | 31630.36 | 36400.97 | 42494.21 |
| Server saga completion | 3045 | 25442.18 | 31282.50 | 35138.01 | 41259.21 |

Checkout status mix: `HTTP 202=3045, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 6090 | 0 | 1000 | 29586.71 | 1110 | 28946728 | 10/0/10 | 8015 | 18m52.667203351s | 29.90% / 104.08% | 98435997 / 127848448 |
| order | 0 | 0 | 0 | 0.00 | 44 | 5386320 | 8/0/8 | 0 | 0s | 12.35% / 44.40% | 12590733 / 15118336 |
| inventory | 3046 | 0 | 369 | 806.60 | 32 | 8862784 | 10/0/10 | 984 | 6m31.506806762s | 15.85% / 95.10% | 16673034 / 38297600 |
| payment | 9135 | 0 | 4 | 9.87 | 49 | 7139008 | 10/0/10 | 641 | 651.252728ms | 23.57% / 59.52% | 14992540 / 19505152 |

### PostgreSQL metrics

Samples: 178. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 0. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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

## c3-clients-500

**What it does:** 500 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 1m0s across all 50 products

**Result:** PASS — 2258 offered, 2258 accepted, 2258 completed (2258 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 1m0s.

**Deployment:** cloud-test6-sse

**Completion observation:** `sse`.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 37.63/s |
| Accepted rate | 37.63/s |
| Completed rate | 37.63/s |
| Mean/max in-flight requests | 557.60 / 500 |
| Mean/max in-flight workflows | 557.60 / 500 |
| Little's law observed / expected | 557.60 / 557.60 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 1.9% |
| Load generator memory at end | 38.0 MB |
| SSE attempts / terminal events / polling fallbacks | 2258 / 2258 / 0 |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 2258 | 32.90 | 1503.06 | 1602.81 | 1631.24 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 2258 | 14737.79 | 20037.45 | 23837.88 | 33772.22 |
| Server saga completion | 2258 | 14603.23 | 19726.76 | 23316.02 | 32995.38 |

Checkout status mix: `HTTP 202=2258, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 4516 | 0 | 500 | 18492.31 | 552 | 16900408 | 10/0/10 | 2385 | 6m51.674374328s | 26.48% / 59.96% | 57802725 / 69750784 |
| order | 0 | 0 | 0 | 0.00 | 44 | 4209496 | 8/0/8 | 0 | 0s | 11.47% / 37.97% | 12683991 / 18059264 |
| inventory | 2259 | 0 | 244 | 715.27 | 36 | 7000832 | 10/0/10 | 501 | 3m30.302563794s | 14.06% / 67.40% | 16473815 / 26726400 |
| payment | 6774 | 0 | 4 | 9.06 | 49 | 6735704 | 10/0/10 | 121 | 87.714809ms | 21.22% / 38.89% | 14904858 / 20553728 |

### PostgreSQL metrics

Samples: 153. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 1. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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
