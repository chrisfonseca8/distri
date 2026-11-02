# Test 7 — bottleneck experiment (final result)

**Date:** 1 Nov 2026, 20:33–21:21 UTC · **Environment:** `staging` (ap-southeast-2; gateway simulator `success` with its 5 s pay delay) · **Load:** C2, 500 closed-loop clients, SSE completion, 60 s measured, repeated 3 times per setting · **Runs:** `cloud-test7-<name>-…` (full evidence in `../cloud-test7-*/`).

**Data in this folder:** the runner's report and JSON for each setting (`<name>.md` / `.json`).

## Verdict

**Test 7 ran as designed, and its answer is negative: none of the four variables moved the number.** At 500 clients the stack completes about **43 checkouts per second** whatever was changed. Doubling the orchestrator's database pool, shortening its outbox poll interval fivefold, doubling the payment workers and doubling the inventory consumers all gave results inside the run-to-run spread. So none of these four is the limit at this load. The limit was not found by this test.

## Results

One variable per run, each run three repetitions (completed = accepted per second; every offered workflow completed in every repetition, 0 pending at the drain, 0 5xx).

| Setting (one change from the baseline) | Run 1 | Run 2 | Run 3 | Mean | CV | End-to-end p95 (mean) | vs baseline |
|---|---:|---:|---:|---:|---:|---:|---|
| **Baseline** (defaults) | 41.8 | 41.9 | 44.8 | **42.9** | 4.0% | 18.1 s | – |
| Orchestrator DB pool `DB_MAX_OPEN_CONNS` 10 → 20 | 39.1 | 40.1 | 40.2 | 39.8 | 1.5% | 19.2 s | −7%, borderline |
| Orchestrator outbox poll 100 ms → 20 ms | 40.4 | 45.0 | 43.7 | 43.0 | 5.6% | 18.2 s | 0% |
| Payment `WORKER_CONCURRENCY` 4 → 8 | 44.1 | 45.2 | 42.8 | 44.0 | 2.7% | 17.5 s | +3% |
| Inventory `INVENTORY_CONSUMER_MAX_CONCURRENCY` 4 → 8 | 41.5 | 44.0 | 47.2 | 44.2 | 6.5% | 18.2 s | +3% |

How to read it: three repetitions each give a spread of 2–7% of the mean, so a change counts only if it is clearly larger than that. The three small positive and zero differences are inside the spread. The DB-pool run is the only one that stands apart (about 3 completed/s lower, with a tight spread), but with three runs per setting that is borderline and I would not call it a real regression. After every experiment the setting was reset and the service restarted with the original values.

## What this means

- **The system is latency-bound, not worker-bound, at this load.** With a closed loop, completed per second is clients divided by the time each workflow takes. 500 clients at about 11–12 s per workflow gives about 43 per second. Making individual stages faster or wider changed nothing, so the time is spent where none of these four settings reaches.
- **Where the extra time comes from is not identified.** The median workflow takes about 6.9 s with 10 clients (almost all of it the simulator's 5 s pay delay, test 5) and about 14.7 s with 500 clients in flight (test 6, SSE), so roughly 8 s of extra waiting appears somewhere between 10 and 500 clients. By Little's law 500 clients at the observed mean of about 11.6 s per workflow gives the 43 per second. The Test 6 service table pointed at inventory (CPU peaking near 95% at 1000 clients), but raising its consumer concurrency did not help.
- This matches the local finding that more payment workers did not help. Following the project rule, I did not raise concurrency further without evidence of a saturated resource.

## Limits

1. **Only four variables, each changed once** and by one step (a factor of 2 or 5). A different step size, a combination, or a variable I did not try (for example the hard-coded consumer prefetch of 10, or the broker) could still matter.
2. **Three repetitions per setting,** so differences under about 7% cannot be separated from noise.
3. **One load point (500 clients)** and one workload (uniform over 50 products), against the simulated gateway.
4. **No profile of the services,** so "where the 11 s of queueing comes from" is open. The saga step timings in Prometheus for these runs were not examined.
5. **Run record not filled in** (git commit, image tag, Neon branch). The first attempt at the DB-pool experiment was discarded because the settings script pointed at the wrong env file on the host, so nothing had been changed; it is not in the results.

## Open items

- Read the per-step saga durations for a 500-client run to see which step stretches from 0.2 s (idle) to seconds.
- Check the RabbitMQ queue depths and consumer utilisation during a 500-client run (ready vs unacked messages), which the Test 7 data does not cover.
