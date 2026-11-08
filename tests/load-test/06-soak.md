# Load test 06 — Soak (5 minutes at 700 clients)

**Result:** PASS for a 5-minute soak. This does not meet the planned 30-60 minutes.

## What is tested

Whether the stack holds a steady rate at about 70% of the 1000-client test point without throughput or latency drifting, the outbox backlog growing, or invariants breaking.

## How it is run

Case C2 with 700 closed-loop clients, SSE completion, 10 s warm-up, 5 minutes measured, one repetition, from the load-generator host. Nothing was changed on the services, and one orchestrator replica was running. Simulated gateway with its 5 s pay delay.

## Results

| Check | Result |
|---|---|
| Offered = accepted = completed | **12,736** (0 pending at the drain, 0 5xx, 0 transport errors) |
| Accepted and completed per second | **42.45 / 42.45** (sustained over 5 minutes) |
| Checkout latency p50 / p95 / p99 | 31 ms / 444 ms / 1.18 s |
| End-to-end latency p50 / p95 / p99 | 16.8 s / 21.8 s / 25.2 s |
| In-flight workflows (mean / max) | 721.9 / 700; Little's law matches (0.00% difference) |
| Phase-22 database audit | PASS |
| Observability audit | PASS; 0 stuck sagas, 0 dead outbox rows, 0 dead-queue messages |

### Drift check, per minute (Prometheus)

| Minute | 0 (ramp) | 1 | 2 | 3 | 4 | 5 | 6 (end) | 7 (drain) |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| Completed per second | 10.8 | 37.9 | 39.6 | 40.4 | 40.7 | 42.0 | 14.5 | 0.0 |
| Accepted per second | 11.1 | 41.6 | 39.5 | 40.4 | 40.6 | 41.7 | 4.1 | 0.0 |
| Saga p95 (s) | 28.4 | 28.8 | 29.0 | 29.0 | 29.0 | 29.0 | 29.0 | – |
| Outbox pending rows (max) | 689 | 32 | 45 | 30 | 19 | 31 | 14 | 0 |

- Throughput does not drift down: minutes 1-5 rise slowly from 37.9 to 42.0 completed/s.
- Latency is flat at about 29 s p95 in the Prometheus histogram. That value sits near the top of the histogram's range, so it is a resolution ceiling; the runner's own end-to-end p95 is 21.8 s.
- The outbox backlog peaks at 689 rows during ramp-up, then stays between 19 and 45 and drains to 0.

State read from the database afterwards: 12,736 sagas COMPLETED, 12,736 orders PAID, 12,736 payments SUCCEEDED, 12,736 reservations CONFIRMED, no negative stock, dead queue 0.

## Limits

- 5 minutes, not 30-60: memory growth, connection leaks and table growth cannot be judged.
- Database growth was not measured (the runner reports the same database size before and after every run, so that figure does not track the service databases).
- One run, no repeat; simulated gateway; single replica of each service.
- `DatabasePoolSaturated`, `HighLatencyP95` and `ServiceDown` alerts fired (not investigated).

**Raw data:** [`load-test.md`](data/soak/load-test.md), [`load-test.json`](data/soak/load-test.json), [`per-minute.json`](data/soak/per-minute.json), [`phase22-audit.md`](data/soak/phase22-audit.md), [`audit.json`](data/soak/audit.json)
