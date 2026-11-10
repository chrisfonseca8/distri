# Two independent opt-in autoscalers — implementation, local test results, and the four staging runs (not run)

Status 2026-11-09: implemented and tested **locally only**. Nothing deployed, no staging run, no AWS change, no commit/push. Both controllers are **off by default**.

## 1. What was built

### Payment webhook workers (payment-service, inside the existing process)
- `pkg/services/webhook_scaler.go`: `WebhookWorkerPool` (add / retire / count / busy-time), `WebhookScaler` (5 s sampler + decision), pure `webhookScaleController`, `NewWebhookSampler`, `EffectiveWebhookMaxWorkers`.
- `pkg/services/webhook_processor.go`: the existing loop now takes an optional per-worker stop signal (`runWebhookWorker`; `runWebhookProcessor` is unchanged for fixed use). A retiring worker is checked **before each claim**, so it finishes the batch it holds and claims nothing more. Each worker has its own `workerID` (lease identity).
- `pkg/businessmetrics/business.go`: the claimable-backlog SQL (`WebhookClaimable`), identical to the claim query's eligibility, used by the sampler and the exported gauges.
- `cmd/payment-service/main.go`: always runs through the pool; the scaler runs only when `PAYMENT_WEBHOOK_AUTOSCALE=true`.
- `pkg/config/webhook_config.go`: settings below. Invalid values fail startup, as for the other webhook settings; scaling settings are validated only when autoscaling is on.

