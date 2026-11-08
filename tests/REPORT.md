# Test report

Results of the cloud test campaign run on 1 November 2026, plus the changes that the findings drove. Raw data for every number is in [`data/`](data/).

**Environment.** AWS `ap-southeast-2`: one host per service (orchestrator, order, inventory, payment), one RabbitMQ host, one load-generator host, and a Neon PostgreSQL database. Every service ran as a single replica.

**The payment gateway is simulated.** All throughput and latency figures use the project's own gateway simulator, not Razorpay. In this campaign the simulator was configured with a 5 s pay delay (`SIM_PAY_DELAY`), so about 5 s of every checkout is a configured delay that no application change can remove. A checkout takes about 7 s end to end on an idle system.

**Terms.** *Accepted/s*: checkouts the API acknowledged (HTTP 202). *Completed/s*: checkouts that reached a final saga state. Where the two differ, a backlog is building. *Closed loop*: a fixed number of clients, each starting a new checkout when its previous one finishes. *Open loop*: checkouts arrive at a fixed rate whether or not earlier ones have finished.

## Verdicts

| Test | What it checks | Verdict | Data |
|---|---|---|---|
| 2 Correctness suite | 20 active phases, three gateway modes (success, decline, timeout) | **PASS** with caveats (see below) | [test2](data/test2/test2-all-runs.json) |
| 3 Fault injection | 7 faults under load | **PASS** (7 of 7) | [test3](data/test3/chaos-test.json) |
| 4 Oversell | 500 buyers for 10 units | **PASS** | [test4](data/test4/load-test.md) |
| 5 Baseline | 10 clients, 3 repetitions | **PASS** | [test5](data/test5/load-test.md) |
| 6 Limit search | client and rate sweeps | **PASS** (curve measured; ceiling not reached in SSE mode) | [test6](data/test6/) |
| 7 Bottleneck search | 4 tuning variables, 3 repetitions each | **PASS** as an experiment; **no variable moved throughput** | [test7](data/test7/) |
| 8 Two orchestrator replicas | correctness with 2 replicas | **PASS** for correctness; throughput **not measured** | [test8](data/test8/chaos-test.md) |
| 9 Soak | 700 clients, 5 minutes | **PASS** (5 minutes only) | [test9](data/test9/load-test.md) |
| 10 Final audit | database invariants after all runs | **PASS** | [test10](data/test10/phase22-audit.md) |

Unit and integration tests: 334 Go tests pass and 0 fail across the six modules. A further 130 database-gated integration tests are skipped in a plain `go test ./...` run (they need `DATABASE_TEST_URL` and the `RUN_*_INTEGRATION` flags; see [`../tools/run-integration.sh`](../tools/run-integration.sh)). Counted by running `go test ./... -count=1 -v` on 8 November 2026, subtests included.

## Headline metrics

| # | Result | Caveat |
|---|---|---|
| 1 | **Fault tolerance: 7 of 7 scenarios passed** under 30 clients with 56-57 sagas in flight (service kills, broker restart, 20 s database connection drop, 200 ms network delay). 0 duplicate payments or reservations, 0 unfinished sagas, dead-queue growth 0. [data](data/test3/chaos-test.json) | Judged on accepted checkouts only: when the orchestrator or inventory-service was down, 57 of 200 requests were refused and never accepted. Each scenario ran once. No ambiguous (UNKNOWN) payment occurred, so that path was not exercised. |
| 2 | **Oversell: 500 concurrent buyers for 10 units; exactly 10 accepted (HTTP 202), 490 refused (HTTP 409).** 0 deadlocks, audit clean. [data](data/test4/load-test.md) | One run. |
| 3 | **Data integrity: 12,736 of 12,736 sagas COMPLETED**, orders PAID, payments SUCCEEDED and reservations CONFIRMED; 0 invariant violations in the final audit. [data](data/test10/phase22-audit.md) | The audit covers the last run's data (the soak), because each load run resets the fixtures. |
| 4 | **Sustained: 42.45 accepted/s and 42.45 completed/s over 5 minutes at 700 clients** (0 pending, 0 5xx). End-to-end p50 / p95 / p99: 16.8 / 21.8 / 25.2 s. [data](data/test9/load-test.md) | Simulated gateway; one run; 5 minutes, not the 30-60 minutes planned; single replicas. |
| 5 | **Peak, single run, tested up to 1000 clients: 50.8 completed/s** at 1000 clients with SSE completion (3,045 checkouts, 0 pending). At 500 clients with SSE, two runs gave 37.6/s (test 6) and 42.9/s (test 7 baseline, spread 3.3%). [data](data/test6/sweep-clients-sse-500-1000.md) | 1000 clients is where the test stopped, not a measured ceiling. One run per point; repeating the 200-client step gave 25.1 and 31.2/s, so single points carry about 20% noise. |
| 6 | **Open loop: 10-30/s completed with flat latency; backlog from 40/s; collapse at 60/s** (19.0/s completed against 60.0/s offered). A sustainable rate is about 30/s. [data](data/test6/sweep-rate-poll.md) | Poll completion; one run per rate. "About 30/s" is read from the jump in p99 between 30/s and 40/s. |
| 7 | **Correctness suite: all 20 active phases pass across the three gateway modes**; the database audit was clean after all four runs. [data](data/test2/test2-all-runs.json) | No single run covers every phase (some phases apply to one mode only). The first decline run failed on the default payment window and passed with a 3 s window and 1 s reconciliation, so the decline result holds for those timings. |
| 8 | **Ruled out: four tuning variables did not move throughput** (see below). | The limit was not identified. |

