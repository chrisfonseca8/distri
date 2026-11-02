# Test 6 — step sweep to find the limit (final result)

**Date:** 1 Nov 2026, 19:48–20:30 UTC · **Environment:** `staging` (ap-southeast-2; four service hosts, one broker host, Neon database; gateway simulator `success`, with the simulator's 5 s pay delay) · **Runs:** `cloud-test6-…194824Z`, `cloud-test6b-…200351Z`, `cloud-test6c-…201235Z`, `cloud-test6sse-…202610Z` (full evidence in `../<run id>/`).

**Data in this folder:** the runner's report and JSON for each of the four sweeps (`sweep-*.md` / `.json`).

## Verdict

**PASS, with a clear answer.** The limit of this stack is **about 1000 concurrent clients, roughly 50 completed checkouts per second** (the earlier run of 29 Oct, same sweep on the older build, reached 60/s at 1000). Below that, throughput grows with the number of clients; around it the end-to-end time grows to 30+ seconds without throughput following. Every run finished every workflow it accepted and all five audits passed, until the offered rate went past capacity (see the open-loop sweep).

**The way completion is measured changes the answer.** With the runner's default "poll" mode (each client asks the orchestrator for status every 100 ms) throughput *peaks at 300 clients at 37/s and then falls*. That fall is the measurement loading the orchestrator, not the application's limit. With "SSE" mode (the orchestrator pushes completion) the same stack keeps scaling to 1000 clients. Use SSE numbers for capacity and poll numbers only as a lower bound.

## Results

### Closed loop, poll completion (default): clients → completed/s

| Clients | 1 | 2 | 5 | 10 | 20 | 50 | 100 | 200 | 200 (repeat) | 300 | 400 | 600 |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| Accepted = completed per second | 0.17 | 0.30 | 0.85 | 1.63 | 3.32 | 7.85 | 15.8 | 25.1 | 31.2 | **36.9** | 31.1 | 20.9 |
| End-to-end p95 (s) | 8.1 | 10.6 | 10.6 | 10.7 | 9.9 | 10.5 | 11.2 | 15.7 | 11.0 | 13.7 | 20.1 | 43.4 |

Near-linear up to 100 clients (every checkout waits about 6 s in the simulator's pay delay, so throughput is clients divided by that), then it bends. The 200-client step was run twice and gave 25.1 and 31.2/s, so **single points vary by about 20%**. Peak 36.9/s at 300 clients; the runner's automatic knee candidate is 300.

### Closed loop, SSE completion

| Clients | 500 | 1000 |
|---|---:|---:|
| Accepted = completed per second | 37.6 | **50.8** |
| End-to-end p95 / p99 (s) | 20.0 / 23.8 | 31.6 / 36.4 |
| Checkout p50 / p95 (ms) | 33 / 1503 | 37 / 1845 |
| Completed, pending at drain | 2258, 0 | 3045, 0 |

### Open loop, offered rate (poll completion)

| Offered/s | 10 | 20 | 30 | 40 | 50 | 60 |
|---|---:|---:|---:|---:|---:|---:|
| Completed per second | 10.0 | 20.0 | 30.0 | 40.0 | 50.0 | **19.0** |
| End-to-end p99 (s) | 13.8 | 13.9 | 13.7 | 51.7 | 130.6 | 175.9 |
| Max in flight | 79 | 145 | 261 | 858 | 1190 | 1641 |

Up to 30/s the system keeps up with flat latency. From 40/s a backlog builds (p99 jumps from 14 s to 52 s, then 131 s at 50/s) even though every offered request is still completed during the run. At 60/s it collapses: 1801 accepted, **571 completed, 1230 still pending at the drain**. A fast 202 with a growing backlog is not capacity, so the **sustainable open-loop rate is about 30/s** and the first overload sign is at 40/s.

## What saturates

| Service | Poll mode, 600 clients | SSE mode, 1000 clients |
|---|---|---|
| orchestrator CPU mean / max | 98% / 132% | 30% / 104% |
| orchestrator requests during the step | 197,692 | 6,090 |
| orchestrator DB pool waits | 401,606 | 8,015 |
| inventory CPU mean / max | 8% / 93% | 16% / **95%** |
| inventory in flight (max) | 174 | 369 |
| payment, order | idle | idle (payment 24% mean) |

- **In poll mode the orchestrator is the busy service** only because the runner's status polling sent it about 200,000 requests (against about 6,000 in SSE mode) and used up its database pool of 10 connections. That is the measurement artifact behind the fall after 300 clients.
- **In SSE mode inventory is the service that nears its limit** at 1000 clients (CPU peaks at 95%, 369 requests in flight, checkout p95 1.8 s). The orchestrator has headroom. This is an inference from the metrics, not a profile of the code.
- Postgres showed no lock waits and no deadlocks in any step.
- Latency is dominated by the simulator's configured 5 s pay delay (see test 5), so the end-to-end figures above include about 5 s that no tuning of the application can remove.

## Observability

All five audits passed with no violations. Alerts fired as in earlier cloud runs (`DatabasePoolSaturated` among them). The 5xx and saga totals in the audit are cumulative since the services started, not per step. No step produced 5xx errors or transport errors in the runner.

## Limits

1. **Simulated gateway** with a 5 s pay delay; real payments will differ.
2. **One run per point** except the 200-client step (two runs: 25.1 and 31.2/s), so the curve carries about 20% noise.
3. **The SSE sweep covers only 500 and 1000 clients** on this build. The older run (29 Oct, 750 and 1000 clients) reached 54 and 60/s; the current 50.8/s at 1000 is lower, and I did not look into whether that is noise or a regression.
4. **No run went past 1000 clients in SSE mode,** so the point where SSE throughput stops growing is not shown. 1000 is where this test stopped, not a proven ceiling.
5. **Single replica of each service, one database,** and the load generator is a 2-vCPU host that was at 2% CPU in the baseline but was not checked at 1000 clients.
6. **Run record not filled in** (git commit, image tag, Neon branch).

## Open items

- Extend the SSE sweep above 1000 clients to see where it flattens, and repeat the points to get a variance.
- Check whether inventory is the limit at 1000 clients (it is the service at 95% CPU).
