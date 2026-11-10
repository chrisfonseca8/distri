# Where the next choke is after the webhook inbox (prediction, written before the A/B run)

Everything marked PREDICTED is arithmetic on the measured numbers in `02`; none of it has been tested.

## What 2 workers should do
- Measured: one worker = 7.77 ms/event = 128.7 events/s (0.99 busy at 60/s). Each checkout = 3 events.
- PREDICTED: 2 workers = ~257 events/s = ~86 checkouts/s. At 60/s (178 events/s) each worker would be ~0.69 busy instead of one worker at 1.38 demanded, so the backlog should stay near zero and capture-to-finish should fall back from 37.6 s to ~0.7 s. 3 workers = ~386 events/s = ~128 checkouts/s. This assumes the payment DB pool and Neon keep up (see 1 below).

## Confound found: client polling inflates orchestrator CPU
Orchestrator `GET /v1/checkouts/{id}` per second: 80 (20/s), 113 (30/s), 159 (40/s), **949 (60/s)**. The client polls every in-flight saga every 2 s and 3,380 sagas were in flight at 60/s. Fit on the 4 runs (4 points, 3 parameters, so the fit is exact by construction and is a rough guide only): orchestrator CPU = 4.7 + 0.677 x checkouts/s + 0.021 x polls/s (% of one core). Polling was ~20 of the 65% mean CPU at 60/s. With the inbox fixed, in-flight sagas ~ 60 x 7.2 s = 430, so ~216 polls/s and orchestrator CPU ~50% of a core, not 84%.

## Ranked next chokes (status)
| # | Where | Numbers | Status |
|---|---|---|---|
| 1 | payment-service DB pool (10) | One pool serves: webhook HTTP inserts (~178/s at 60/s), the processing workers (now 2), 4 job workers, reconciler, outbox relay, consumers. DB statements/s 913 (40/s) -> 1,189 (60/s) with workers=1. Only end-of-run snapshot (10 open, 0 in use, 0 waits). No pool gauge. | **Suspected** first real limit when workers go to 2; **unknown** until measured |
| 2 | webhook inbox again, at ~86 checkouts/s (2 workers) | predicted capacity above | **Predicted**; add workers if so |
| 3 | orchestrator CPU / single process | PREDICTED CPU of one core: 50% at 60/s, 65% at 80/s, 80% at 100/s, 95% at 120/s; host has 2 vCPU so the hard ceiling is higher. Reply acks 187/s, outbox publish 210/s at 60/s, DB statements 3,859/s (incl. polling). | **Suspected** around 100-120 checkouts/s, not a limit at 60/s |
| 4 | orchestrator outbox relay | pending 160 at 60/s but oldest 0.65 s, publish p95 0.17 s | **Unlikely** at 60/s; unknown above |
| 5 | order-service commands | queue ready max 38 at 60/s (4 consumers), 102 acks/s, CPU 11% | **Unlikely** |
| 6 | loadgen+simulator host (2 vCPU) | emits ~178 webhooks/s and receives ~949 polls... CPU never scraped | **Unknown** |
| 7 | Neon compute (2 CU cap), lock waits | no series | **Unknown** |

## Measurements the A/B run must add so the next choke is read, not guessed
payment pool wait_count/wait_duration (end snapshot) and payment DB statements/s; payment container and host CPU; orchestrator CPU and `distri.orchestrator.replies` ready/unacked; loadgen host CPU; poll rate (use a longer `LOAD_STATUS_POLL_INTERVAL`, same in both arms, so polling stops contaminating orchestrator CPU). After B, one more run at 80/s and 100/s (same method) to find the next knee.

---
# Correction, same day: did the DB hit its limit? (measured, not predicted)

Sources: `db_pool` in each run's `load-test.json` (per-run delta of `sql.DB.Stats` against a baseline taken at run start: `orchestrator-service/pkg/telemetry/telemetry.go:213`), Prometheus `db_query_duration_seconds` (GORM statement time, which **includes waiting for a pool connection**), and `pg_stat_*` on the Neon audit connection after the runs.

**Per-run pool waits (wait_count / total wait), pool max 10 per service**

| Service | s20 | s30 | s40 | o60 |
|---|---|---|---|---|
| orchestrator | 2,616 / 1,805 s (cold first run) | 2,051 / 8.4 s | 10,262 / 40.6 s | **235,827 / 1,604 s** |
| payment | 44 / 12.2 s (cold) | 31 / 0.02 s | 172 / 0.16 s | 776 / **0.70 s** |
| inventory | 23 / 0.16 s | 0 | 0 | 0 |
| order | 0 | 0 | 0 | 0 |

**Statements (Prometheus)**

