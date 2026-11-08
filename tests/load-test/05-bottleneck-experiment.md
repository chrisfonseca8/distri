# Load test 05 — Bottleneck experiment

**Result:** the experiment ran as designed and its answer is negative: **none of the four variables moved throughput**. The limit was not found.

## What is tested

Whether any of four obvious tuning knobs is the limit at 500 clients. One variable is changed per run, everything else stays at its default, and each setting is repeated three times to see run-to-run spread.

## How it is run

- Load: case C2 at 500 closed-loop clients, SSE completion, 60 s measured, 3 repetitions per setting; AWS staging stack, simulated gateway with its 5 s pay delay.
- After each experiment the setting was reset and the service restarted with the original values.

## Results

Completed = accepted per second; every offered workflow completed in every repetition, with 0 pending at the drain and 0 5xx.

| Setting (one change from the baseline) | Run 1 | Run 2 | Run 3 | Mean | CV | End-to-end p95 (mean) | vs baseline |
|---|---:|---:|---:|---:|---:|---:|---|
| **Baseline** (defaults) | 41.8 | 41.9 | 44.8 | **42.9** | 4.0% | 18.1 s | – |
| Orchestrator DB pool `DB_MAX_OPEN_CONNS` 10 → 20 | 39.1 | 40.1 | 40.2 | 39.8 | 1.5% | 19.2 s | −7%, borderline |
| Orchestrator outbox poll 100 ms → 20 ms | 40.4 | 45.0 | 43.7 | 43.0 | 5.6% | 18.2 s | 0% |
| Payment `WORKER_CONCURRENCY` 4 → 8 | 44.1 | 45.2 | 42.8 | 44.0 | 2.7% | 17.5 s | +3% |
| Inventory `INVENTORY_CONSUMER_MAX_CONCURRENCY` 4 → 8 | 41.5 | 44.0 | 47.2 | 44.2 | 6.5% | 18.2 s | +3% |

Three repetitions give a spread of 2-7% of the mean, so a change counts only if it is clearly larger. The three small positive and zero differences are inside the spread. The DB-pool run stands apart (about 3 completed/s lower, tight spread), but with three runs it is borderline and is not called a regression.

## What it means

- The system is latency-bound, not worker-bound, at this load. In a closed loop, completed per second is clients divided by time per workflow: 500 clients at about 11-12 s per workflow gives about 43/s (Little's law).
- Where the extra waiting comes from is not identified. The median workflow takes about 6.9 s with 10 clients (mostly the simulator's pay delay) and about 14.7 s with 500 clients in flight, so roughly 8 s of waiting appears between 10 and 500 clients.
- Following the project's rule, concurrency was not raised further without evidence of a saturated resource.

## Limits

- Four variables, each changed once and by one step (a factor of 2 or 5). A combination, a different step size, the fixed consumer prefetch of 10, or the broker could still matter.
- Three repetitions: differences under about 7% cannot be separated from noise.
- One load point (500 clients) and one workload (uniform over 50 products); simulated gateway.
- No service profile was taken; RabbitMQ queue depths were not examined in this run.

**Raw data:** [`baseline`](data/bottleneck/baseline.md), [`orch-dbpool-20`](data/bottleneck/orch-dbpool-20.md), [`orch-outbox-poll-20ms`](data/bottleneck/orch-outbox-poll-20ms.md), [`payment-workers-8`](data/bottleneck/payment-workers-8.md), [`inventory-consumers-8`](data/bottleneck/inventory-consumers-8.md) (each as `.md` and `.json` in [`data/bottleneck/`](data/bottleneck/))