**Closed loop and open loop are different measurements.** Closed loop (fixed clients) produced about 43/s at 500-700 clients because the clients throttle themselves as latency grows. Open loop (fixed arrival rate) shows what the system can absorb without a backlog: about 30/s. Neither is a ceiling for the other.

## Fault-tolerance results

200 checkouts per scenario at 50 per second from 30 clients; a fault is injected about 1.5 s in. [data](data/test3/chaos-test.json)

| Scenario | Accepted | Refused while down | Final status | Unfinished | Duplicates | Dead-queue growth | Drain (s) |
|---|---:|---:|---|---:|:---:|---:|---:|
| payment-service `kill -9` | 200 | 0 | 200 COMPLETED | 0 | none | 0 | 9.5 |
| orchestrator `kill -9` | 143 | 57 | 143 COMPLETED | 0 | none | 0 | 11.9 |
| order-service `kill -9` | 200 | 0 | 200 COMPLETED | 0 | none | 0 | 21.0 |
| inventory-service `kill -9` | 143 | 57 | 143 COMPLETED | 0 | none | 0 | 11.8 |
| RabbitMQ restart | 200 | 0 | 200 COMPLETED | 0 | none | 0 | 9.5 |
| Database connection drop (20 s) | 200 | 0 | 200 COMPLETED | 0 | none | 0 | 46.5 |
| Network delay 200 ms (40 s) | 200 | 0 | 200 COMPLETED | 0 | none | 0 | 16.6 |

The orchestrator and inventory-service refuse checkouts while down because the stock reservation is a synchronous call. With the broker down nothing was refused, because accepting a checkout writes only to the Postgres outbox. During the broker restart the outbox logged 83 publish failures and no row was marked dead.

Not covered: Neon autosuspend (only a connection drop on one host), faults on hosts other than payment for the network delay, backup/restore, broker volume loss.

## Oversell result

500 clients submitted 500 attempts for one product with 10 units: 10 accepted (HTTP 202), 490 refused (HTTP 409), 0 transport errors, 0 5xx, 0 lock waiters, 0 deadlocks. Checkout latency p50 / p95 / p99: 465 / 546 / 552 ms. The 10 sold finished end to end in 7.4 s (p50) and 13.0 s (p95). The phase-22 database audit passed. [data](data/test4/load-test.md)

The same command also ran a throughput case at 500 clients on one hot product. It offered 2,184 checkouts, accepted 2,033 and refused 151 with HTTP 503 (accepted rate 33.9/s). Nothing was oversold and no saga was left pending. This is an overload observation, not a capacity figure.

## Load sweep

All figures use the simulated gateway with its 5 s pay delay. Accepted equals completed in every closed-loop step, so one row is shown. [data](data/test6/)

**Closed loop, poll completion** (clients ask the orchestrator for status every 100 ms)

| Clients | 1 | 2 | 5 | 10 | 20 | 50 | 100 | 200 | 200 (repeat) | 300 | 400 | 600 |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| Accepted = completed per s | 0.17 | 0.30 | 0.85 | 1.63 | 3.32 | 7.85 | 15.8 | 25.1 | 31.2 | **36.9** | 31.1 | 20.9 |
| End-to-end p95 (s) | 8.1 | 10.6 | 10.6 | 10.7 | 9.9 | 10.5 | 11.2 | 15.7 | 11.0 | 13.7 | 20.1 | 43.4 |

**Closed loop, SSE completion** (the orchestrator pushes completion)

| Clients | 500 | 1000 |
|---|---:|---:|
| Accepted = completed per s | 37.6 | **50.8** (peak, single run) |
| End-to-end p95 / p99 (s) | 20.0 / 23.8 | 31.6 / 36.4 |
| Checkout p50 / p95 (ms) | 33 / 1503 | 37 / 1845 |
| Completed; pending at drain | 2,258; 0 | 3,045; 0 |

**Open loop** (poll completion)

