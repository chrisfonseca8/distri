# Test 4 — oversell on the cloud stack (final result)

**Date:** 1 Nov 2026, 19:15–19:22 UTC · **Environment:** `staging` (ap-southeast-2; four service hosts, one broker host, Neon database; gateway simulator in `success` mode) · **Run id:** `cloud-test4-20261101T191526Z`

**Data in this folder:** [`load-test.md`](load-test.md) / [`load-test.json`](load-test.json) (the runner's report for both runs), [`phase22-audit.md`](phase22-audit.md) (database audit after the run), [`audit.json`](audit.json) (observability audit), [`run.json`](run.json).
**Full evidence** (Prometheus export, 8 MB of logs): `../cloud-test4-20261101T191526Z/`.

## Verdict

**PASS.** 500 buyers competed for 10 units of one product. Exactly 10 were accepted and completed; the other 490 were refused with HTTP 409. No oversell, no stock violation, and the database audit afterwards was clean.

A second run in the same command (c5a, a throughput run at 500 clients) **failed** an accounting check because the checkout endpoint refused 151 requests with HTTP 503. That run is not part of the oversell criterion. It is recorded below as a separate overload finding.

## What was done

`LOAD_CASE=C5` runs two cases back to back, so the same command ran both. The settings were `LOAD_CLIENTS=500`, `LOAD_ORDERS=500`, `LOAD_HOT_STOCK=10` and `LOAD_MAX_CONNS=1000`, run from the load-generator host.

| Run | What it does |
|---|---|
| c5a, hot-product throughput | 500 closed-loop clients buy one high-stock product for 60 s (10 s warm-up). **My `LOAD_CLIENTS=500` setting applied to it too.** Its normal setting is 50 clients, so this was an overload run, not the canonical case. |
| c5b, hot-product sell-out | 500 clients submit 500 attempts against one product with 10 units. It requires exactly 10 successes and HTTP 409 for the rest. This is the oversell test. |

## Oversell result (c5b)

| Check | Result |
|---|---|
| Attempts offered | 500 |
| `HTTP 202` accepted | **10** |
| `HTTP 409` out of stock | **490** |
| Transport errors, 5xx, decode errors | 0 |
| Completed | 10 (10 paid, 0 cancelled, 0 pending at the drain) |
| Invariant: consumes exactly the available stock, rejects the remainder with 409 | **PASS** |
| Lock waiters, deadlocks | 0 |
| Checkout latency p50 / p95 / p99 | 465 / 546 / 552 ms |
| End-to-end latency for the 10 sold (p50 / p95) | 7.4 s / 13.0 s |
| Phase-22 database audit | **PASS** (1 of 1) |

Only 10 checkouts needed the full saga, so 490 of the 500 requests ended at the inventory step. The end-to-end figures of 7 to 13 s come from 500 clients hitting a handful of services at the same instant, not from the payment path.

## Overload finding (c5a)

| Check | Result |
|---|---|
| Offered / accepted / completed | 2184 / 2033 / 2033 (2033 paid, 0 pending at drain) |
| `HTTP 202` / `HTTP 503` | 2033 / **151** |
| Failed check | "one unique completed checkout attempt per offered workflow": 151 violations, which is exactly the 151 refused requests |
| Checkout latency p50 / p95 / p99 | 150 ms / 3.08 s / 3.21 s; end-to-end p95 21.6 s |
| Accepted rate | 33.9/s |

The 503s were not requests cut off at the end of the window (my first guess, which was wrong). The service metrics for the same run point at the inventory step and database waits:
- **inventory:** 151 errors, up to 468 requests in flight, and a handler p95 of 3.0 s. That is almost exactly 3 s, which suggests a timeout, but I have not looked in the code for the limit.
- **orchestrator:** also 151 errors, up to 500 requests in flight, and 397,580 database pool waits. Its pool has 10 connections.

So at 500 concurrent clients on one hot product, about 7% of checkouts were refused with 503, and the inventory step on that single row was the slow part. This is an inference from the metrics, not a traced cause. There is no correctness impact: nothing was oversold and 0 sagas were left pending.

## Observability (`audit.json`)

- **Verdict PASS, no violations.** At the end: 0 stuck sagas, 0 dead outbox rows, 0 unknown or unconfirmed payments, 0 incomplete refunds, 0 stock violations, 0 dead-queue depth.
- **Alerts fired:** `DatabasePoolSaturated` (fired in the test 2 and test 3 runs as well), `HighLatencyP95` and `ServiceDown`. I have not checked why `ServiceDown` fired during a load run with no fault injected.
- **5xx:** the audit shows 1395 in total, but this is a cumulative counter since the services started, not a per-run figure. The Test 5 baseline run, which the runner counted as 0 5xx, exported the identical 1395. Of this run only the 151 counted by the runner are attributable to it.

## Limits

1. **One oversell sample.** The two runs in the command were c5a and c5b, not repeats, so there is no variance figure. A repeat has not been done.
2. **Gateway simulator in `success` mode.** Real Razorpay behaviour was not involved.
3. **One product and one load shape.** 500 clients, 10 units. Multi-product and multi-item contention were covered separately by the earlier cloud contention runs, not here.
4. **c5a was an accident of settings.** It shows overload behaviour at 500 clients, not a throughput result, and it should not be quoted as a capacity figure.
5. **Run record not filled in:** git commit, image tag and Neon branch are not recorded.

## Open items

- Find which limit produced the 503s in c5a, and whether `ServiceDown` belongs to the same overload.
- Optional: repeat the oversell run once or twice for confidence.
