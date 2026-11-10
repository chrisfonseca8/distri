# Faster webhook scale-up + payment DB pool autoscaler — 100/s rerun (2026-11-09)

## Code changes (uncommitted)
- Webhook scaler: scale-up cooldown was already `PAYMENT_WEBHOOK_SCALE_UP_COOLDOWN`; staging now sets **10s** (default stays 30s). Still one worker per step, same age/backlog conditions, max 10, scale-down unchanged. The worker cap now follows the payment pool's *current* limit every sample: `min(10, pool_limit - PAYMENT_WEBHOOK_POOL_RESERVE(4))` (`WebhookScaler.WithPoolLimit`, `webhookScaleInput.MaxWorkers`). Pool 10 -> cap 6, 15 -> 10, 20 -> 10. Logged as "webhook worker cap changed with the database pool".
- Payment pool autoscaler: `payment-service/pkg/poolscaler` is a copy of the orchestrator's package (services are separate Go modules, shared code is copied) with its own settings `PAYMENT_DB_POOL_*`, `PAYMENT_DB_CONNECTION_BUDGET(_RESERVED)`, `PAYMENT_REPLICAS` and metrics `payment_db_pool_*` / `payment_db_*`. Same rule: increase only when in-use >= 80% of cap AND avg wait per blocked acquisition >= 5 ms with >= 10 blocked, for 2 consecutive 5 s samples; guard (probe latency >3x baseline or >100 ms, >=5 ungranted locks, >=3 connection errors, connections+step > budget, unreadable probe) holds increases; step 5; down after 24 low samples; every change logged with the reason. Opt-in (`PAYMENT_DB_POOL_AUTOSCALE=true`).
- Tests: `go test -race ./pkg/poolscaler ./pkg/config ./pkg/services` in payment-service pass (new test: cap follows the pool). `compose.cloud.yaml` / `compose.local.yaml` pass the new variables.

## Staging settings for this run
Payment host (`payment-service-dyn2`, orchestrator unchanged on `dyn1`): `PAYMENT_WEBHOOK_AUTOSCALE=true`, `PAYMENT_WEBHOOK_SCALE_UP_COOLDOWN=10s` (rest default: start 1, max 10, interval 5 s, up age 2 s, up backlog 10, up samples 2, down samples 12 / cooldown 60 s / util 0.6, reserve 4); `DB_MAX_OPEN_CONNS=10`; `PAYMENT_DB_POOL_AUTOSCALE=true`, `PAYMENT_DB_POOL_MAX=20`, step 5 (default), `PAYMENT_DB_CONNECTION_BUDGET=100`, `_RESERVED=10`, `PAYMENT_REPLICAS=1` -> ceiling min(20, 90) = 20; up cooldown 30 s default. Orchestrator unchanged (pool 10 -> max 50, budget 100). Both containers recreated before the run; same runner, 60 s warm-up + 180 s window, 5 s polling, 5 s simulator delay, same mix.
Backups on the payment host: `compose.cloud.yaml.bak-dyn2`, `deploy/cloud/.env.payment.bak-dyn2`. Rollback: restore both and `up -d --force-recreate --no-deps --pull never payment-service`.

## Invalid first attempt (kept, labelled)
`data/dyn2-100-INVALID-not-fresh`: an earlier launch of the sweep eventually got through its SSH retries and ran at ~19:12 unnoticed, so the "measured" run at 19:17 started on already-scaled containers. Discarded; containers recreated and the run repeated once: `data/dyn2-100`.

## Result (fresh start; load start 19:24:49Z, measured window from +85 s)
| | previous 100/s (09 not applied) | this run |
|---|---|---|
| time to worker 2 / worker 3 | ~20 s / ~55 s | **20 s / 30 s** (4th worker at 40 s) |
| peak claimable ramp backlog | 5,354 | **2,483** (at +30 s) |
| oldest claimable webhook wait | 17.9 s | **8.6 s** |
| backlog < 20 | ~85 s after load start | **+65 s** |
| peak RUNNING sagas in ramp | 2,338 | 1,854 |
| steady completed/s | 100.0 (18,000/18,000) | **100.0 (18,000/18,000), 0 pending, 0 cancelled** |
| steady saga p50 / p95 / p99 / max | 6.3 / 11.2 / - / 24.2 s | **6.2 / 10.9 / 14.0 / 24.0 s** |
| steady workers | 3 | 4 (held; scale-down refused: 3 workers would run at 0.71-0.94 > 0.60) |
| orchestrator pool | 10 -> 15 | 10 -> 15 at +54 s |
| payment pool | 10, no scaler | **10 -> 10, no resize** |

Ramp saga p95 for checkouts accepted in the first minute: **not measured exactly** — the runner clears the saga tables after warm-up, so that cohort is not in the DB, and the Prometheus histogram buckets are coarse (10/30 s). Evidence from the histogram of completions in the first 85 s: 9.7% finished <= 10 s (steady: 92%), 99.9% <= 30 s, so the ramp p95 lies in the 10-30 s bucket (interpolated ~29 s; treat as an estimate). The previous run has no first-minute figure, so there is no like-for-like comparison of that number.

Webhook scaler timeline: 19:25:06 -> 2 (ready 1,792, oldest 5.4 s), 19:25:16 -> 3 (2,483 / 8.6 s), 19:25:26 -> 4 (2,513 / 8.2 s); then holding at 4 (busy 1.8-2.8 s/s over 4 workers ~ 46-70%), scale-down to 3 at 19:30:21 after the run. The 10 s cooldown overshot by one worker relative to the previous end state (3), because the third and fourth were added before the second's effect showed in the backlog; held at 4 harmlessly.

Payment pool: cap stayed 10, **trigger not met**. Over 54 samples (ramp + window): near-full (>=80%) in 20, elevated wait (avg >= 5 ms with >= 10 blocked) in 1 (33 ms once), both in 0; longest run meeting both = 0 (needs 2). Max in-use 10, max 602 blocked/s, window total 54,147 blocked acquisitions, 60.9 s total wait, avg 1.12 ms per blocked acquisition. Guard held in 1 sample. So the pool is often full, but acquisitions wait ~1 ms, and the scaler correctly did not grow it. Because the pool stayed at 10 the webhook cap stayed 6 (never reached).

Audit: phase 22 on this run **PASS** (`data/dyn2-100/audit/correctness-test.md`, second run entry `inprovemts-dyn2-100`; all assertions violations=0). Same caveat as before: its "background jobs drain" assertion reports PASS with the message "timed out waiting for the worker backlog".

Other: orchestrator CPU 63% mean / 72% max of one core (host 48%), payment 36%, broker host 26%; webhook arrivals = processed = 292/s; unfinished after drain 0.

## What it means
- The 10 s cooldown cut the ramp backlog by 54%, the oldest wait by 52%, and brought worker 3 forward from ~55 s to 30 s; the steady state is unchanged (p95 10.9 s ~ the simulator floor).
- The payment pool autoscaler did nothing at 100/s because waits are ~1 ms — it is untested under real pressure in staging. To see it act it needs a higher rate (or a smaller starting pool).
