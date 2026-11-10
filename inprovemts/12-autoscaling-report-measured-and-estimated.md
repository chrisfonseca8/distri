# Autoscaling report: every test so far, measured and estimated (2026-11-11)

Status: **reported**. Numbers marked **M** come from the runner reports in `inprovemts/data/<run>/load-test.md`, the 1 November campaign in `tests/`, and the scaler logs. Numbers marked **E** are estimates; none of them was run. Nothing marked E replaces or edits a measured figure. All throughput and latency figures use the **simulated gateway** (about 5 s pay delay), never real Razorpay.

## 1. What "auto scaling" means here, and who used it

| Layer | What it scales | Settings | Used by |
|---|---|---|---|
| **A. In-process autoscalers** | payment webhook workers (1 up to 10), orchestrator DB pool (10 up to 50), payment DB pool (from 9 Nov, `dyn2`), consumers by queue depth | trigger: backlog and age (workers); average wait 5 ms, later 2 ms (pools) | `dyn50`, `dyn60`, `dyn80`, `dyn100`, `dyn2-100`, `dyn3-130/160`, `asg130`, `asg160`, `r130` |
| **B. EC2 ASG behind an ALB** | orchestrator instances, 1 up to 2 | target tracking, average CPU 50% for 3 minutes, warm-up 180 s | `asg130`, `asg160`, `dyn130`, `r130` |
| **None (fixed)** | one webhook worker, pools of 10, one instance of each service | n/a | 1 Nov campaign, `dry10`, `s20`, `s30`, `s40`, `o60` |

"E with autoscaling" below means **layers A and B both on**, with the orchestrator in **poll** completion mode.

## 2. What autoscaling measurably did

| Evidence | Before | After | Source / caveat |
|---|---|---|---|
| Webhook workers autoscaled at 60/s | saga p50 / p95 / max 43.6 / 80.1 / 92.3 s, 3,380 sagas running, 9,005 events backlog | 6.4 / 10.9 / 20.9 s, 491 running, 20 claimable (workers scaled 1 to 2) | **M**, `o60` against `dyn60`. Polling also changed from 2 s to 5 s, so it is not a strict A/B |
| Faster scale-up (cooldown 30 s to 10 s) at 100/s | peak backlog 5,354, oldest event 17.9 s | 2,483 and 8.6 s; workers 2 at 20 s, 3 at 30 s | **M**, `dyn100` against `dyn2-100` (steady results identical: 18,000 of 18,000, p95 about 11 s) |
| Payment DB pool trigger 5 ms to 2 ms at 130/s | pool never resized; 254,902 blocked acquisitions, 771 s total wait | resized 10 to 15; 23,274 blocked, 17.3 s | **M**, `dyn3-130` against `asg130`; p95 26.2 to 11.1 s, but the database project also changed |
| Orchestrator DB pool autoscaler | pool stuck at 10 | 10 to 15 to 20 within about 1 to 1.5 minutes at 80 to 130/s | **M**, scaler logs |
| EC2 ASG scale-out | n/a | desired size raised 4.0 to 5.9 min after the runner started; new instance serving 7.1 to 9.7 min after | **M**, `dyn130`, `r130` |
| Two orchestrator instances alive together | n/a | 200 of 200 completed, 0 duplicates, second replica did 400 of 800 steps (local); in `r130` and `dyn130` the second instance joined and after the drain 23,401 of 23,401 were PAID with 0 violations | **M**, failure case 08; run records |

## 3. When would the EC2 policy fire? (orchestrator CPU against rate)

Container CPU is a percent of one core (2 vCPU host). Host % is that divided by 2. CloudWatch read 2 to 10 points above the guest's own `vmstat` in `dyn130`, so the last column adds that range.

| Offered/s | Orchestrator CPU mean (M) | Host % (E) | CloudWatch host % (E) | Policy fires (needs above 50% for 3 min)? |
|---:|---:|---:|---:|---|
| 10 | 9.6% (`dry10`) | 5 | 7 to 15 | no |
| 20 | 17.7% (`rehearse`), 19.2% (`s20`) | 9 | 11 to 19 | no |
| 30 | 26.7% | 13 | 15 to 23 | no |
| 40 | 34.0% | 17 | 19 to 27 | no |
| 50 | 36.5% | 18 | 20 to 28 | no |
| 60 | 43.4% | 22 | 24 to 32 | no |
| 80 | 54.8% | 27 | 29 to 37 | no |
| 100 | 66.3% | 33 | 35 to 43 | no |
| 130 | 87.1% (`asg130`) | 44 | 46 to 54 | **borderline** |
| 160 | 105% (`asg160`, saturated) | 53 | 55 to 63 | yes |
| 200 | about 134% (E) | about 67 | 69 to 77 | yes |