| Offered/s | 10 | 20 | 30 | 40 | 50 | 60 |
|---|---:|---:|---:|---:|---:|---:|
| Completed per s | 10.0 | 20.0 | 30.0 | 40.0 | 50.0 | **19.0** |
| End-to-end p99 (s) | 13.8 | 13.9 | 13.7 | 51.7 | 130.6 | 175.9 |
| Max in flight | 79 | 145 | 261 | 858 | 1190 | 1641 |

At 60/s, 1,801 checkouts were accepted, 571 completed and 1,230 were still pending at the drain. A fast 202 with a growing backlog is not capacity.

**Idle baseline** (10 clients, 3 repetitions): 1.55 accepted = completed per s (spread 4.0%), end-to-end p95 11.3 s, checkout p95 about 66 ms. The payment step averaged 6.16 s of the 6.75 s saga; the simulator's pay delay accounts for most of it. [data](data/test5/load-test.md)

**What saturates.** In poll mode the orchestrator is the busy service because the clients' own status polling sent it about 197,692 requests at 600 clients and exhausted its 10-connection database pool (401,606 pool waits). In SSE mode at 1000 clients it received 6,090 requests and 8,015 pool waits, and inventory-service was the service nearest its limit (CPU peaking at 95%, 369 requests in flight). That is inferred from metrics; no profile was taken. Postgres showed no lock waits or deadlocks in any step.

## Soak

700 clients (70% of the 1000-client test point), SSE completion, 10 s warm-up, 5 minutes measured, one run. [data](data/test9/load-test.md)

- 12,736 checkouts offered, accepted and completed; 0 pending, 0 5xx, 0 transport errors.
- Throughput by minute (Prometheus), minutes 1-5: 37.9, 39.6, 40.4, 40.7, 42.0 completed/s. No downward drift.
- Outbox backlog peaked at 689 rows during ramp-up, stayed between 19 and 45, and drained to 0.
- Saga p95 stayed flat at about 29 s in the Prometheus histogram. That value sits near the top of the histogram's range, so it is a resolution ceiling; the runner's end-to-end p95 is 21.8 s.
- Not covered: memory growth, connection leaks and database growth over 30-60 minutes (database size growth was not measured).

## Changes and their impact

*Measured* means a before and after run exists. *Qualitative* means the earlier behaviour is documented and the fix is covered by tests, but no like-for-like measurement exists. Code references are tests that exercise the change.

| Change | Problem it fixed | Before | After | Evidence | Type |
|---|---|---|---|---|---|
| **Stuck-payment gap and grace policy** (`PAYMENT_UNCONFIRMED_GRACE`, default 5 min) | An unreachable or ambiguous gateway left payments undecided with no bounded outcome. | A transport error was treated like a timeout, so the failure surfaced only after the full 5-minute window. The saga went STUCK at about 12.5 min. | A request that never left the process fails in about 16 s and the saga compensates. An ambiguous charge is decided at window plus grace (about 10 min), before the saga gives up. A capture seen late is recorded for refund, never accepted. No blind retry of a possibly-successful charge. | [backlog](../docs/backlog.md) (EDGE-14/15, IMP-6), [test](../payment-service/pkg/services/unconfirmed_integration_test.go). Cloud: [test 3](data/test3/chaos-test.json), payment kill, 0 unfinished (no UNKNOWN payment arose). | Qualitative |
| **Bounded refund after payment** (`SAGA_STUCK_REFUND_AFTER`, default 10 min) | Payment taken but the order or stock step failed; the saga stayed STUCK until a person acted. | STUCK indefinitely, no automatic refund. | After 10 min: refund, cancel the paid order, release stock, in guarded steps. A late reply for the old step is ignored. Cost: a checkout whose replies were only lost is refunded and cancelled. | [backlog](../docs/backlog.md) (EDGE-1/2, IMP-14), [test](../payment-service/pkg/services/refund_after_payment_integration_test.go) | Qualitative |
| **Outbox dead state** (dead after 20 permanent failures; broker outages do not count) | A permanently unroutable message could be retried forever. | Not documented. | Dead after 20 permanent failures and no longer claimed. In the broker-restart fault test: 83 publish failures, 0 rows marked dead. | [test](../order-service/pkg/outbox/relay_test.go) (`TestPoisonMessageBecomesDeadButBrokerOutageDoesNotCount`), [test 3](data/test3/chaos-test.json) | Qualitative (after only) |
| **Idempotency-key validation and checkout limits** | Unbounded keys and leaked internal error text. | A 300-character key was accepted (202). Over-limit quantities returned an inventory-internal message. | Keys capped at 255 characters of letters, digits and `. _ : -`. The orchestrator validates 1-20 lines and quantity 1-100 and returns a clean error. | [backlog](../docs/backlog.md) (BUG-1/2), [test](../orchestrator-service/pkg/services/checkout/checkout_request_test.go) | Qualitative |
| **SSE vs polling completion** | Status polling by the clients loaded the orchestrator and distorted the measurement. | Poll mode: peak 36.9/s at 300 clients, falling to 20.9/s at 600. At 600 clients the orchestrator handled 197,692 requests, 401,606 DB pool waits, 98% mean CPU. | SSE mode: 37.6/s at 500 and 50.8/s at 1000 clients. At 1000 clients: 6,090 requests, 8,015 pool waits, 30% mean CPU. | [test 6](data/test6/) | Measured, partial pair: 600 vs 1000 clients, single runs, about 20% noise |
| **Purge loops** (outbox, processed markers, jobs, webhooks 14 d, attempts and sagas 30 d) | Bookkeeping tables grew without bound. | Kept forever (confirmed by row counts, not recorded). | Purged on a schedule. Orders, reservations, payment attempts and refunds are never purged. | [backlog](../docs/backlog.md) (EDGE-8), [test](../payment-service/pkg/retention/retention_integration_test.go) (one per service) | Qualitative. The soak did not measure database growth. |
| **Fail-closed webhooks** | Webhooks turned off silently when the secret was missing. | One log line, then polling only. | With `GATEWAY_PROVIDER=razorpay` and no secret the service refuses to start unless `WEBHOOKS_DISABLED=true` is set. | [backlog](../docs/backlog.md) (BUG-3) | Qualitative |
| **Saga edge cases closed** | Orphaned checkout attempts, a stale-read race in the reservation sweeper, `order.create` that could not fail cleanly, rejected refunds waiting forever. | Dead idempotency keys; possible stock release under a live saga; sagas stuck holding reservations. | The sweeper claims an orphan with one guarded update; order failures reply `Failed` and compensate; rejected refunds retry 3 times at 30-minute intervals; refunds with an unknown outcome are never auto-retried. | [backlog](../docs/backlog.md) (EDGE-6, 9-13), [test](../orchestrator-service/pkg/sweeper/sweeper_edge_integration_test.go) | Qualitative |