**Decision rule.** Every `PAYMENT_WEBHOOK_SCALE_INTERVAL` (5 s) the scaler reads `ready` (claimable events) and `oldest_ready_age` (how long the oldest has been claimable) in one query.
- **Up (+1):** for `UP_SAMPLES` consecutive samples, `oldest_ready_age >= UP_AGE` and `ready >= UP_MIN_BACKLOG` and the backlog has not shrunk by more than 10% since the previous sample (persisting or growing). Then `UP_COOLDOWN` must have passed since the last addition. Never above the cap.
- **Down (-1):** for `DOWN_SAMPLES` consecutive samples, `ready <= DOWN_BACKLOG` and `oldest_ready_age <= DOWN_AGE` **and** the measured load fits the remaining workers: (busy seconds per second, from the workers' own apply time) / (workers - 1) <= `DOWN_UTILIZATION`. An empty inbox alone does not scale down (e.g. 2 workers each 0.69 busy = 1.38 work-seconds per second: refused). Unknown load also refuses. `DOWN_COOLDOWN` after any scale action; never below `MIN`.
- **Failed/missing sample:** count left unchanged, evidence streaks reset (fresh consecutive samples needed), `payment_webhook_scale_sample_errors_total` incremented.
- **DB-pool guard:** effective cap = min(`WORKERS_MAX`, `DB_MAX_OPEN_CONNS - POOL_RESERVE`), so workers cannot starve ingestion and consumers of connections. **With the current payment pool of 10 and the default reserve of 4, the effective cap is 6, not 10** (logged at start). Reaching 10 needs `DB_MAX_OPEN_CONNS >= 14` on the payment service.

### Orchestrator database pool (orchestrator-service)
- `pkg/poolscaler/poolscaler.go`: `Scaler`, pure `controller`, metrics. `pkg/poolscaler/guard.go`: `DBGuard`, `ConnectionErrorCounter`.
- `pkg/config/pool_scaler_config.go`: settings below; `Ceiling()`; `OpenProbeDB()` in `database_config.go` (a separate 1-connection handle for the guard).
- `cmd/orchestrator-service/main.go`: built and fail-fast validated at start; run only when `ORCHESTRATOR_DB_POOL_AUTOSCALE=true`.

**Decision rule.** Every `ORCHESTRATOR_DB_POOL_INTERVAL` (5 s) from `DB.Stats()`: `blocked = ΔWaitCount`, `avg_wait = ΔWaitDuration / ΔWaitCount` (acquisition wait only; SQL execution time is not in these numbers), `in_use`, `cap`.
- **Up (+5, clamped to the ceiling):** for `UP_SAMPLES` consecutive samples, `blocked >= UP_MIN_WAITS` and `avg_wait >= UP_WAIT` and `in_use >= UP_NEAR_FULL x cap`; `UP_COOLDOWN` since the last increase. `SetMaxOpenConns` and `SetMaxIdleConns` are set together (an idle limit below the cap would churn connections).
- **Ceiling:** min(hard 50, `POOL_MAX`, (`CONNECTION_BUDGET` - `BUDGET_RESERVED`) / `REPLICAS`). The budget is **required** when autoscaling is on.
- **Pause increases (guard)** while any holds: database round trip of `SELECT 1` on the guard's own connection > `GUARD_LATENCY_FACTOR` x a baseline of healthy probes (or > `GUARD_LATENCY_MAX`); `pg_locks` ungranted >= `GUARD_LOCK_WAITERS`; >= `GUARD_CONN_ERRORS` new connection-type errors (`db_errors_total` kinds connection, timeout, pg_08*, pg_53*, pg_57p*); live connections to the database + step > `CONNECTION_BUDGET` (`pg_stat_activity`, all clients). An unreadable probe holds increases too.
- **Down (-5, never below the starting `DB_MAX_OPEN_CONNS`):** `DOWN_SAMPLES` consecutive samples with `in_use <= DOWN_USAGE x cap` and zero blocked acquisitions; `DOWN_COOLDOWN` after any resize.
- Missing statistics or backwards counters: pool unchanged, streaks reset.

**Independence.** The two controllers live in different services and packages, share no code, and neither reads the other's metrics or settings. The orchestrator scaler reads only its own pool and database health; the webhook scaler reads only the inbox and its own workers' busy time.

### Metrics exported
Webhook (payment-service): `payment_webhook_workers_active`, `payment_webhook_ready_backlog`, `payment_webhook_oldest_ready_age_seconds`, `payment_webhook_workers_busy_ratio`, `payment_webhook_scale_events_total{direction}`, `payment_webhook_scale_sample_errors_total` (the first three are exported even with autoscaling off, so the fixed arm is measurable). Pool (orchestrator, only when enabled): `orchestrator_db_pool_cap`, `_in_use`, `_ceiling`, `_acquire_wait_seconds` (average per blocked acquisition), `_blocked_acquisitions_per_second`, `_scale_up_held`, `orchestrator_db_pool_scale_events_total{direction}`, `orchestrator_db_probe_latency_seconds`, `_probe_baseline_seconds`, `orchestrator_db_ungranted_locks`, `orchestrator_db_live_connections`, `orchestrator_db_pool_guard_errors_total`. Every scale decision is logged with the triggering numbers (`webhook workers scaled`, `database pool resized`, plus `... holding` when blocked).

**Correction to my earlier notes:** pool gauges did exist all along as `go_sql_in_use_connections`, `go_sql_open_connections`, `go_sql_max_open_connections`, `go_sql_wait_count_total`, `go_sql_wait_duration_seconds_total` (the orchestrator registers Go's DBStatsCollector; my metric listing hid names starting with `go_`). From them, the existing 60/s run shows the orchestrator pool at >= 9 in use in 20 of 54 samples (mean 6.1) against 5 of 39 at 40/s; the payment pool mean was 3.2 in both. So "no pool gauge" in `04`/`05` is wrong; `go_sql_*` can be used for p95-style pool analysis in the next runs.

## 2. Local test results (throwaway `postgres:17-alpine`, removed afterwards)
- `gofmt` clean, `go vet ./...` clean in both services.
- payment-service: `go test ./... -race -count=1 -p 1` **all packages ok**; 92 tests in `pkg/services` + `pkg/config` pass, 0 skipped (database tests ran).
- orchestrator-service: `go test ./... -race -count=1 -p 1` **all packages ok** (its own DB-gated integration tests skip because only the guard's DB needs were loaded; the new pool-scaler database tests ran and passed).
- New tests, payment: scale-up needs age + non-shrinking backlog for consecutive samples (and ignores young, tiny and draining backlogs); cooldown spacing (>= 30 s between additions); hard cap and the DB-pool allowance (`EffectiveWebhookMaxWorkers`); scale-down needs sustained low demand and capacity for the load, refuses with an empty inbox when remaining workers could not carry the load, respects min and cooldown; missing sample leaves the count and restarts evidence; pool add/retire/never-below-one; **a retiring worker finishes its batch and claims nothing more**; scaler loop adds after consecutive pressure then holds in cooldown; failed samples leave the count; config defaults/validation. Database-backed: the sampler counts exactly what the claim query could claim (run side by side over ready / expired-lease / live-lease / future / processed / parked rows); **40 payments x 5 events (+ late authorized/failed after) processed while workers are added and retired every 15 ms: every payment settles once, one saga reply, no compensation, each event claimed exactly once, late authorized never regresses** (run 3x under `-race`).
- New tests, orchestrator: avg wait is computed from interval deltas (a huge lifetime wait history does not hide or fake pressure); steps of 5; needs high wait AND enough blocked acquisitions AND near-full; cooldown; ceiling = min(hard 50, configured max, budget share) including the clamped last step; guard pauses (slow database, locks, connection errors, unreadable) and needs fresh evidence after release; gradual scale-down to the start size; cooldown; missing stats and backwards counters; `Stats`/resize loop on a fake pool (open and idle set together); config defaults, required budget, replicas, 13 invalid settings. Database-backed: guard against a real database (latency baseline, a real waiting lock, the connection budget) and **a real `*sql.DB` under contention grows 5 -> 10 -> 15 -> 20 and stops at the ceiling of 20**.
- A test bug found and fixed during the run (not a product bug): a flaky "retiring worker" test numbered workers by goroutine start order; it now numbers them at creation (re-run 40x under `-race`, clean).

## 3. Staging settings for the four runs at 60 checkouts/s

All four runs: the same new image tag (payment-service and orchestrator-service built from this tree; build/push/deploy is a separate step), same simulator (`SIM_PAY_DELAY=5s`, seed 1), same 50-SKU mix, `loadgen-09e4015-c4dur`, 60 s warm-up, 180 s at 60/s, drain to the 120 s workflow timeout, `LOAD_STATUS_POLL_INTERVAL=5s` in all four (so polling stops dominating the orchestrator pool; this differs from the earlier 2 s runs and from `o60`, which is therefore not a comparison arm). Between runs: recreate both containers so each starts from its configured start size, wait for a quiet stack (the driver does), keep the same Neon compute (it autosuspends; warm it first).

Where to set them: payment settings in `deploy/cloud/.env.payment` on the payment host, orchestrator settings in `deploy/cloud/.env.orchestrator` on the orchestrator host (both pass through `compose.cloud.yaml`). Apply with, on each host:
```sh
cd ~/distri && docker compose --env-file deploy/cloud/.env.<payment|orchestrator> -f compose.cloud.yaml up -d --force-recreate <payment-service|orchestrator>
```
Common to every run (unchanged from today): payment `DB_MAX_OPEN_CONNS=10 DB_MAX_IDLE_CONNS=10 WEBHOOK_PROCESS_BATCH=100`; orchestrator `DB_MAX_OPEN_CONNS=10 DB_MAX_IDLE_CONNS=10 ORCHESTRATOR_CONSUMER_MAX_CONCURRENCY=30`.

| Setting | Run 1 `fixed` | Run 2 `webhook` | Run 3 `pool` | Run 4 `both` |
|---|---|---|---|---|
| payment `PAYMENT_WEBHOOK_AUTOSCALE` | false | **true** | false | **true** |
| payment `PAYMENT_WEBHOOK_WORKERS` (start) | 1 | 1 | 1 | 1 |
| payment `PAYMENT_WEBHOOK_WORKERS_MIN` / `_MAX` | n/a | 1 / 10 (effective cap 6 with a pool of 10; see above) | n/a | 1 / 10 |
| payment other scale settings | n/a | defaults: interval 5s, up age 2s, up min backlog 10, up samples 2, up cooldown 30s, down samples 12, down cooldown 60s, down utilization 0.6, pool reserve 4 | n/a | same as run 2 |
| orchestrator `ORCHESTRATOR_DB_POOL_AUTOSCALE` | false | false | **true** | **true** |
| orchestrator pool start | `DB_MAX_OPEN_CONNS=10` | 10 | 10 | 10 |
| orchestrator `ORCHESTRATOR_DB_POOL_MAX` | n/a | n/a | 50 | 50 |
| orchestrator `ORCHESTRATOR_DB_CONNECTION_BUDGET` / `_RESERVED` / `ORCHESTRATOR_REPLICAS` | n/a | n/a | 100 / 10 / 1 (ceiling 50) | 100 / 10 / 1 |
| orchestrator other pool settings | n/a | n/a | defaults: interval 5s, step 5, up wait 5ms, up min waits 10, up near full 0.8, up samples 2, up cooldown 30s, down usage 0.3, down samples 24, down cooldown 60s, guard latency factor 3 / max 100ms, lock waiters 5, conn errors 3 | same as run 3 |

The budget of 100 with 10 reserved is an assumption for the shared Neon compute (`max_connections` is 901; the other services hold 10 each plus the audit and admin connections), not a measured limit; set it from the real allowance before use. The thresholds are starting guesses derived from the 60/s run (orchestrator average wait was 6.8 ms there, 4 ms at 40/s), not tuned values.

Run each (laptop):
```sh
inprovemts/scripts/sweep.sh <loadgen-ip> fixed:60     # then edit env, recreate containers, confirm startup log lines
inprovemts/scripts/sweep.sh <loadgen-ip> webhook:60
inprovemts/scripts/sweep.sh <loadgen-ip> pool:60
inprovemts/scripts/sweep.sh <loadgen-ip> both:60
```
(`sweep.sh` uses `LOAD_STATUS_POLL_INTERVAL=2s` today via `~/run-inprovemts.sh` on the loadgen host; change that variable to `5s` there before the first run so all four match.) Startup log lines to confirm per run: payment `webhook workers configured ... autoscale=<bool>` and, when on, `webhook autoscaler started ... max=6`; orchestrator `database pool autoscaler started ... ceiling=50`. Phase 22 after each run:
```sh
CORRECTNESS_RESULTS_SUBDIR=cloud/inprovemts/<label>/audit PHASES=22 RUN_LABEL=inprovemts-<label> \
docker compose --env-file deploy/cloud/.env.loadgen -f deploy/cloud/compose.loadgen.yaml --profile correctness run --rm correctness
```

## 4. What to record per run (same timeline as before)
Load generator and saga: offered/accepted/completed per second, saga p95, drain time, unfinished at cutoff (`load-test.json`, `scripts/prom_summary.py`). Webhook: `payment_webhook_workers_active`, `..._ready_backlog`, `..._oldest_ready_age_seconds`, `..._workers_busy_ratio`, processed vs received per second, time from the first pressured sample to each worker addition (log lines `webhook workers scaled`). Pool: `orchestrator_db_pool_cap`, `go_sql_in_use_connections`, `orchestrator_db_pool_acquire_wait_seconds`, `go_sql_wait_count_total` and `go_sql_wait_duration_seconds_total` rates, guard metrics, time to each resize (`database pool resized`). Other services' pool use (`go_sql_*` for payment, inventory, order) to see where the load moves. Neon console CPU for the window (the compute is shared by all four databases).

Questions the four runs answer: (1) does webhook scaling alone remove the 37 s capture-to-finish wait; (2) does pool scaling alone change anything while the inbox is the limit (expected: little, because most of the pool pressure came from polling of the backlog); (3) does enabling both beat either; (4) do the controllers interfere (watch the guard metrics and the payment pool while the worker count rises).

## 5. Known limits
- Not run on staging; thresholds are guesses; the effective webhook cap is 6 on a 10-connection payment pool.
- The pool scaler's lock check is `SELECT count(*) FROM pg_locks WHERE NOT granted`, which is not filtered by database, so on the shared Neon compute a lock wait in another service's database can also pause increases. That errs on the safe side (fewer increases), but it can hold the pool down for a reason unrelated to the orchestrator.
- `SetMaxOpenConns` lowering below the current in-use count lets the extra connections finish and close as they are returned; there is no forced close.
- The load generator's polling still loads the orchestrator pool; use the 5 s interval in all arms.