So below about 130/s the ASG never adds an instance, and every result below is unchanged by it. Even when it fires, a new instance serves only 7 to 10 minutes later.

## 4. Open-loop rate tests

| Run | Setup then | Measured: completed / offered; saga p50 / p95 / max; orchestrator CPU | Estimate with autoscaling (E) | Confidence |
|---|---|---|---|---|
| `dry10` 10/s, 30 s | fixed | 300 / 300; 6.3 / 10.7 / 26.2 s; 9.6% | same; no autoscaler or policy action | high |
| `newdb-smoke10`, `rehearse` 10 and 20/s | A on, new DB | 300 / 300 and 1,200 / 1,200; about 6.5 / 11 s | same | high |
| `s20` 20/s | fixed | 3,600 / 3,600; 6.5 / 15.8 / 25.1 s (cold start) | same (the high p95 is cold-start, not capacity) | high |
| `s30` 30/s | fixed | 5,401 / 5,401; 6.3 / 11.0 / 23.5 s | same | high |
| `s40` 40/s | fixed | 7,200 / 7,200; 6.6 / 11.3 / 27.9 s | same | high |
| `o60` 60/s | fixed (1 worker) | 10,801 / 10,801; **43.6 / 80.1 / 92.3 s**; 3,380 running | about 6.4 / 10.9 / 20.9 s: the measured autoscaled counterpart is `dyn60`; the EC2 layer does not fire | high (counterpart measured) |
| `dyn50`, `dyn60` | A on | 9,000 and 10,801 complete; about 6.3 / 10.9 / 22 s; 36% and 43% CPU | same; EC2 does not fire | high |
| `dyn80` 80/s | A on | 14,400 / 14,400; 6.5 / 11.6 / 22.1 s; first minute shows a ramp (backlog 4,072, 120 goroutines waiting on the pool) | same; the ramp is not removed by EC2 scaling | high |
| `dyn100`, `dyn2-100` 100/s | A on | 18,000 / 18,000; 6.2 to 6.3 / 10.9 to 11.2 / 24 s; 66% CPU | same | high |
| `dyn3-130` 130/s | A on, payment trigger 5 ms | 23,401 / 23,401; **15.8 / 26.2 / 37.9 s**; 82% CPU | about 11 s p95 with the 2 ms trigger (the measured counterpart is `asg130`); EC2 borderline and too slow to matter | medium |
| `asg130` 130/s | A + B | 23,401 / 23,401; **6.5 / 11.1 / 23.7 s**; 87%; 10 of 10 invariants | same; a possible scale-out would land after the window | medium-high |
| `dyn3-160` 160/s | A on, trigger 5 ms | 12,129 / 28,800; 59.4 / 172.3 / 183.0 s; 100% | about 16,000 / 28,800 (the measured counterpart is `asg160`) with one instance | medium |
| `asg160` 160/s | A + B, one instance | 16,221 / 28,800 (90/s); 66.3 / 144.9 / 152.9 s; 105%; pool pinned at 20 | **E with 2 instances from the start:** 90 to 100% complete, p50 / p95 about 7 to 9 / 12 to 30 s, about 53% CPU each | low to medium |
| 200/s | n/a | not run | see `11-final-...md`, section 3 | low |
| `dyn130` (10 min), `r130` ×2 | A + B | invalid or database-limited | **not estimable**: these runs measured the database (storage cap, 2 vCPU compute), not the application | n/a |

## 5. Closed-loop, baseline and soak (1 November campaign, fixed, single replicas)

| Test | Measured | Estimate with autoscaling (E) | Confidence |
|---|---|---|---|
| Baseline, 10 clients ×3 | 1.55/s, p95 11.3 s | same | high |
| Open-loop sweep 10 to 60/s | 10 to 50/s completed = offered; **60/s completed 19.0/s**, p99 175.9 s; backlog from 40/s (p99 51.7 s) | 40 to 60/s completed = offered, saga p95 about 11 s (closest measured: `s40`, `dyn50`, `dyn60` from 9 Nov; code and config also changed between the two dates) | medium |
| Closed-loop poll sweep to 600 clients | peak **36.9/s** at 300 clients, 20.9/s at 600; the fall is measurement load (197,692 polls, 401,606 pool waits) | peak 37 to 40/s with the same shape: raising the orchestrator pool 10 to 20 gave 39.8/s (borderline, minus 7%) in test 7. A second instance could lift it by 0 to 30% (orchestrator at about 98% CPU, so the policy likely fires) | low |
| Closed-loop SSE, 500 / 1000 clients | 37.6 / **50.8/s**, inventory-service at 95% CPU | with one instance: same. With a **second orchestrator: worse (E 5 to 30/s)**, because SSE notification is replica-local (measured: 30.3 then 5.0/s with two replicas) | medium (the mechanism is measured) |
| Soak, 700 clients SSE, 5 min | 42.45/s, p95 21.8 s | same; the policy does not fire at about 42/s | medium |
| Bottleneck experiment (4 variables) | none moved throughput (42.9/s baseline; spread 2 to 7%) | same: autoscalers make the pool and worker variables dynamic, and the limit was elsewhere (inventory CPU) | medium |