## What was ruled out

Experiments that produced no improvement. They are listed so the same dead ends are not repeated.

- **Four tuning variables (test 7).** 500 clients, SSE completion, three repetitions each. Baseline 42.9/s. Orchestrator DB pool 10 to 20: 39.8/s (borderline, -7%). Outbox poll 100 ms to 20 ms: 43.0/s. Payment `WORKER_CONCURRENCY` 4 to 8: 44.0/s. Inventory consumer concurrency 4 to 8: 44.2/s. Run-to-run spread is 2-7%, so only the DB-pool difference stands apart, and it is not an improvement. The limit was not identified. [data](data/test7/)
- **The fall after 300 clients in poll mode** is measurement load, not an application limit (197,692 status requests against 6,090 in SSE mode).
- **500 clients on one hot product** is an overload run: 151 of 2,184 requests were refused with 503. It is not a capacity measurement.
- **Two-replica throughput** was not measured. With two orchestrator replicas, correctness held (200 of 200 completed, 0 duplicates, the second replica performed 400 of 800 saga steps), but a first throughput attempt showed the SSE completion notification is replica-local, so completions handled by one replica never reach clients waiting on the other. [data](data/test8/chaos-test.md)

## Known limits

- **Simulated gateway, not Razorpay.** No real payment, real webhook over public HTTPS, or Razorpay Test Mode success path was exercised in the cloud runs.
- **Single replica** of every service; one broker and one database are single points of failure. This is a learning deployment, not a high-availability one.
- **SSE completion notification is single-replica.** The in-process hub (`orchestrator-service/pkg/statusstream`) cannot serve clients across replicas. Horizontal scaling needs a shared notification path first.
- **Short soak.** 5 minutes, one run.
- **One run per point** in most sweeps; the same-setting spread observed was 2-7% for repeated runs and about 20% for single sweep points.
- **No ambiguous payment was produced** during fault injection, so the "outcome unknown, never retried blindly" path is covered by tests, not by a cloud run.
- **Run records are incomplete.** The git commit, image tag and Neon branch were not recorded for these runs.

### Known issues, not yet diagnosed

- **`DatabasePoolSaturated` fired in most load runs**, including the 10-client baseline, where the orchestrator had 65 pool waits (1.1 s in total). The rule (`deploy/observability/prometheus/rules/alerts.yml`) fires on a utilisation above 0.9 **or on any pool wait in a one-minute window**. Hypothesis, untested: the second clause is too sensitive for pools of 10 connections. Real contention also occurred (for example, 401,606 waits in poll mode at 600 clients).
- **`ServiceDown` fired during runs with no fault injected.** Cause not established.
- The audit's first assertion prints "timed out waiting for the worker backlog" next to PASS. It appears in earlier passing audits as well and looks like a fixed label, but the audit code was not read to confirm.
