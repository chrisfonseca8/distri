# Staging bottleneck diagnosis — results and decision

Date 2026-11-09. Environment: AWS staging (Sydney), one c7i.large per service + m7i.large broker + loadgen/simulator host, Neon `ep-falling-tree-a74c336v` (disposable, confirmed by the user), image `09e4015` for all four services, simulator `SIM_PAY_DELAY=5s` (lognormal, see below), pools 10/10 per service. **Simulated gateway; the older fast-gateway results (`tests/load-test/04-...`) are a different experiment and are not mixed in here.** SSE not used; completion is read from server-side saga timestamps; the client polls every 2 s.

## 1. Commands and configuration

Target check (before any traffic): each service container's DB host and image tag were read with `docker inspect` (all `ep-falling-tree-a74c336v`, `*-09e4015`), and `LOAD_TEST_ALLOWED_DB_HOST` in the loadgen env matched. Instances were started with `aws ec2 start-instances`; no security group changed (the laptop's rotating egress IP <laptop-egress-ip> was already allowed).

Telemetry: `deploy/observability/scripts/obs-tunnels.sh start` (SSH tunnels), `hosts.env` updated to the new public IPs, Prometheus file-sd targets written for run `inprovemts-dry10-20261109T143509Z` (all 14 targets up).

One code change, authorised by "move forward" after the question was asked: `tools/load-test/runner/config.go` — case C4 now honours `LOAD_DURATION` (it was fixed at 30 s). The image was built locally (`docker build tools`), tagged `ghcr.io/chrisfonseca8/distri:loadgen-09e4015-c4dur`, and loaded onto the loadgen host over SSH (`docker save | ssh docker load`); **nothing was pushed to a registry.**

Each run (script `inprovemts/scripts/sweep.sh`, which calls `~/run-inprovemts.sh` on the loadgen host and waits for RabbitMQ ready+unacked = 0, outbox pending = 0, RUNNING sagas = 0 before starting):
```sh
IMAGE_TAG=09e4015-c4dur TEST_RUN_ID=inprovemts-<label> RESULTS_SUBDIR=cloud/inprovemts/<label>-<ts> \
LOAD_CASE=C4 LOAD_RATE_STEPS=<rate> LOAD_RUNS=1 LOAD_WARMUP=60s LOAD_DURATION=180s \
LOAD_WORKFLOW_TIMEOUT=120s LOAD_STATUS_POLL_INTERVAL=2s LOAD_MAX_CONNS=1000 LOAD_TEST_ENVIRONMENT=inprovemts-<label> \
docker compose --env-file deploy/cloud/.env.loadgen -f deploy/cloud/compose.loadgen.yaml --profile loadtest run --rm loadtest
```
Runs: s20 `20`, s30 `30`, s40 `40`, o60 `60` (UTC start: 14:59:54, 15:05:16, 15:10:33, ~15:19:30). Earlier 30 s-window runs (`data/dry10`, `data/s20-30s-window`, `data/s30-30s-window`) are kept but are not part of the table.

Analysis tools (all in this folder): `scripts/prom_summary.py <run-dir>` (Prometheus timeline), `scripts/step_intervals.sql` (per-saga step intervals from `orchestrator.saga_steps` and `payments.payment_attempts`, run through a throwaway `postgres:17-alpine` psql container against the audit DB).

## 2. Comparison (measured window = 180 s after a 60 s warm-up; open loop; uniform over 50 SKUs; 0 rejected, 0 5xx, 0 timeouts in every run)

| Run | Offered/s | Accepted/s | Completed/s | Server saga p50 | p95 | max | Peak RUNNING sagas | Peak webhook backlog | Oldest saga age | Drain after offering stops | Unfinished |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| s20 | 20 | 20.0 | 20.0 | 6.5 s | 15.8 s | 25.1 s | 260 | 277 | 21 s | 13 s | 0 |
| s30 | 30 | 30.0 | 30.0 | 6.3 s | 11.0 s | 23.5 s | 223 | 33 | 19 s | 17 s | 0 |
| s40 | 40 | 40.0 | 40.0 | 6.6 s | 11.3 s | 27.9 s | 361 | 342 | 25 s | 13 s | 0 |
| **o60** | 60 | 60.0 | 60.0 | **43.6 s** | **80.1 s** | 92.3 s | **3,380** | **9,005** | **88 s** | **87 s** | 0 |

"Completed" counts COMPLETED + COMPENSATED; compensated was 0 in all runs. HTTP: 202 only. p95 of `POST /v1/checkouts` was 45 ms (dry10), orchestrator handler p95 50 ms at 40/s. s20 ran first after the stack had been idle and has the worst p95 at the lowest rate (cold connections / consumer autoscale / Neon compute): treat it as a warm-up artefact, not a rate effect (not re-run).

## 3. Where the time goes

Per-saga intervals, same unit (ms), from the saga/payment tables (rows exist only for the last run before a reset, so s40 and o60):