## 6. Fault injection and correctness

| Test | Measured result | Estimate with autoscaling (E) | Confidence |
|---|---|---|---|
| 1 payment `kill -9` | 200 of 200 completed, 0 duplicates | unchanged | high |
| 2 orchestrator `kill -9` | 143 accepted, 57 refused while down, 143 completed, 0 unfinished | with 2 instances: fewer refusals (the other instance keeps serving), but about half the requests to the dead target fail until the ALB marks it unhealthy (2 failed checks at 30 s = about 60 s); the container restarts in seconds; the survivor finishes the sagas; 0 duplicates, 0 unfinished | medium-low |
| 3 order `kill -9`, 4 inventory `kill -9` | 200 and 143 of their accepted checkouts completed, 0 duplicates | unchanged (those services are not autoscaled) | high |
| 5 broker restart, 6 database connection drop 20 s, 7 network delay 200 ms | all completed, 0 duplicates, 0 dead-queue growth | unchanged; with two orchestrators each recovers its own pool | medium |
| 8 two orchestrator replicas | PASS: 200 of 200, 0 duplicates, second replica 400 of 800 steps | same behind an ALB in poll mode; supported by `r130` and `dyn130` (second instance joined; 0 violations after the drain) | high |
| 9 payment declined | PASS only on the rerun with a 3 s payment window; the first run with the default window did **not** pass | unchanged (payment-side behaviour, not autoscaled) | medium |
| 10 payment timeout (ambiguous) | PASS | unchanged | high |
| 11 overload on one hot product | 151 of 2,184 refused (503); no oversell; its own accounting check not passed | unchanged (the limit is one inventory row, not autoscaled capacity) | medium |
| Oversell (500 buyers, 10 units) | exactly 10 accepted, 490 refused, 0 deadlocks | PASS, unchanged (decided in the database) | high |
| Correctness suite (20 phases, 3 gateway modes) and final audit | PASS; audit clean; `phase 22` PASS on the `dyn60` and `dyn100` data; all 10 invariants PASS in the valid runs | PASS: these are database-state checks | high |
| Unit and integration (470 tests, with a disposable Postgres) | 0 failed, 0 skipped; includes two-replica sweeper tests | not affected by infrastructure scaling | high |
| Mutation checks | 13 of 17 caught, 4 survived | not affected | high |
| Penetration | **not performed** | not estimable; the ALB is internal, the new surface is the SSM parameters and the security groups | n/a |

Not tested at all, and estimated only by reasoning: terminating an instance under load (E: in-flight steps are retried by the sweeper after the 30 s step timeout, so sagas finish later but are not lost; scale-in is disabled so it cannot happen by itself), and losing the one subnet the ASG uses (E: no instances, no service, so this is not highly available).

## 7. Cross-cutting caveats that bound every estimate
1. **Completion mode matters.** Polling works across replicas (it reads the database). SSE completion is replica-local, so a second orchestrator behind an ALB breaks SSE clients. All the 9 November and later runs polled.
2. **Scale-out is too slow for short tests.** About 3 minutes of breach, 1 minute to boot, 2.5 minutes of ALB checks: serving 7 to 10 minutes after load starts. A 4-minute window sees one instance, so "ASG on" and "one instance" are the same result there.
3. **The database is the final limit.** On the free Neon compute (2 vCPU) 130/s saturated the CPU in both `r130` runs. Every E above assumes a database with headroom, like the first project showed.
4. **Single runs.** Most points are one run; repeats of the same setting varied 2 to 7%, and single sweep points about 20%.
5. **Polling inflates load.** Each in-flight workflow is polled every 5 s (100 ms in the 1 November sweeps), so slow sagas raise the orchestrator's CPU and pool waits.
6. **Storage.** One workflow is 14 to 16 KB; the free project holds about 65,000.

## 8. What to run to turn estimates into measurements (in order)
1. Split the databases across two projects (orchestrator on its own), record Neon size before and after each run.
2. Start two orchestrator instances before the load, then run 160/s and 200/s once each in poll mode.
3. Run 130/s with one instance and the policy on, to settle whether it fires near 50%.
4. Terminate one of two instances under load and check that nothing is lost or duplicated.
5. Re-run the orchestrator `kill -9` case with two instances behind the ALB and count refusals.
6. If SSE must scale out, build a shared completion-notification path first, then repeat the 500 and 1000-client SSE tests with two instances.
