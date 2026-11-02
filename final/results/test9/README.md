# Test 9 — soak (final result, shortened to 5 minutes)

**Date:** 1 Nov 2026, 21:47–21:54 UTC · **Environment:** `staging` (ap-southeast-2; gateway simulator `success` with its 5 s pay delay) · **Run:** `cloud-test9-20261101T214709Z` (full evidence in `../cloud-test9-20261101T214709Z/`).

**Data in this folder:** [`load-test.md`](load-test.md) / [`load-test.json`](load-test.json), [`per-minute.json`](per-minute.json) (the Prometheus series averaged per minute), [`phase22-audit.md`](phase22-audit.md), [`audit.json`](audit.json), [`run.json`](run.json).

## Verdict

**PASS for a 5-minute soak; this does not meet the plan's 30–60 minutes.** At 700 clients (70% of the roughly 1000-client limit from test 6) the stack ran 5 minutes with every workflow completing, steady throughput, no growth in the outbox backlog, and a clean audit afterwards. The owner shortened the soak to 5 minutes, so slow problems that take longer to show (memory growth, database growth, connection leaks) are **not ruled out**.

## What was done

C2 with 700 closed-loop clients, SSE completion, a 10 s warm-up and a 5-minute measured period, one repetition, from the load-generator host. 700 clients is 70% of the ~1000-client limit. Nothing was changed on the services, and one orchestrator replica was running (a second replica from test 8 had been stopped first).

## Results

| Check | Result |
|---|---|
| Offered = accepted = completed | **12,736** (0 pending at the drain, 0 5xx, 0 transport errors) |
| Accepted and completed per second | **42.45 / 42.45** |
| Checkout latency p50 / p95 / p99 | 31 ms / 444 ms / 1.18 s |
| End-to-end latency p50 / p95 / p99 | 16.8 s / 21.8 s / 25.2 s |
| In-flight workflows (mean / max) | 721.9 / 700; Little's law matches (0.00% difference) |
| Phase-22 audit | **PASS** |
| Observability audit | PASS, no violations; 0 stuck sagas, 0 dead outbox rows, 0 dead-queue messages |

### Drift check, per minute (Prometheus)

| Minute | 0 (ramp) | 1 | 2 | 3 | 4 | 5 | 6 (end) | 7 (drain) |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| Completed per second | 10.8 | 37.9 | 39.6 | 40.4 | 40.7 | 42.0 | 14.5 | 0.0 |
| Accepted per second | 11.1 | 41.6 | 39.6 | 40.4 | 40.6 | 41.7 | 4.1 | 0.0 |
| Saga p95 (s) | 28.4 | 28.8 | 29.0 | 29.0 | 29.0 | 29.0 | 29.0 | – |
| Outbox pending rows (max) | 689 | 32 | 45 | 30 | 19 | 31 | 14 | 0 |

- **Throughput does not drift down.** Minutes 1–5 rise slowly from 37.9 to 42.0 completed/s.
- **Latency is flat** at about 29 s p95 in the Prometheus histogram. That figure sits near the top of the histogram's range, so it is a ceiling on resolution, not a precise number; the runner's own end-to-end p95 is 21.8 s.
- **The outbox backlog does not grow:** it peaks at 689 rows during the ramp-up (clients starting), then stays between 19 and 45 and drains to 0.

## State after the soak

Read directly from the database afterwards: 12,736 sagas, all COMPLETED; 12,736 orders PAID; 12,736 payments SUCCEEDED; 12,736 reservations CONFIRMED; no negative stock; dead queue 0.

## Limits

1. **5 minutes, not 30–60.** Trends that need longer (memory, connections, table growth, retention jobs) cannot be judged.
2. **Database growth was not measured:** the runner reports the same database size (7,995,392 bytes) before and after every run, including this one, so that figure does not track the service databases. The plan's "database growth matches the retention jobs" check is therefore not covered.
3. **Latency is dominated by the simulator's 5 s pay delay,** and the end-to-end time with 700 clients in flight (median 16.8 s) is about 10 s above the idle figure; the cause of that queueing is still open (see tests 6 and 7).
4. **One run,** no repeat, against the simulated gateway, one replica of each service.
5. **Alerts fired:** `DatabasePoolSaturated`, `HighLatencyP95` and `ServiceDown`. I did not investigate them.
6. **Run record not filled in** (git commit, image tag, Neon branch).