| Interval | s40 avg / p95 | o60 avg / p95 |
|---|---:|---:|
| accept -> stock reserved (done synchronously before the saga row; step finished ~1 ms before `created_at`) | ~0 | ~0 |
| reserved -> order created | 158 / 418 | 211 / 625 |
| order created -> payment step started | 0 | 0 |
| payment started -> attempt row created | 64 / 111 | 66 / 166 |
| attempt created -> capture observed (gateway delay) | 5,533 / 9,719 | 5,532 / 9,838 |
| **capture observed -> TAKE_PAYMENT finished (result handling + reply)** | **731 / 1,527** | **37,585 / 73,809** |
| order update (MARK_ORDER_PAID) | 165 / 387 | 188 / 522 |
| stock confirm (CONFIRM_STOCK) | 391 / 670 | 394 / 760 |
| accept -> terminal | 7,041 / 11,331 | 43,972 / 80,060 |

- The 5.5 s average / 9.7 s p95 "gateway" interval is **the simulator, not our code**: `gateway-simulator/internal/sim/store.go:343` draws the pay delay from `lognormal(median=SIM_PAY_DELAY, sigma=0.4)`. Median 5 s gives p95 = 5·e^(1.645·0.4) = 9.65 s and p99 = 12.7 s; measured p50 5.1 s, p95 9.7-9.9 s, p99 12.3-13.5 s, the same each minute (`data/s40/payment-lateness.txt`). That is why saga p95 is ~11 s even when nothing is queued. It is unchanged at 60/s.
- Between 40/s and 60/s, one interval grows 50x: **capture observed -> TAKE_PAYMENT finished**. Every other step moves by <0.3 s.

## 4. What falls behind first: the payment webhook inbox

Webhooks per attempt: 3 (`payment.authorized`, `payment.captured`, `order.paid`; 21,600 rows for 7,200 attempts).

| Run | Webhooks received/s | Processed/s | Peak backlog | Oldest pending |
|---|---:|---:|---:|---:|
| s20 | 59.5 | 59.5 | 277 | 9.1 s |
| s30 | 88.9 | 88.9 | 33* | 8.5 s |
| s40 | 118.6 | 118.6 | 342 | 8.5 s |
| o60 | 178.2 | **126.8** | **9,005** | **76.7 s** |

