# PAYMENT_WEBHOOK_WORKERS — change, local validation, and the staging A/B plan (not run)

Status 2026-11-09: implemented and validated **locally only**. Nothing deployed, no AWS change, no staging run, no commit/push.

## What changed

| File | Change |
|---|---|
| `payment-service/pkg/config/webhook_config.go` | `WebhookConfig.Workers`, env `PAYMENT_WEBHOOK_WORKERS`, default 1. Non-integer fails in `envInt` ("must be an integer"); `Validate()` rejects 0 and negatives ("must be greater than zero"). Startup fails, same as the other webhook settings. |
| `payment-service/pkg/services/webhook_processor.go` | `NewWebhookProcessors(db, cfg, n)` builds n independent processors over one pool (each gets its own `workerID`); `RunWebhookWorkers(ctx, processors)` runs one loop per processor in its own goroutine and returns only when all have stopped; start log now includes `worker_id`. The processing loop, claim query, batch, interval, lease and retry logic are unchanged. |
| `payment-service/cmd/payment-service/main.go` | builds the processors, attaches the shared compensation worker to each, logs `webhook workers configured workers=N`, runs them under the existing `workerWaitGroup` (shutdown waits for all, via the same `shutdownContext`). |
| `compose.cloud.yaml`, `compose.local.yaml` | pass `PAYMENT_WEBHOOK_WORKERS: "${PAYMENT_WEBHOOK_WORKERS:-1}"` to payment-service (without this line the variable never reaches the container). |
| tests | `pkg/config/webhook_config_test.go` (+2), `pkg/services/webhook_processor_loop_test.go` (+2), new `pkg/services/webhook_workers_integration_test.go` (+2) |

Unrelated, from the earlier step and still uncommitted: `tools/load-test/runner/config.go` (C4 honours `LOAD_DURATION`).

## Is two workers safe? (step 4; read from code, then tested)

- **Same row never handed to two workers:** the claim is `FOR UPDATE SKIP LOCKED` plus a lease (`locked_until`); each worker has its own `workerID`, and `finish`/`reschedule` update only `WHERE locked_by = <my id> AND status = 'PENDING'`, otherwise `errLeaseLost`. Distinct ids per worker matter: with one shared id, the lease-lost check could not tell sibling goroutines apart, which is why n separate processors are built instead of n goroutines on one.
- **Different events for the same payment, concurrently:** `apply` loads the attempt with `SELECT ... FOR UPDATE` (`webhook_processor.go`, "Lock the attempt"). The second event waits, then reads the committed row (READ COMMITTED re-reads the updated version), so it decides on current state. `payment.authorized` and `payment.failed` return "no state change" before any write, so a late authorized cannot move a captured payment backwards. A repeated capture hits "capture already applied", and `applyDecision` writes through `writePaymentResult(tx, id, currentStatus, ...)`, a guarded conditional update, so a lost race returns "not changed".
- **Duplicate effects:** compensation is `ON CONFLICT DO NOTHING` per (gateway, payment id); the saga reply is produced by the one winning status transition. Pre-existing tests already covered four concurrent `ProcessOnce` callers; the new tests cover the real loops.
- **Residual, not changed:** each pass runs `ReplayParked` (one idempotent UPDATE over parked rows) in every worker; with no parked rows it is a no-op, with parked rows two workers could briefly contend on the same rows. Cost: one extra cheap statement per pass per worker. Not exercised under load.
- No code change was needed to the state-transition logic.

## Local validation (throwaway `postgres:17-alpine` on 127.0.0.1:55432, db `distri_test`, schema from `go run ./cmd/migrate`; container removed afterwards)

```sh
cd payment-service
export DATABASE_TEST_URL='postgres://distri:distri@127.0.0.1:55432/distri_test?sslmode=disable&search_path=payments'
export RUN_ADMIN_INTEGRATION=1 RUN_CHECKOUT_INTEGRATION=1 RUN_INVENTORY_INTEGRATION=1 RUN_OUTBOX_INTEGRATION=1 RUN_PAYMENT_JOBS_INTEGRATION=1 RUN_REPLY_INTEGRATION=1 RUN_SWEEPER_INTEGRATION=1
gofmt -l . ; go vet ./... ; go test ./... -race -count=1 -p 1
```
Result: `gofmt` clean, `go vet` clean, every package `ok` with `-race`, **0 skipped** in `pkg/services` (the DB-backed tests ran). 18 webhook tests pass, including the 6 new ones, which were also run 3x in a row with `-race`:

| New test | What it proves |
|---|---|
| `TestWebhookWorkersDefaultToOneAndAcceptAPositiveInteger` | default 1, `2` accepted, batch/interval/lease unchanged |
| `TestWebhookWorkersRejectZeroNegativeAndNonIntegers` | `0`, `-1`, `abc`, `1.5`, `"2 "` fail startup |
| `TestRunWebhookWorkersStartsEveryWorkerAndWaitsForAllToStop` | N loops run concurrently; the call returns only after all (including a slow one) have stopped |
| `TestNewWebhookProcessorsGivesEachWorkerItsOwnLeaseIdentity` | distinct `workerID`s, shared settings, 0 workers / nil DB rejected |
| `TestTwoWebhookWorkersApplyEachPaymentOnce` | real loops, 2 workers, 30 payments x 5 events (+ late authorized/failed afterwards): every payment SUCCEEDED once, exactly one saga reply, no compensation, every event PROCESSED after exactly 1 claim, late authorized never regresses |
| `TestTwoWebhookWorkersNeverClaimTheSameRowTwice` | 60 duplicate captures on one payment: each row claimed once, one reply |

**Not done:** a negative control (breaking the attempt row lock to prove the new test fails). My first attempt did not compile and the second used an invalid lock clause, so it proved nothing; the file was restored and re-verified (`git diff` shows only the intended change). The race protection therefore rests on code reading plus tests that pass, not on a demonstrated failing mutation. Run time of the two DB tests is ~1.7 s, so they were not stressed beyond 30 payments.

## How to enable two workers (after deployment)

On the payment host's env file (`deploy/cloud/.env.payment` on the host; never printed here) set `PAYMENT_WEBHOOK_WORKERS=2`, then recreate only that container:
```sh
cd ~/distri && docker compose --env-file deploy/cloud/.env.payment -f compose.cloud.yaml up -d --force-recreate payment-service
docker logs --since 1m distri-staging-app-payment-service-1 2>&1 | grep -E 'webhook workers configured|webhook processor started'
# expect: workers=2 and two "webhook processor started" lines with different worker_id
```
Batch (100 deployed via compose), poll interval, lease and the DB pool (10) are not touched.

## Staging A/B plan (prepare only; needs a separate go-ahead: build + push image, deploy, run)

1. Build and publish one new image tag for payment-service containing this change (`deploy/cloud/scripts/build-push.sh`), deploy it with `deploy/cloud/scripts/deploy-staging.sh` to the payment host only. Both arms must run this same image; the earlier `09e4015` o60 run is not arm A.
2. **Arm A** `PAYMENT_WEBHOOK_WORKERS=1`; **Arm B** `PAYMENT_WEBHOOK_WORKERS=2`. Same payment container and EC2 host, same simulator (`SIM_PAY_DELAY=5s`), pools 10/10, uniform 50-SKU mix, same loadgen image `loadgen-09e4015-c4dur`, tunnels and Prometheus as before.
3. Each arm, from the laptop (waits for a quiet stack, then 60 s warm-up, 180 s at 60/s, drain to the 120 s workflow timeout):
```sh
inprovemts/scripts/sweep.sh <loadgen-ip> w1-60:60     # arm A
# switch the env var, recreate the container, confirm the two log lines, wait for quiet
inprovemts/scripts/sweep.sh <loadgen-ip> w2-60:60     # arm B
```
4. Per arm, compare (reuse tools in this folder):
   - webhook arrivals and processed events/s: `python3 inprovemts/scripts/prom_summary.py inprovemts/data/<label>` (and the `payment_webhook_events_processed_total` vs `http_requests_total{route="POST /v1/webhooks/razorpay"}` rates used in `02`, section 4);
   - peak pending (`payment_webhook_backlog`) and oldest pending age (`payment_webhook_oldest_pending_age_seconds`): the summary's backlog table;
   - processor busy-seconds per second: `rate(payment_webhook_processing_duration_seconds_sum[180s])` (arm B can exceed 1.0; the per-worker limit is 1.0);
   - DB pool acquisition waits: `db_pool.wait_count` / `wait_duration` per service in `load-test.json`. This is an **end-of-run snapshot only**; there are no pool gauges in Prometheus, so p95 acquisition time stays unknown unless a pool gauge is added first;
   - saga p95, drain time, unfinished at cutoff: `load-test.json` and `data/comparison.txt` method in `02`;
   - step intervals: `inprovemts/scripts/step_intervals.sql` through the psql container.
5. Phase 22 after each arm (the audit resets nothing with `CORRECTNESS_RESET_DATA=false`; run before the next arm's load run resets the data):
```sh
# on the loadgen host
CORRECTNESS_RESULTS_SUBDIR=cloud/inprovemts/<label>/audit PHASES=22 RUN_LABEL=inprovemts-<label> \
docker compose --env-file deploy/cloud/.env.loadgen -f deploy/cloud/compose.loadgen.yaml --profile correctness run --rm correctness
```
6. Decision rule (set before looking): B sustains the arrival rate if processed/s is within 3% of arrivals for the whole window, `payment_webhook_oldest_pending_age_seconds` max stays under the 40/s figure (~9 s) and does not trend upward, saga p95 is back near ~11 s, drain <= ~20 s, unfinished 0, and phase 22 passes. Expected capacity for 2 workers is ~2 x 128 = ~257 events/s (>= 178 needed) if the DB and pool keep up; this is a prediction, not a result. The next limit to watch is the orchestrator (84% CPU peak at 60/s in `02`).
