# Autoscaled webhook workers + orchestrator pool at 50/s and 60/s — results and next choke (2026-11-09)

Both controllers enabled. Simulated gateway (5 s median pay delay), uniform 50-SKU mix, open loop, 60 s warm-up + 180 s at the rate + drain, polling every **5 s** (the earlier fixed-size 60/s run polled every 2 s, so it is context, not an exact A/B). Neon `ep-falling-tree-a74c336v` (disposable).

## What was deployed to staging (changed hosts; reversible)
- Images built locally from this tree (`payment-service-dyn1`, `orchestrator-service-dyn1`) and loaded onto the two hosts with `docker save | ssh docker load`. **Nothing was pushed to a registry.**
- Payment host and orchestrator host: `compose.cloud.yaml` replaced by the working-tree version (passes the new variables through; backups `compose.cloud.yaml.bak-dyn1`), env files edited (backups `deploy/cloud/.env.payment.bak-dyn1`, `deploy/cloud/.env.orchestrator.bak-dyn1`): `IMAGE_TAG=dyn1` plus
  - payment: `PAYMENT_WEBHOOK_AUTOSCALE=true` (everything else default: start 1, max 10 -> **effective 6** on a pool of 10 with reserve 4, interval 5 s, up age 2 s, up samples 2, up cooldown 30 s, down samples 12, down cooldown 60 s, down utilization 0.6);
  - orchestrator: `ORCHESTRATOR_DB_POOL_AUTOSCALE=true`, `ORCHESTRATOR_DB_CONNECTION_BUDGET=100`, `ORCHESTRATOR_DB_CONNECTION_BUDGET_RESERVED=10`, `ORCHESTRATOR_REPLICAS=1` (start 10, ceiling 50, step 5, up wait 5 ms, up samples 2, up cooldown 30 s, down usage 0.3, down samples 24, down cooldown 60 s).
- Load generator host: `~/run-inprovemts.sh` rewritten (poll 5 s; also records `vmstat` and `docker stats` for the host and the simulator).
- Both containers were recreated before each run so each starts at 1 worker and a pool of 10.
- **Rollback:** on each host `cp -p compose.cloud.yaml.bak-dyn1 compose.cloud.yaml; cp -p deploy/cloud/.env.<payment|orchestrator>.bak-dyn1 deploy/cloud/.env.<payment|orchestrator>` then `docker compose --env-file deploy/cloud/.env.<..> -f compose.cloud.yaml up -d --force-recreate --no-deps --pull never <payment-service|orchestrator>` (the `09e4015` images are still on the hosts). **Currently left running on dyn1 with both autoscalers on.**

Commands (laptop): `inprovemts/scripts/sweep.sh 3.27.207.44 dyn50:50` then recreate both containers then `... dyn60:60`; analysis `python3 inprovemts/scripts/choke_summary.py inprovemts/data/dyn50|dyn60`; audit `PHASES=22` through the compose `correctness` profile (output in `data/dyn60/audit/`).

## Comparison
| Run | Offered | Accepted/s | Completed/s | Saga p50 / p95 / max | Peak RUNNING sagas | Peak webhook backlog | Oldest webhook wait | Drain after offering stops | Unfinished |
|---|---:|---:|---:|---|---:|---:|---:|---:|---:|
| 60/s fixed (1 worker, pool 10; earlier run) | 10,801 | 60.0 | 60.0 | 43.6 / 80.1 / 92.3 s | 3,380 | 9,005 (all pending) | 76.7 s | 87 s | 0 |
| **50/s autoscaled** | 9,000 | 50.0 | 50.0 | 6.2 / 10.8 / 22.4 s | 367 | 27 (claimable) | 0.1 s | 20 s | 0 |
| **60/s autoscaled** | 10,801 | 60.0 | 60.0 | 6.4 / 10.9 / 20.9 s | 491 | 20 (claimable) | 0.1 s | 18 s | 0 |

