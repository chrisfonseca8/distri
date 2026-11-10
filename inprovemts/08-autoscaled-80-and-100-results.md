# Autoscaled webhook workers + orchestrator pool at 80/s and 100/s — results and next choke (2026-11-09)

Same setup, settings, image (`*-dyn1`), method and windows as `07` (60 s warm-up, 180 s measured, drain to 120 s, 5 s polling, containers recreated before each run so each starts at 1 worker and a pool of 10). Commands: `inprovemts/scripts/sweep.sh 3.27.207.44 dyn80:80`, `... dyn100:100`, then `python3 inprovemts/scripts/choke_summary.py inprovemts/data/dyn80|dyn100`; scaler logs saved in `data/dyn80|dyn100/*-scaler.log`; phase 22 on the 100/s data.

## Comparison (measured window = 180 s after warm-up)
| Run | Offered | Accepted/s | Completed/s | Saga p50 / p95 / max | Peak RUNNING (window / warm-up) | Peak claimable webhook backlog (window / warm-up) | Drain after offering stops | Unfinished |
|---|---:|---:|---:|---|---:|---:|---:|---:|
| 50/s | 9,000 | 50.0 | 50.0 | 6.2 / 10.8 / 22.4 s | 367 | 27 / ~700 | 20 s | 0 |
| 60/s | 10,801 | 60.0 | 60.0 | 6.4 / 10.9 / 20.9 s | 491 | 20 / ~800 | 18 s | 0 |
| **80/s** | 14,400 | 80.0 | 80.0 | 6.5 / 11.6 / 22.1 s | ~914 / 1,897 | 21 / 4,072 | 19 s | 0 |
| **100/s** | 18,000 | 100.0 | 100.0 | 6.3 / 11.2 / 24.2 s | ~893 / 2,338 | 24 / 5,354 | 18 s | 0 |

0 rejected, 0 5xx, 0 timeouts. Phase 22 on the 100/s data: **PASS** (the 60/s data passed in `07`). The saga p95 stays at the simulator-shaped ~11 s up to 100/s inside the window, but the **warm-up shows a real transient** (below).

## What the autoscalers did
- **Webhook workers**, 80/s: 1 -> 2 at 16:59:18 (ready=2,002, oldest 7.9 s), cooldown held 4 samples while the backlog grew to ~4,000 (oldest 15-16 s), 2 -> 3 at 16:59:48. 100/s: 1 -> 2 at 17:05:28 (ready=2,390, oldest 6.9 s), cooldown held 6 samples while it grew to ~5,150 (oldest 17 s), 2 -> 3 at 17:06:03. Both held at **3** for the whole measured window and drained the backlog (clear before the window began). Scale-down 3 -> 2 happened at 17:10:38 (after the 100/s run). Utilisation of the active workers in the window: 66% at 80/s (3 workers, 8.3 ms/event) and **80% at 100/s** (2.40 busy-seconds per second over 3 workers, 8.1 ms/event); processed = arrived: 237/s and 298/s.
- **Orchestrator pool**, 80/s: 10 -> 15 at 17:00:53 (avg wait 84 ms, 2,322 blocked, in use 10/10), 15 -> 20 at 17:02:03 (avg wait 7.1 ms, in use 14/15). Waiting goroutines fell with each step: ~10 at cap 10, ~2 at cap 15, ~0.1 at cap 20 (blocked/s from ~800 to ~100). 100/s: 10 -> 15 at 17:05:38 (avg wait 10.4 ms, 6,020 blocked) during the warm-up; at 15 it settled (in use mean 8.9, avg wait 2.1 ms, below the 5 ms trigger), no further resize. Guard: held increases once at 80/s (probe latency 10 ms peak), never at 100/s; 0 ungranted locks; live connections to `orchestrator_db` 22 max against the budget of 100.

