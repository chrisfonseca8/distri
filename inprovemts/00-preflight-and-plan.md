# Staging bottleneck diagnosis — preflight and run plan

Date: 2026-11-09. Status: **no traffic sent yet.** Nothing in AWS changed.
Scope: staging with the simulated gateway, 5 s pay delay. The earlier results
(`tests/load-test/04-rate-sweep-open-loop.md`, 2026-11-01) stay separate; they are the prior, not part of this experiment.

## What exists (reused, nothing new built)

| Need | Existing tool |
|---|---|
| Open-loop rate steps | `LOAD_CASE=C4` with `LOAD_RATE_STEPS=20,30,40` / `60` (`tools/load-test/runner/config.go`) |
| Cloud runner | `deploy/cloud/compose.loadgen.yaml` on the load-gen host (`notes/run-cloud-test.txt`), `tools/cloud/cloud-test.sh` |
| Run timeline + metrics export | `tools/cloud/cloud-test.sh new/fetch` -> `deploy/observability/scripts/obs-run.sh`, `obs-export-run.py` (Prometheus, Loki, Grafana) |
| Per-step saga timing | `orchestrator.saga_steps` has `started_at`, `finished_at`, `attempts`, `status`; `orchestrator.sagas` has `created_at`, `updated_at`, `status`, `current_step` |
| Queue-wait / critical path | `tools/diagnostics/choke_report`, `tools/diagnostics/critical_paths` (read these tables) |
| Outbox backlog + age | gauges `outbox_pending_rows`, `outbox_oldest_pending_age_seconds`, `outbox_dead_rows` |
| Broker / pool / handler metrics | `rabbitmq_consume_*`, `rabbitmq_dead_letters_total`, `http_request_duration_seconds`, `db_query_duration_seconds`, `db_transactions_total`, DB pool gauges; alert `DatabasePoolSaturated` |
| Invariants after a run | `PHASES=22` correctness audit |
| Lock sampling | `tools/diagnostics/orchestrator_lock_sampler.sql` |

## Gaps to confirm on the live stack before the first run (measure before instrumenting)

1. Whether the pinned images expose a **webhook inbox** pending/oldest-age gauge (payment-service) — only the orchestrator outbox gauge is confirmed in source.
2. Whether `distri.orchestrator.replies` ready/unacked is scraped (RabbitMQ management or exporter) — check the Prometheus targets.
3. p95 pool *acquisition* time: the code exposes `WaitCount/WaitDuration` (mean wait, not p95). If only that exists, p95 is "unknown" for pool wait and the report must say so.
4. Inventory row-level lock waits: use `pg_locks` sampling per run (`orchestrator_lock_sampler.sql` pattern, adapted for the inventory schema) — a sampler only, no code change.
5. The C4 runner's **drain is `DrainTimeout=15s` hard-coded** (`config.go:82`) and the default completion mode polls every 100 ms. The requested 2-minute drain and low polling need `LOAD_STATUS_POLL_INTERVAL=2s` and `LOAD_WORKFLOW_TIMEOUT`; if the runner does not honour a longer drain, completion is taken from the saga tables by `choke_report`, not from the client. Decide after one dry run at 10/s.

## Run order (all with the same image tags, pool sizes, simulator seed/settings, uniform mix over 50 SKUs)

| Run | Case | Rate | Warm-up | Measure | Drain | Rest between |
|---|---|---|---|---|---|---|
| S20 | C4 | 20/s | 60 s | 180 s | 120 s | until queues=0, outbox=0 |
| S30 | C4 | 30/s | 60 s | 180 s | 120 s | same |
| S40 | C4 | 40/s | 60 s | 180 s | 120 s | same |
| O60 | C4 | 60/s | 60 s | 180 s | defined after S-runs | do **not** repair before recording state |

Command shape (from `notes/run-cloud-test.txt`, on the load-generator host):

```sh
RUN_NAME="cloud/inprovemts/s30-$(date -u +%Y%m%dT%H%M%SZ)"
RESULTS_SUBDIR="$RUN_NAME" LOAD_CASE=C4 LOAD_RATE_STEPS=30 LOAD_RUNS=1 \
LOAD_WARMUP=60s LOAD_DURATION=180s LOAD_WORKFLOW_TIMEOUT=120s \
LOAD_STATUS_POLL_INTERVAL=2s LOAD_MAX_CONNS=1000 \
LOAD_TEST_ENVIRONMENT=inprovemts-s30 \
docker compose --env-file deploy/cloud/.env.loadgen -f deploy/cloud/compose.loadgen.yaml run --rm loadtest
```
(Exact values get recorded per run in this folder once executed; the above is a plan, not a result.)

## Blockers needing the user's decision