0 rejected, 0 5xx, 0 timeouts in both new runs. Phase 22 on the 60/s data: **PASS** (all 11 assertions; the text "timed out waiting for the worker backlog" next to the first one is that check's failure-message template, printed on every audit, including earlier passing ones).

## What the autoscalers did
- **Webhook workers:** 50/s: 1 -> 2 at 16:41:00 (ready=697, oldest claimable 4.6 s, 2 samples); 60/s: 1 -> 2 at 16:47:25 (ready=811, oldest 4.0 s). Both runs held at **2** (peak busy ratio 1.43 and 1.50 work-seconds per second across the two workers, so ~0.72-0.75 each). Scale-down was correctly refused ~25 times during each run (`1 workers would run at 1.1-1.3, limit 0.6`) even when the inbox was empty, and happened once traffic stopped (16:46:00, 16:52:30: "demand low for 12 samples, projected load 0.00"). Reaction time: the first addition came about 25 s after the load started, during the warm-up (the transient backlog before it was ~700-800 events, ~4 s old); the measured windows contain no growth.
- Processed = arrived in both runs: 148/s and 178/s (the fixed single worker topped out at 127/s).
- **Orchestrator pool:** no resize in either run. Blocked acquisitions 5,405 (30/s, avg 2.8 ms) at 50/s and 4,239 (23.5/s, avg 3.1 ms) at 60/s, both under the 5 ms trigger; pool in use mean 4.2 / 5.2, max 10, within 1 of the cap in 8% of samples. Compare the fixed 60/s run: 235,827 blocked acquisitions (880/s) averaging 6.8 ms, with polling every 2 s. The guard held increases once for a moment (`scale_up_held` reached 1; probe latency peaked at 20 ms), but no increase was being requested.
- So the large pool waits seen before came from the 2 s polling of a 3,380-saga backlog; with the backlog gone and 5 s polling (89-110 polls/s instead of 949) the pool did not need to grow.

## The next choke (measured at 60/s unless marked PREDICTED)
Nothing is saturated at 60/s. Highest utilisations, in order:

| # | Candidate | Numbers at 60/s | Status |
|---|---|---|---|
| 1 | **Payment DB pool (10), not autoscaled** | in use max 10, within 1 of cap in 8% of samples (mean 4.4, up from 3.5 at 50/s: +0.09 per checkout/s); 1,131 blocked acquisitions avg 0.76 ms. Each added webhook worker takes up to one connection (cap 6 of 10) and webhook HTTP inserts grow with rate. | **Suspected** next limit; PREDICTED mean in-use near 8 at ~100/s, earlier in bursts |
| 2 | Orchestrator DB pool (10, autoscaled) | mean 5.2 (+0.105 per checkout/s), 8% of samples near full | PREDICTED to start growing around 80-90/s (mean reaches 8 at ~87/s); the scaler is built for this and the guard/budget will bound it |
| 3 | Webhook inbox with 2 workers | busy 1.5 of 2.0 (75% peak); capacity ~257 events/s ~ 86 checkouts/s | PREDICTED third worker at ~85+/s; effective worker cap is 6 (pool-limited) |
| 4 | Neon compute CPU | **not measured** (no API access); statement p95 still ~0 for both services | **Unknown**; a Neon CPU graph for 16:40-16:52 UTC (10:10-10:22 PM IST) would settle it |
| 5 | Orchestrator CPU | 36% -> 42% of a core (+0.63 per checkout/s), host 32% mean / 36% max | PREDICTED ~100% of one core near 150/s; not a limit at 60/s |
| 6 | RabbitMQ / broker | host CPU 19% mean (22% max); replies queue ready max 60, order commands 25; 0 dead letters | Unlikely |
| 7 | Load generator + simulator host | host CPU 6% mean (9% max); simulator container 5% mean (9% max at 60/s) | Not a limit |
| 8 | Simulator pay-delay tail | saga p95 10.9 s ~ the lognormal p95 + ~1 s | Confirmed floor, not an application limit |

Summary: the first choke (single webhook worker) is gone; to find the next one the rate must go up. The measurement that would turn the PREDICTED rows into facts is the same two runs at **80/s and 100/s** with these settings (watch payment pool in-use and waits, orchestrator pool cap and acquire wait, worker count, Neon CPU).

## Limits
One run per rate; scaler thresholds are defaults, untuned; the payment pool has no autoscaler by design (only the orchestrator's was requested); Neon CPU not measured; the earlier fixed-size row used 2 s polling.