| | stmts/s | mean | p99 | statement-time "busy connections" (rate of summed duration) |
|---|---:|---:|---:|---:|
| payment s40 -> o60 | 913 -> 1,189 | 1.96 -> 2.00 ms | 7.1 -> 7.7 ms | 1.79 -> 2.37 |
| orchestrator s40 -> o60 | 1,861 -> 3,859 | 1.70 -> **3.49 ms** | 6.9 -> **44.9 ms** | 3.17 -> **13.46 (> the pool of 10, so it includes queueing)** |

**Findings**
1. **The orchestrator's pool (10) hit its limit at 60/s.** 235,827 waits (~880/s over the run), 1,604 s of cumulative waiting, ~6.8 ms per wait, ~6 goroutines waiting on average. It was 23x fewer waits at 40/s. The server time per statement did not change: 3.49 ms mean minus ~1.56 ms of pool wait per statement leaves ~1.9 ms, equal to the 1.7 ms at 40/s. So the extra latency is queueing for connections, not a slower Postgres.
2. **Much of that load is the load generator's polling** (949 `GET /v1/checkouts/{id}`/s at 60/s vs 159/s at 40/s), each a DB read on that 10-connection pool. So this is partly a test artefact; in a fixed-inbox 60/s run the in-flight count and polling would be ~4x lower. It had a small effect on saga steps (CREATE_ORDER 158 -> 211 ms, MARK_ORDER_PAID 165 -> 188 ms, CONFIRM_STOCK 391 -> 394 ms), far smaller than the inbox's +37 s.
3. **The payment pool did not hit its limit at workers=1:** 776 waits totalling 0.70 s in the whole 60/s run (0.9 ms each); a lower bound on connections held (webhook HTTP 0.95 + webhook worker 0.99 + command consumer 0.55) is 2.5 of 10. A second worker adds ~1.0, so ~3.5 + job workers/reconciler/relay (not measured) is still probably under 10. My earlier "payment pool is the main suspect" is **downgraded to unlikely-but-unmeasured**.
4. **Postgres/Neon itself shows no limit:** `max_connections` 901 vs ~21 backends (orchestrator 6, payments 6, orders 5, inventory 3, +admin/audit); 0 deadlocks in every service DB; payment statement mean flat at ~2 ms from 489 to 1,189 stmts/s. **Not measurable here:** Neon compute CPU (no API access), lock waits during the run (sampled only after the runs, and the runner's sampler sees only the audit connection).
5. s20's huge waits (1,805 s for 2,616 waits, ~690 ms each; statement mean 10.4 ms vs ~1.7-2 ms in later runs) are the cold first run (Neon compute and connections warming), which explains its odd p95. Treat s20 as warm-up.

**Revised ranking of next chokes:** (1) orchestrator pool (10), confirmed saturated at 60/s, partly inflated by polling; (2) webhook inbox again at ~86/s after 2 workers (predicted); (3) payment pool, unlikely at 2 workers; (4) orchestrator CPU; Neon compute and the loadgen host remain unknown.

---
# What is causing the orchestrator pool to wait (measured, 2026-11-09)

Connection-holding time by caller, orchestrator-service, busy seconds per second (= rate x duration of the handler that holds a connection; a pool of 10 supplies 10 s per second; durations include time spent waiting for a connection, so they are inflated once the pool is saturated):

| Caller | s40 | o60 |
|---|---:|---:|
| `GET /v1/checkouts/{id}` (client polling) | 0.62 (159/s) | **9.71 (949/s)** |
| reply consumer (`saga_reply` tx, 158 -> 187/s) | 2.14 | 2.86 |
| `POST /v1/checkouts` (`create_saga` tx, 40 -> 60/s) | 1.27 | 2.18 |
| total | 4.0 (40% of the pool) | 14.8 (148%) |

- SELECT statements/s 675 -> 2,333 (+1,658) while INSERT 596 -> 782 and UPDATE 516 -> 649 grew only ~1.3x: the growth is the polling reads.
- Without polling the saga work at 60/s needs ~5 of 10 connections.
- Chain: webhook-inbox backlog -> running sagas 361 -> 3,380 -> client polls every in-flight saga every 2 s (949/s) -> polling alone wants ~97% of the pool -> every other caller queues. Secondary structural factor: `ORCHESTRATOR_CONSUMER_MAX_CONCURRENCY=30` against `DB_MAX_OPEN_CONNS=10` (reply consumers reached 12 at 40/s, 9 at 60/s) lets consumers outnumber connections by design.
- Neon console (user screenshot, last hour, compute shared by all four service DBs): CPU peaked at ~0.6 vCPU during the 40/s run and ~1.5 vCPU during the 60/s run (read off the chart, approximate; allocated size not visible in the screenshot), RAM ~1-2 GB, 0 deadlocks, then idle. Not pegged, but not far from a 2 vCPU limit if that is the cap.