(*from the `payment_webhook_backlog` series; s30's figure is lower than s20/s40, consistent with noise from the 1 s poll.)

- Processed/s equals arrivals up to 118.6/s, then stays at ~127/s while 178/s arrive. (178.2 − 126.8) × 180 s = 9,252 events, against an observed peak backlog of 9,005: the backlog is explained by this rate gap. The oldest pending webhook (76.7 s) matches the capture-to-finish p95 (73.8 s).
- payment-service was not busy: container CPU mean 22% (max 25%) of one core, host 17%, no time over 70%; DB statement p95 ~0 s; webhook processing handler p95 ~0.01 s; payment job queue age 0.16 s; no dead letters, no retries. So the inbox limit is **not CPU or database capacity; it is a serial processor** (section 4b). Defaults in `payment-service/pkg/config/webhook_config.go`: poll interval 1 s, batch 20, lease 30 s; the deployed env values were not read (an SSH read of the container env was blocked by the harness and I did not work around it) but they do not matter for the cap, see 4b.

### 4b. Why it caps at ~127 events/s (measured utilisation + code)

`payment_webhook_processing_duration_seconds` (sum/count, 180 s windows) gives the processor's busy time per second:

| Run | Events/s | Mean ms/event | Processor busy (s per s) |
|---|---:|---:|---:|
| s20 | 59.5 | 9.85 | 0.59 |
| s30 | 88.9 | 8.83 | 0.78 |
| s40 | 118.6 | 7.91 | 0.94 |
| o60 | 126.8 | 7.77 | **0.99** |

The code (`payment-service/cmd/payment-service/main.go:~300`, `pkg/services/webhook_processor.go:90-117, 203-218`) starts **one goroutine** per payment-service process; it claims up to `Batch` rows (`FOR UPDATE SKIP LOCKED`, with `workerID` and a lease), then handles them **one at a time, one DB transaction each**, and loops again immediately when the batch was non-empty. The idle poll interval and the batch size therefore do not limit throughput under load. Capacity = 1 / 7.77 ms = **128.7 events/s**, observed 126.8. Each checkout produces 3 events, so the inbox sustains 128.7 / 3 = **42.9 checkouts/s**: the knee sits between the 40/s run (utilisation 0.94) and the 60/s run.

## 5. Services, RabbitMQ, outbox, pools

| | s40 | o60 |
|---|---|---|
| orchestrator CPU (container, % of 1 core) mean / max / seconds >70% | 33.5 / 37.1 / 0 s | **59.5 / 84.3 / 55 s** |
| payment / inventory / order CPU mean | 17.7 / 11.1 / 9.5 | 22.2 / 13.8 / 11.3 |
| broker host CPU mean/max (2 vCPU) | 16.3 / 18.0 | 17.6 / 19.6 |
| `distri.orchestrator.replies` ready max / unacked max / consumers max | small / small / 12 | 135 / 40 / 9 |
| `distri.order.commands` ready max | 4 | 38 |
| inventory / payment command queues ready | ≈0 | ≈0 |
| dead letters, retry-queue republishes | 0, 0 | 0, 0 |
| outbox pending max (orch / pay / inv / order) | 13 / 18 / 28 / 8 | **160** / 24 / 30 / 12; oldest pending ≤ 0.65 s |
| orchestrator consumer throughput | 111 acks/s | 187 acks/s, handler p95 0.03 s |

- Orchestrator is the next stage to saturate: 84% of a core peak and above 70% for 55 s at 60/s, outbox pending 160 (still <1 s old). It is **not** what falls behind first (its reply queue stayed ≤135 messages and handlers p95 30 ms).
- DB pools: the runner reports one end-of-run snapshot only (all services `in_use 0`, `wait_count 0` after s20/dry10). There are no pool gauges in Prometheus, so p95 connection acquisition time and in-use-over-time are **unknown**. DB statement p95 is ~0 s for all four services in both runs.
- DB CPU / lock waits / transaction time: the runner's DB sampler only sees its own audit connection (`connections max 1`, `pg_stat_statements` off), and Neon compute CPU is not scraped: **unknown**.
- Inventory row contention: stock reservations are spread over 50 SKUs and inventory handler p95 stayed at 0.02 s with queue depth ≈ 0; no evidence of a hot row, but no lock-wait sampling was done: **unknown**, low suspicion.

## 6. Overload recovery (o60, 60/s for 180 s)

Original state recorded before any repair (nothing was repaired or called): runner `pending_at_drain = 0`; orchestrator DB after the run: `COMPLETED / CONFIRM_STOCK = 10,801` and no other row (`data/o60/saga-state-after-drain.txt`); 0 dead outbox rows, 0 dead letters, 0 retry republishes. **Everything drained by itself, 87 s after offering stopped, inside the 120 s window.** There were no unfinished checkouts to group by step or to trace through outbox / DLQ / consumer logs / sweeper, so that trace was not performed. This was a 3-minute overload; it does not show behaviour of a much longer one.

## 7. Choke points (ranked)

| # | Choke point | Status | Evidence | Next measurement |
|---|---|---|---|---|
| 1 | payment-service webhook inbox: one serial processor, 7.8 ms/event, 128.7 events/s = 42.9 checkouts/s | **Confirmed** (first stage to fall behind; mechanism confirmed by 0.99 utilisation and the code) | o60: processed 126.8/s vs 178.2/s arriving; backlog 9,005; interval 5a 37.6 s avg; s40 processed = arrived (118.6/s). Payment CPU 22%, DB p95 ~0 | Rerun 60/s with a second payment-service process (same host or second host); expect ~2x capacity because the claim uses `SKIP LOCKED` + lease. `WEBHOOK_PROCESS_BATCH` will not help |
| 2 | orchestrator CPU | **Suspected** next limit | o60: 84% max, 55 s >70%, outbox pending 160 | Rerun 60-80/s after #1 is lifted; check CPU and reply-queue depth |
| 3 | Simulator pay-delay tail (lognormal σ=0.4) | **Confirmed**, not an application limit | p95 9.7 s ≈ 9.65 s predicted; flat across rates | None; label every latency number as simulator-shaped |
| 4 | Shared loadgen+simulator host (2 vCPU) | **Unknown** | Simulator emits ~178 webhooks/s at 60/s; its CPU and the generator's were not scraped (generator CPU 1.5% in dry10) | Scrape loadgen host CPU during the next run |
| 5 | DB pools (max 10), Neon compute, lock waits | **Unknown** | no pool or Neon series; only end snapshots with 0 waits | Add pool gauges, or sample `pg_stat_activity` on the service DBs |
| 6 | RabbitMQ broker | Unlikely | CPU ≤20%, queues ≤135, 0 dead letters | none now |
| 7 | Inventory row-level locks | **Unknown**, low suspicion | handler p95 0.02 s, queue ≈0 | lock sampler on `inventory` in the 60/s rerun |

## 8. Decision

**Test payment-service on two processes first; the limit is a serial loop, so a second replica should help (untested).**
- What stops progressing first is payment-service's webhook inbox, with idle CPU. The loop is single-goroutine, so more CPU or batch size will not help. Rows are claimed with `FOR UPDATE SKIP LOCKED` plus `workerID`/lease, so a second process should take about half the rows; that is the property the two-replica run must verify (and that no duplicate payment effect appears). A cheaper code-side alternative (handling a claimed batch with a few goroutines) is the user's call; neither has been tried.
- The orchestrator is the second candidate and the only one with sustained high CPU, but it did not cause the observed backlog.
- Sustainable rate with this setup: 40/s with flat latency (peak, one 3-minute run per rate); 60/s is accepted at 60/s but completes late and drains in 87 s. Not a long soak.
