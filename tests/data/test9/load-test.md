# Local load-test results

C1 is a smoke test; C2 is the repeated baseline; C3/C4 sweep concurrency and offered rate; C5–C7 exercise hot-row, skewed, and multi-item contention; C9–C12 exercise duplicate checkouts, concurrent double pay, all-decline, and random-outcome behaviour; C13 sweeps simulated gateway latency; C14 sweeps the database pool size and idle connections. When a case is run again, earlier runs are kept in the Repeat history table. Everything in this file is written by the runner. Each run starts from an isolated transactional fixture, uses the configured gateway simulator, and finishes with the database invariant audit.

Updated: `2026-11-01T21:53:30Z`

## C2 run-to-run variance (1 runs recorded)

| Metric | Mean | Min | Max | Std dev | CV |
|---|---:|---:|---:|---:|---:|
| Accepted/s | 42.45 | 42.45 | 42.45 | 0.00 | 0.00% |
| Completed/s | 42.45 | 42.45 | 42.45 | 0.00 | 0.00% |
| Checkout p95 ms | 444.10 | 444.10 | 444.10 | 0.00 | 0.00% |
| Pay p95 ms | 0.00 | 0.00 | 0.00 | 0.00 | 0.00% |
| End-to-end p95 ms | 21779.42 | 21779.42 | 21779.42 | 0.00 | 0.00% |

## c2-baseline-1

**What it does:** 700 closed-loop clients continuously perform checkout → pay → terminal-order workflows for 5m0s across all 50 products

**Result:** PASS — 12736 offered, 12736 accepted, 12736 completed (12736 paid, 0 cancelled, 0 pending at drain).

**Environment:** 2 logical CPUs, 3999805440 bytes memory (the host running the runner); gateway `success` with 200ms simulator latency; warm-up 10s; measurement 5m0s.

**Deployment:** cloud-test9-soak-5m

**Completion observation:** `sse`.

### Client metrics

| Metric | Value |
|---|---:|
| Offered rate | 42.45/s |
| Accepted rate | 42.45/s |
| Completed rate | 42.45/s |
| Mean/max in-flight requests | 721.86 / 700 |
| Mean/max in-flight workflows | 721.86 / 700 |
| Little's law observed / expected | 721.86 / 721.86 (0.00% difference) |
| Load generator CPU (percent of one core, measured window) | 2.4% |
| Load generator memory at end | 45.8 MB |
| SSE attempts / terminal events / polling fallbacks | 12736 / 12736 / 0 |

| Latency | Samples | p50 ms | p95 ms | p99 ms | max ms |
|---|---:|---:|---:|---:|---:|
| Checkout | 12736 | 30.91 | 444.10 | 1184.80 | 1287.64 |
| Pay | 0 | 0.00 | 0.00 | 0.00 | 0.00 |
| End to end | 12736 | 16758.23 | 21779.42 | 25183.04 | 33741.15 |
| Server saga completion | 12736 | 16720.48 | 21696.56 | 24912.78 | 33725.15 |

Checkout status mix: `HTTP 202=12736, transport=0, decode=0, 5xx=0`<br>
Pay status mix: `transport=0, decode=0, 5xx=0`

### Service metrics

| Service | Requests | Errors | In-flight max | Handler p95 ms | Goroutines | Heap alloc | DB open/in-use/idle | DB waits | DB wait duration | CPU mean/max | Memory mean/max |
|---|---:|---:|---:|---:|---:|---:|---|---:|---:|---|---|
| orchestrator | 25472 | 0 | 701 | 20272.53 | 749 | 31401072 | 10/0/10 | 31208 | 7m43.28962955s | 34.50% / 107.26% | 73379307 / 82194432 |
| order | 0 | 0 | 0 | 0.00 | 39 | 6813304 | 8/0/8 | 0 | 0s | 15.42% / 55.74% | 12941459 / 17743872 |
| inventory | 12741 | 0 | 356 | 52.47 | 32 | 5239680 | 10/0/10 | 708 | 4m0.099211039s | 17.80% / 94.92% | 15553263 / 32256000 |
| payment | 38208 | 0 | 4 | 8.79 | 44 | 7489200 | 10/0/10 | 1209 | 1.176486648s | 26.18% / 64.13% | 14787741 / 20090880 |

### PostgreSQL metrics

Samples: 639. Connections mean/max: 1.00/1. Active mean/max: 0.00/0. Idle-in-transaction max: 0. Lock-waiting sessions max: 0. Ungranted locks max: 1. Deadlocks before/after: 0/0. Database bytes before/after: 7995392/7995392. New PostgreSQL sessions opened during the run: 0.

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
