# Runs so far (2026-11-09) — reported from `load-test.json`, verified against the files in `data/`

Target verified before traffic: Neon `ep-falling-tree-a74c336v` (confirmed disposable by the user), image tag `09e4015` on all four services, simulator `SIM_PAY_DELAY=5s`, `SIM_API_LATENCY=zero`. See `00-preflight-and-plan.md`.

Command (loadgen host, `~/run-inprovemts.sh <label> <rate> <warmup> <duration> <test-run-id>`; wrapper around):
```sh
TEST_RUN_ID=<id> RESULTS_SUBDIR=cloud/inprovemts/<label>-<ts> LOAD_CASE=C4 LOAD_RATE_STEPS=<rate> LOAD_RUNS=1 \
LOAD_WARMUP=<warm> LOAD_DURATION=<dur> LOAD_WORKFLOW_TIMEOUT=120s LOAD_STATUS_POLL_INTERVAL=2s LOAD_MAX_CONNS=1000 \
docker compose --env-file deploy/cloud/.env.loadgen -f deploy/cloud/compose.loadgen.yaml --profile loadtest run --rm loadtest
```

| Run | Rate | Warm-up | Measured window | Offered / accepted / completed | Accepted/s | Completed/s | E2E p95 | Server saga p95 | Unfinished | Notes |
|---|---:|---|---|---|---:|---:|---:|---:|---:|---|
| dry10 | 10 | 10 s | **30 s** | 300 / 300 / 300 | 10.0 | 10.0 | 12.06 s | 10.68 s | 0 | dry run (not part of the sweep) |
| s20 | 20 | 60 s | **30 s** | 600 / 600 / 600 | 20.0 | 20.0 | 12.06 s | 10.31 s | 0 | **window 30 s, not the 3 min requested** |

Per-service, same runs (runner snapshot; container CPU is a % of one core as reported by Docker, mean / max over the window):

| Service | dry10 CPU | s20 CPU | dry10 p95 handler | s20 p95 handler | s20 pool at end (open/in-use/wait_count) |
|---|---|---|---:|---:|---|
| orchestrator | 9.6 / 18.9 | 15.2 / 28.8 | 34.8 ms | 37.7 ms | 10 / 0 / 0 |
| payment | 5.6 / 12.3 | 8.5 / 18.0 | 6.5 ms | 7.0 ms | 10 / 0 / 0 |
| inventory | 3.3 / 8.8 | 5.5 / 13.3 | 17.8 ms | 23.0 ms | 4 / 0 / 0 |
| order | 3.6 / 8.6 | 4.9 / 11.2 | – | – | 6 / 0 / 0 |

Reading (reported, not yet a conclusion): at 10-20/s nothing is anywhere near saturated (CPU ≤ 29% of one core on a 2-vCPU host, zero pool waits, zero 5xx). The server-side saga p95 is about 10.3-10.7 s at both rates even though the simulator's pay delay is 5 s; the extra ~5 s is the open question to split with the step timestamps. A run whose saga minimum (2.65 s in dry10) is below the 5 s pay delay also needs explaining.

## Problems found in the tooling (not fixed — they are the user's code)

1. **`LOAD_DURATION` is ignored by case C4.** `tools/load-test/runner/config.go` only reads it inside `if c.CaseName == "C2"`; C4 is fixed at 30 s. A 3-minute open-loop window needs either a small change there (accept `LOAD_DURATION` for C4) and a rebuilt/pushed loadgen image, or something else. Until then every sweep point is a 30 s window, which is too short to show a backlog growing slowly at 30-40/s.
2. The runner's DB sampler connects to the audit database (`distri_audit`) with one connection (`connections max = 1`), so DB-side lock waits / active connections in `load-test.json` describe the audit connection, not the service databases. `pg_stat_statements` is unavailable. Pool numbers in the report are a single end-of-run snapshot, not a series.
3. My sweep driver (`inprovemts/scripts/sweep.sh`) hangs when an SSH attempt times out (the laptop's egress IP rotates between addresses not all in the security groups). It needs retries before it is trusted for unattended runs.