## The ramp-up transient (the real cost of reactive scaling)
Starting from 1 worker and a pool of 10 under a sudden 80-100/s load, the first minute is not clean:
| | 80/s | 100/s |
|---|---|---|
| time from load start to the first worker | ~20 s (the first two samples) | ~20 s |
| time to the third worker | ~50 s (30 s cooldown between additions) | ~55 s |
| peak claimable webhook backlog / oldest event | 4,072 / 16.0 s | 5,354 / 17.9 s |
| peak running sagas | 1,897 | 2,338 |
| orchestrator pool at that moment | 119.6 goroutines waiting for a connection in one 10 s sample (16:59:27), 10 conns, up to 1,874 blocked/s | up to 788 blocked/s, ~4.7 waiting |
| clear (ready backlog ~0) | ~17:00:17, ~80 s after load start | ~17:06:31, ~85 s |
This is not in the measured window, but it is the weak point of the current settings: one worker per 30 s cooldown against a 3x step. The measured 3-worker end state is correct; the path to it takes ~1 minute and builds a backlog of seconds-to-17 s old events plus a surge in orchestrator pool waits (through polling of the backlog). A shorter up-cooldown or adding more than one worker when the age is far above the threshold would shorten it; neither is implemented or tested.

## The next choke at 100/s (measured unless marked PREDICTED)
| # | Candidate | Numbers | Status |
|---|---|---|---|
| 1 | **Payment DB pool (10), not autoscaled** | in use hits 10 repeatedly in steady state (rows at 17:06:41, 17:07:11, 17:08:21 ...); within 1 of the cap in 35% (80/s) and 38% (100/s) of samples, up from 8% at 60/s; mean in use 6.95 / 6.84; blocked acquisitions 22,391 / 42,521 in the window (236/s at 100/s), total wait 41.8 s / 41.3 s (avg 1.9 / 1.0 ms, ~0.23 goroutines waiting on average); DB statements 2,279/s. It already has no headroom, though it has not hurt latency. The 3 webhook workers take up to 3 of the 10 connections. | **Confirmed as the tightest resource**, not yet a latency limit |
| 2 | Webhook workers (3 now, cap 6 by pool allowance) | 80% utilised at 100/s (8.1 ms/event; was 7.3-7.8 ms at lower rates); capacity of 3 ~370 events/s ~ 123 checkouts/s | PREDICTED 4th worker at ~115-125/s; the cap of 6 is bounded by payment pool headroom (see 1) |
| 3 | Orchestrator CPU | 55% -> 66% of a core mean (max 72%) from 80 to 100/s (+0.54 per checkout/s), host 49% mean / 54% max of 2 vCPU, 0 s above 70% on the host | PREDICTED ~100% of one core near 160/s; first container peaks above 70% already |
| 4 | Orchestrator pool (15-20, autoscaled) | mean in use 8.6-8.9; scaler handled it; ceiling 50, budget 100 | Under control; guard and budget not exercised |
| 5 | Neon compute CPU | **not measured** | **Unknown**: a Neon CPU graph for 16:58-17:10 UTC (10:28-10:40 PM IST) would settle it; the database is shared by all four services |
| 6 | Order-service command consumers | pinned at 4 max; at 100/s 199 msgs/s x 9.4 ms = 1.87 busy, avg 2.8 of 4 consumers = 68%; queue ready peaked 188 only during the warm-up ramp (consumer scaling lag), 127 at 80/s | **Not a choke** at 100/s; ~47% of the 4-consumer capacity (~425 msgs/s) |
| 7 | Orchestrator reply consumers | 65% at 100/s (avg 9.3 of max 30), 397 acks/s | Not a choke |
| 8 | Broker host | 26.6% mean / 29% max CPU | Unlikely |
| 9 | Load generator + simulator host | `vmstat`: 8.3% mean / 16% max (80/s), 8.1% / 16% (100/s) of 2 vCPU | Not a limit |

Answer to "where is the next choke": the **payment database pool**. It is the one resource already running full in steady state at 80 and 100/s, has no controller, and is the resource the webhook workers compete for as they scale. After it, the webhook workers (cap and per-event time), then orchestrator CPU. Neon CPU remains the unknown that could change the order.

## Staging state
Unchanged from `07`: payment and orchestrator hosts run the `dyn1` images with both autoscalers on; rollback instructions are in `07`. The six instances are still running.

## Limits
One run per rate; thresholds are defaults; the warm-up transient is shown from Prometheus series, not a separate experiment; Neon CPU not measured.