1. **All six staging instances are `stopped`.** Starting them is an AWS change and costs money (6 x c7i/m7i.large, roughly 0.5 USD/h combined in Sydney — estimate, check current pricing). Public IPs are not Elastic, so `ip_details.txt` is stale after start.
2. **The runner truncates tables.** It refuses to run unless `LOAD_TEST_ALLOWED_DB_HOST` equals the Neon host. I need you to confirm the Neon branch behind `.env.loadgen` is the disposable one (I cannot see which branch it is without reading the secret file; I will read only the host name).
3. Free-tier Neon storage is capped (512 MB per the project notes). Four runs at up to 60/s (about 10k checkouts each) may fill it; I will record DB size before/after each run and clean between runs.

## After the sweep (not started)

Two-machine comparison, ALB, ASG min 1 / max 2 and scale-in verification all change AWS resources. **I will stop for your approval before any of it.**

## Preflight findings, 2026-11-09 (read-only checks; secrets not read)

| Check | Result |
|---|---|
| EC2 state | all 6 instances `stopped`; nothing started yet |
| Laptop public IP | <laptop-egress-ip> — **not** in the SSH allow-list of any of the 3 security groups (broker, loadgen, services). SSH would time out. |
| Neon target | **Ambiguous.** Local per-host env files (`.env.orchestrator/order/inventory/payment/loadgen`, dated 2026-10-29) point at Neon project `ep-red-fog-a7jcox3d` (ap-southeast-2). `.env.databases` (modified today, 01:09) points at a *different* project `ep-falling-tree-a74c336v`. `.env.databases.old-neon` points back to red-fog, so red-fog was renamed "old". `.env.cloud` (2026-10-20) names a third host `ep-dark-fog-b7b0feo1` (us-east-1) and tag `2026-10-20`. |
| Image tag in local host env files | `f4ebe64-working` (the deployed tag is unverified until read from the hosts) |
| DB pool | `DB_MAX_OPEN_CONNS=10` in `.env.cloud` |

Conclusion: which database each host really uses cannot be proved from the laptop. The hosts' own env files are the truth (`deploy-staging.sh --check` verifies them). No traffic until the hosts' DB host, image tag and `LOAD_TEST_ALLOWED_DB_HOST` are shown to agree and to be the disposable branch.

## Environment verified, 2026-11-09 (after the user confirmed `falling-tree` is disposable)

- Instances started (broker, orchestrator, order, inventory, payment, loadgen). Laptop egress IP rotates between <laptop-egress-ip>, <laptop-egress-ip> and others; all three SGs already allow <laptop-egress-ip>, so **no security-group change was made**. SSH times out intermittently when the egress IP is not in the list; retry succeeds.
- Running containers (read with `docker inspect`, secrets not printed): orchestrator, order, inventory, payment images all `*-09e4015`; every service's DB URL host is `ep-falling-tree-a74c336v.ap-southeast-2.aws.neon.tech`; loadgen `.env.loadgen` has `LOAD_TEST_ALLOWED_DB_HOST` = same host, `IMAGE_TAG=09e4015`; simulator `SIM_PAY_DELAY=5s`, `SIM_API_LATENCY=zero`, image `gateway-simulator-09e4015`.
- Settings recorded for every run: `DB_MAX_OPEN_CONNS=10`/`DB_MAX_IDLE_CONNS=10` on all four services; orchestrator consumer concurrency 1 -> max 30 (autoscale, 5 s interval, 20 msgs/worker); inventory consumer 1 -> max 4, prefetch 10; payment consumer 1 -> max 4, prefetch 10, `WORKER_CONCURRENCY=4`; `CONSUMER_MAX_ATTEMPTS=5`.
- Telemetry: SSH tunnels up (`obs-tunnels.sh`), `hosts.env` updated to the new public IPs (git-ignored). `prometheus/targets/` is root-owned (created by Docker) and sudo needs a password, so the target files are written through a root throwaway container; all 14 targets report `up`.
- Metrics present: `saga_step_duration_seconds`, `saga_duration_seconds`, `sagas_by_current_step`, `saga_steps_by_status`, `saga_oldest_active_age_seconds`, `outbox_pending_rows`, `outbox_oldest_pending_age_seconds`, `payment_webhook_backlog`, `payment_webhook_oldest_pending_age_seconds`, `rabbitmq_detailed_queue_messages_ready/unacked` for all 17 queues (including `distri.orchestrator.replies`), `rabbitmq_dead_letters_total`, `rabbitmq_consume_duration_seconds`, `http_request_duration_seconds`, `db_query_duration_seconds`, `db_transactions_total`, container CPU (cAdvisor) and node CPU.
- **Gap found: no DB pool gauges** (open / in-use / wait count / wait duration) in Prometheus. Pool in-use vs max and p95 acquisition time are therefore **unknown** from Prometheus; the load-test runner's service snapshot (`sql.DB.Stats()`) and `pg_stat_activity` are the fallbacks. Not instrumenting until a run shows pool pressure matters.
- Output of one command exposed the simulator's test-mode webhook/key secrets in this session's terminal (env dump of the payment container). They are staging/simulator values, not Razorpay credentials; rotate if the transcript is shared.
