# Test report

All tests of the project and their results in this one file. Parts 1 to 5 below the summary list every test individually: unit and integration, correctness, load, failure cases and penetration. The raw result files (JSON and Markdown) were removed from the working tree; they remain in git history (commit `bbb4180`, folders `tests/*/data/`) and in the evidence archive.

**Environment.** AWS `ap-southeast-2`: one host per service (orchestrator, order, inventory, payment), one RabbitMQ host, one load-generator host, and a Neon PostgreSQL database. Every service ran as a single replica.

**The payment gateway is simulated.** All throughput and latency figures use the project's own gateway simulator, not Razorpay. In this campaign the simulator was configured with a 5 s pay delay (`SIM_PAY_DELAY`), so about 5 s of every checkout is a configured delay that no application change can remove. A checkout takes about 7 s end to end on an idle system.

**Terms.** *Accepted/s*: checkouts the API acknowledged (HTTP 202). *Completed/s*: checkouts that reached a final saga state. Where the two differ, a backlog is building. *Closed loop*: a fixed number of clients, each starting a new checkout when its previous one finishes. *Open loop*: checkouts arrive at a fixed rate whether or not earlier ones have finished.

## Verdicts

| Test | What it checks | Verdict | Where |
|---|---|---|---|
| Correctness suite | 20 active phases, three gateway modes (success, decline, timeout) | **PASS** with caveats (see below) | correctness |
| Fault injection | 7 faults under load | **PASS** (7 of 7) | failure cases |
| Oversell | 500 buyers for 10 units | **PASS** | oversell |
| Baseline | 10 clients, 3 repetitions | **PASS** | load 01 |
| Limit search | client and rate sweeps | **PASS** (curve measured; ceiling not reached in SSE mode) | load 02-04 |
| Bottleneck search | 4 tuning variables, 3 repetitions each | **PASS** as an experiment; **no variable moved throughput** | load 05 |
| Two orchestrator replicas | correctness with 2 replicas | **PASS** for correctness; throughput **not measured** | failure case 08 |
| Soak | 700 clients, 5 minutes | **PASS** (5 minutes only) | load 06 |
| Final audit | database invariants after all runs | **PASS** | final audit |
| Penetration | security attacks | **not performed** | penetration |

Unit and integration tests: 292 Go test functions across six modules (470 passes including subtests). Without a database, 334 pass and 130 database-gated tests skip; with a disposable PostgreSQL container, all 470 pass, 0 fail, 0 skipped, race detector on. Counted on 8 November 2026. Per-file detail: unit.

## Headline metrics

| # | Result | Caveat |
|---|---|---|
| 1 | **Fault tolerance: 7 of 7 scenarios passed** under 30 clients with 56-57 sagas in flight (service kills, broker restart, 20 s database connection drop, 200 ms network delay). 0 duplicate payments or reservations, 0 unfinished sagas, dead-queue growth 0. data | Judged on accepted checkouts only: when the orchestrator or inventory-service was down, 57 of 200 requests were refused and never accepted. Each scenario ran once. No ambiguous (UNKNOWN) payment occurred, so that path was not exercised. |
| 2 | **Oversell: 500 concurrent buyers for 10 units; exactly 10 accepted (HTTP 202), 490 refused (HTTP 409).** 0 deadlocks, audit clean. data | One run. |
| 3 | **Data integrity: 12,736 of 12,736 sagas COMPLETED**, orders PAID, payments SUCCEEDED and reservations CONFIRMED; 0 invariant violations in the final audit. data | The audit covers the last run's data (the soak), because each load run resets the fixtures. |
| 4 | **Sustained: 42.45 accepted/s and 42.45 completed/s over 5 minutes at 700 clients** (0 pending, 0 5xx). End-to-end p50 / p95 / p99: 16.8 / 21.8 / 25.2 s. data | Simulated gateway; one run; 5 minutes, not the 30-60 minutes planned; single replicas. |
| 5 | **Peak, single run, tested up to 1000 clients: 50.8 completed/s** at 1000 clients with SSE completion (3,045 checkouts, 0 pending). At 500 clients with SSE, two runs gave 37.6/s (test 6) and 42.9/s (test 7 baseline, spread 3.3%). data | 1000 clients is where the test stopped, not a measured ceiling. One run per point; repeating the 200-client step gave 25.1 and 31.2/s, so single points carry about 20% noise. |
| 6 | **Open loop: 10-30/s completed with flat latency; backlog from 40/s; collapse at 60/s** (19.0/s completed against 60.0/s offered). A sustainable rate is about 30/s. data | Poll completion; one run per rate. "About 30/s" is read from the jump in p99 between 30/s and 40/s. |
| 7 | **Correctness suite: all 20 active phases pass across the three gateway modes**; the database audit was clean after all four runs. data | No single run covers every phase (some phases apply to one mode only). The first decline run failed on the default payment window and passed with a 3 s window and 1 s reconciliation, so the decline result holds for those timings. |
| 8 | **Ruled out: four tuning variables did not move throughput** (see below). | The limit was not identified. |

**Closed loop and open loop are different measurements.** Closed loop (fixed clients) produced about 43/s at 500-700 clients because the clients throttle themselves as latency grows. Open loop (fixed arrival rate) shows what the system can absorb without a backlog: about 30/s. Neither is a ceiling for the other.

## Fault-tolerance results

200 checkouts per scenario at 50 per second from 30 clients; a fault is injected about 1.5 s in. data

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

500 clients submitted 500 attempts for one product with 10 units: 10 accepted (HTTP 202), 490 refused (HTTP 409), 0 transport errors, 0 5xx, 0 lock waiters, 0 deadlocks. Checkout latency p50 / p95 / p99: 465 / 546 / 552 ms. The 10 sold finished end to end in 7.4 s (p50) and 13.0 s (p95). The phase-22 database audit passed. data

The same command also ran a throughput case at 500 clients on one hot product. It offered 2,184 checkouts, accepted 2,033 and refused 151 with HTTP 503 (accepted rate 33.9/s). Nothing was oversold and no saga was left pending. This is an overload observation, not a capacity figure.

## Load sweep

All figures use the simulated gateway with its 5 s pay delay. Accepted equals completed in every closed-loop step, so one row is shown. data

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

**Idle baseline** (10 clients, 3 repetitions): 1.55 accepted = completed per s (spread 4.0%), end-to-end p95 11.3 s, checkout p95 about 66 ms. The payment step averaged 6.16 s of the 6.75 s saga; the simulator's pay delay accounts for most of it. data

**What saturates.** In poll mode the orchestrator is the busy service because the clients' own status polling sent it about 197,692 requests at 600 clients and exhausted its 10-connection database pool (401,606 pool waits). In SSE mode at 1000 clients it received 6,090 requests and 8,015 pool waits, and inventory-service was the service nearest its limit (CPU peaking at 95%, 369 requests in flight). That is inferred from metrics; no profile was taken. Postgres showed no lock waits or deadlocks in any step.

## Soak

700 clients (70% of the 1000-client test point), SSE completion, 10 s warm-up, 5 minutes measured, one run. data

- 12,736 checkouts offered, accepted and completed; 0 pending, 0 5xx, 0 transport errors.
- Throughput by minute (Prometheus), minutes 1-5: 37.9, 39.6, 40.4, 40.7, 42.0 completed/s. No downward drift.
- Outbox backlog peaked at 689 rows during ramp-up, stayed between 19 and 45, and drained to 0.
- Saga p95 stayed flat at about 29 s in the Prometheus histogram. That value sits near the top of the histogram's range, so it is a resolution ceiling; the runner's end-to-end p95 is 21.8 s.
- Not covered: memory growth, connection leaks and database growth over 30-60 minutes (database size growth was not measured).

## Changes and their impact

*Measured* means a before and after run exists. *Qualitative* means the earlier behaviour is documented and the fix is covered by tests, but no like-for-like measurement exists. Code references are tests that exercise the change.

| Change | Problem it fixed | Before | After | Evidence | Type |
|---|---|---|---|---|---|
| **Stuck-payment gap and grace policy** (`PAYMENT_UNCONFIRMED_GRACE`, default 5 min) | An unreachable or ambiguous gateway left payments undecided with no bounded outcome. | A transport error was treated like a timeout, so the failure surfaced only after the full 5-minute window. The saga went STUCK at about 12.5 min. | A request that never left the process fails in about 16 s and the saga compensates. An ambiguous charge is decided at window plus grace (about 10 min), before the saga gives up. A capture seen late is recorded for refund, never accepted. No blind retry of a possibly-successful charge. | [backlog](../docs/backlog.md) (EDGE-14/15, IMP-6), [test](../payment-service/pkg/services/unconfirmed_integration_test.go). Cloud: test 3, payment kill, 0 unfinished (no UNKNOWN payment arose). | Qualitative |
| **Bounded refund after payment** (`SAGA_STUCK_REFUND_AFTER`, default 10 min) | Payment taken but the order or stock step failed; the saga stayed STUCK until a person acted. | STUCK indefinitely, no automatic refund. | After 10 min: refund, cancel the paid order, release stock, in guarded steps. A late reply for the old step is ignored. Cost: a checkout whose replies were only lost is refunded and cancelled. | [backlog](../docs/backlog.md) (EDGE-1/2, IMP-14), [test](../payment-service/pkg/services/refund_after_payment_integration_test.go) | Qualitative |
| **Outbox dead state** (dead after 20 permanent failures; broker outages do not count) | A permanently unroutable message could be retried forever. | Not documented. | Dead after 20 permanent failures and no longer claimed. In the broker-restart fault test: 83 publish failures, 0 rows marked dead. | [test](../order-service/pkg/outbox/relay_test.go) (`TestPoisonMessageBecomesDeadButBrokerOutageDoesNotCount`), test 3 | Qualitative (after only) |
| **Idempotency-key validation and checkout limits** | Unbounded keys and leaked internal error text. | A 300-character key was accepted (202). Over-limit quantities returned an inventory-internal message. | Keys capped at 255 characters of letters, digits and `. _ : -`. The orchestrator validates 1-20 lines and quantity 1-100 and returns a clean error. | [backlog](../docs/backlog.md) (BUG-1/2), [test](../orchestrator-service/pkg/services/checkout/checkout_request_test.go) | Qualitative |
| **SSE vs polling completion** | Status polling by the clients loaded the orchestrator and distorted the measurement. | Poll mode: peak 36.9/s at 300 clients, falling to 20.9/s at 600. At 600 clients the orchestrator handled 197,692 requests, 401,606 DB pool waits, 98% mean CPU. | SSE mode: 37.6/s at 500 and 50.8/s at 1000 clients. At 1000 clients: 6,090 requests, 8,015 pool waits, 30% mean CPU. | test 6 | Measured, partial pair: 600 vs 1000 clients, single runs, about 20% noise |
| **Purge loops** (outbox, processed markers, jobs, webhooks 14 d, attempts and sagas 30 d) | Bookkeeping tables grew without bound. | Kept forever (confirmed by row counts, not recorded). | Purged on a schedule. Orders, reservations, payment attempts and refunds are never purged. | [backlog](../docs/backlog.md) (EDGE-8), [test](../payment-service/pkg/retention/retention_integration_test.go) (one per service) | Qualitative. The soak did not measure database growth. |
| **Fail-closed webhooks** | Webhooks turned off silently when the secret was missing. | One log line, then polling only. | With `GATEWAY_PROVIDER=razorpay` and no secret the service refuses to start unless `WEBHOOKS_DISABLED=true` is set. | [backlog](../docs/backlog.md) (BUG-3) | Qualitative |
| **Saga edge cases closed** | Orphaned checkout attempts, a stale-read race in the reservation sweeper, `order.create` that could not fail cleanly, rejected refunds waiting forever. | Dead idempotency keys; possible stock release under a live saga; sagas stuck holding reservations. | The sweeper claims an orphan with one guarded update; order failures reply `Failed` and compensate; rejected refunds retry 3 times at 30-minute intervals; refunds with an unknown outcome are never auto-retried. | [backlog](../docs/backlog.md) (EDGE-6, 9-13), [test](../orchestrator-service/pkg/sweeper/sweeper_edge_integration_test.go) | Qualitative |

## What was ruled out

Experiments that produced no improvement. They are listed so the same dead ends are not repeated.

- **Four tuning variables (test 7).** 500 clients, SSE completion, three repetitions each. Baseline 42.9/s. Orchestrator DB pool 10 to 20: 39.8/s (borderline, -7%). Outbox poll 100 ms to 20 ms: 43.0/s. Payment `WORKER_CONCURRENCY` 4 to 8: 44.0/s. Inventory consumer concurrency 4 to 8: 44.2/s. Run-to-run spread is 2-7%, so only the DB-pool difference stands apart, and it is not an improvement. The limit was not identified. data
- **The fall after 300 clients in poll mode** is measurement load, not an application limit (197,692 status requests against 6,090 in SSE mode).
- **500 clients on one hot product** is an overload run: 151 of 2,184 requests were refused with 503. It is not a capacity measurement.
- **Two-replica throughput** was not measured. With two orchestrator replicas, correctness held (200 of 200 completed, 0 duplicates, the second replica performed 400 of 800 saga steps), but a first throughput attempt showed the SSE completion notification is replica-local, so completions handled by one replica never reach clients waiting on the other. data

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


---

# Detailed results by test

- [Part 1: Unit and integration tests](#part-1-unit-and-integration-tests)
- [Part 2: Correctness tests](#part-2-correctness-tests)
- [Part 3: Load tests](#part-3-load-tests)
- [Part 4: Failure-case tests](#part-4-failure-case-tests)
- [Part 5: Penetration tests](#part-5-penetration-tests)


## Part 1: Unit and integration tests

### What is being tested

The behaviour of each Go module in isolation, close to the code: saga state transitions, idempotency, outbox and relay behaviour, retry and backoff, reservation arithmetic and concurrency, payment reconciliation and refunds, webhook handling, configuration guards, and the load-generator's own helpers. These are the tests that run on every push in CI.

Two kinds, labelled in every file below:

- **Unit** — runs with no external service.
- **Integration (needs Postgres)** — runs against a real PostgreSQL database. In a plain `go test ./...` these skip themselves; they run when `DATABASE_TEST_URL` points at a throwaway `*_test` database and the matching `RUN_*_INTEGRATION=1` flag is set (done by [`tools/integration/run-integration.sh`](../tools/integration/run-integration.sh) and the `integration` job in CI).

The `*_test.go` files stay next to the code they test. This folder documents them: one file per module, one section per test file, one row per test function.

### What has been done

Run on 8 November 2026 with `go test ./... -count=1 -v`, every test binary started from an empty directory (so no stray `.env` is read):

| Run | Passed | Failed | Skipped |
|---|---:|---:|---:|
| Plain run, no database | 334 | 0 | 130 |
| With a disposable PostgreSQL 17 container, all integration flags on, `-race`, one package at a time | **470** | **0** | **0** |

Counts include subtests. Counting top-level test functions only: 292 in total, all of which pass with the database. The 130 skips in the plain run are exactly the database-gated tests.

### Modules

| Module | Test files | Test functions | Unit | Integration (Postgres) | Result with database |
|---|---:|---:|---:|---:|---|
| `gateway-simulator` | 4 | 34 | 34 | 0 | all pass |
| `inventory-service` | 7 | 12 | 5 | 7 | all pass |
| `order-service` | 11 | 31 | 23 | 8 | all pass |
| `orchestrator-service` | 25 | 85 | 46 | 39 | all pass |
| `payment-service` | 22 | 106 | 58 | 48 | all pass |
| `tools` (load generator, correctness suite) | 9 | 24 | 24 | 0 | all pass |
| **Total** | **78** | **292** | **190** | **102** | **all pass** |

### Tests that map directly to a Step-3 change or a headline claim

| Behaviour | Test |
|---|---|
| No oversell of the last 10 units under 200 concurrent buyers | `TestConcurrentBuyersCannotOversellLastTenUnits` — inventory-service |
| A poisoned outbox row becomes dead; a broker outage does not count | `TestPoisonMessageBecomesDeadButBrokerOutageDoesNotCount` — order-service |
| Unreachable gateway past window + grace fails the attempt; inside grace it never guesses | `TestUnreachableGatewayPastWindowPlusGraceFailsTheAttempt`, `TestUnreachableGatewayInsideTheGraceKeepsAskingAndNeverGuesses` — payment-service |
| Bounded refund after payment | `refund_after_payment_integration_test.go` — payment-service |
| Idempotency-key and checkout-limit validation | `TestPrepareRejectsBadIdempotencyKeysAndLimitsWithCleanMessages` — orchestrator-service |
| Webhook secret required unless disabled by name | `TestRazorpayWithoutWebhookSecretRefusesToStartUnlessDisabledByName` — payment-service |

### Limits

- Passing tests show the scenarios they encode behave as written; they do not prove the absence of other defects.
- Mutation testing was not re-run for this report, so no claim is made about how many defects the tests would catch.
- Integration tests ran against PostgreSQL 17 in Docker, not Neon, and on a single machine.
- Database-backed results here are from the 8 November run; the cloud campaign of 1 November is documented in the other folders.

### Tests — `gateway-simulator`

4 test files · 34 top-level test functions · 34 pass · 34 unit, 0 integration (need Postgres)

"Kind" is *integration* when the test is skipped in a plain `go test` run and only runs against a database (`DATABASE_TEST_URL` plus the matching `RUN_*_INTEGRATION=1` flag). "What it checks" is the comment above the test function; where there is none it is derived from the test name and marked so.


##### [`internal/sim/plan_test.go`](../gateway-simulator/internal/sim/plan_test.go)

Package `gateway-simulator/internal/sim` · 7 test function(s) · 7 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestPlanValidation` | unit | PASS | – | Plan Validation *(from the test name)* |
| `TestScriptedDeclineThenCaptureUsesExactTimes` | unit | PASS | – | Scripted Decline Then Capture Uses Exact Times *(from the test name)* |
| `TestOrderPlanCanPayAnExistingDuplicate` | unit | PASS | – | Order Plan Can Pay An Existing Duplicate *(from the test name)* |
| `TestDeliveryOrderIsIndependentOfTransitionOrder` | unit | PASS | – | Delivery Order Is Independent Of Transition Order *(from the test name)* |
| `TestOmittedEventIsListedButNeverSent` | unit | PASS | – | Omitted Event Is Listed But Never Sent *(from the test name)* |
| `TestScriptedDuplicatesAreByteIdentical` | unit | PASS | – | Scripted Duplicates Are Byte Identical *(from the test name)* |
| `TestDifferentEventsAboutOnePaymentCanArriveConcurrently` | unit | PASS | – | Different Events About One Payment Can Arrive Concurrently *(from the test name)* |

##### [`internal/sim/refund_test.go`](../gateway-simulator/internal/sim/refund_test.go)

Package `gateway-simulator/internal/sim` · 7 test function(s) · 7 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestRefundOutcomesFollowTheScript` | unit | PASS | 3 | Refund Outcomes Follow The Script *(from the test name)* |
| `TestRefundIdempotencyKeyAndDuplicates` | unit | PASS | 3 | Refund Idempotency Key And Duplicates *(from the test name)* |
| `TestRefundResponseFaultsNeverUndoTheCommit` | unit | PASS | 5 | Refund Response Faults Never Undo The Commit *(from the test name)* |
| `TestDelayedVisibilityHidesTheRefundFromLookupsButNotFromTheAmount` | unit | PASS | – | Delayed Visibility Hides The Refund From Lookups But Not From The Amount *(from the test name)* |
| `TestRefundEventsComeFromTheStoredRefund` | unit | PASS | – | Refund Events Come From The Stored Refund *(from the test name)* |
| `TestRefundWebhookDeliveryCanBeDelayedOmittedAndReordered` | unit | PASS | – | Refund Webhook Delivery Can Be Delayed Omitted And Reordered *(from the test name)* |
| `TestRefundResponseFaultsOverHTTP` | unit | PASS | – | Refund Response Faults Over HTTP *(from the test name)* |

##### [`internal/sim/store_test.go`](../gateway-simulator/internal/sim/store_test.go)

Package `gateway-simulator/internal/sim` · 11 test function(s) · 11 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestCreateOrderValidation` | unit | PASS | 7 | Create Order Validation *(from the test name)* |
| `TestDuplicateReceiptCompatibility` | unit | PASS | 2 | Duplicate Receipt Compatibility *(from the test name)* |
| `TestLifecycleKeepsEveryReadEndpointConsistent` | unit | PASS | – | Lifecycle Keeps Every Read Endpoint Consistent *(from the test name)* |
| `TestScenarios` | unit | PASS | 3 | Scenarios *(from the test name)* |
| `TestManualCaptureAccount` | unit | PASS | – | Manual Capture Account *(from the test name)* |
| `TestReceiptLookupLag` | unit | PASS | – | Receipt Lookup Lag *(from the test name)* |
| `TestRefundRules` | unit | PASS | – | Refund Rules *(from the test name)* |
| `TestRefundRequiresCapture` | unit | PASS | – | Refund Requires Capture *(from the test name)* |
| `TestConcurrentRefundsNeverExceedTheCapturedAmount` | unit | PASS | – | Concurrent Refunds Never Exceed The Captured Amount *(from the test name)* |
| `TestDecisionsAreReproducibleAndIndependentOfCreationOrder` | unit | PASS | – | Decisions Are Reproducible And Independent Of Creation Order *(from the test name)* |
| `TestSchedulerRunsTasksInTimeOrder` | unit | PASS | – | Scheduler Runs Tasks In Time Order *(from the test name)* |

##### [`internal/sim/webhook_test.go`](../gateway-simulator/internal/sim/webhook_test.go)

Package `gateway-simulator/internal/sim` · 9 test function(s) · 9 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestDeliverySignsTheExactBytesAndSendsTheEventID` | unit | PASS | – | Delivery Signs The Exact Bytes And Sends The Event ID *(from the test name)* |
| `TestRetriesKeepTheEventIdentityAndPayload` | unit | PASS | – | Retries Keep The Event Identity And Payload *(from the test name)* |
| `TestAcknowledgementTimeoutIsRetried` | unit | PASS | – | Acknowledgement Timeout Is Retried *(from the test name)* |
| `TestUnreachableReceiverIsRetriedUntilTheMaxAge` | unit | PASS | – | Unreachable Receiver Is Retried Until The Max Age *(from the test name)* |
| `TestDeliveryConcurrencyIsBounded` | unit | PASS | – | Delivery Concurrency Is Bounded *(from the test name)* |
| `TestRedeliverySendsTheOriginalBytesAndEventID` | unit | PASS | – | Redelivery Sends The Original Bytes And Event ID *(from the test name)* |
| `TestEventsComeFromCommittedTransitionsAndKeepTheirSnapshot` | unit | PASS | – | Events Come From Committed Transitions And Keep Their Snapshot *(from the test name)* |
| `TestDeclineEmitsAFailedEventWithErrorFields` | unit | PASS | – | Decline Emits A Failed Event With Error Fields *(from the test name)* |
| `TestDuplicateOrderPolicyIsSeparateFromTheObservedReceiptBehaviour` | unit | PASS | 3 | Duplicate Order Policy Is Separate From The Observed Receipt Behaviour *(from the test name)* |

### Tests — `inventory-service`

7 test files · 12 top-level test functions · 12 pass · 5 unit, 7 integration (need Postgres)

"Kind" is *integration* when the test is skipped in a plain `go test` run and only runs against a database (`DATABASE_TEST_URL` plus the matching `RUN_*_INTEGRATION=1` flag). "What it checks" is the comment above the test function; where there is none it is derived from the test name and marked so.


##### [`pkg/observability/middleware_test.go`](../inventory-service/pkg/observability/middleware_test.go)

Package `inventory-service/pkg/observability` · 3 test function(s) · 3 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestCorrelationSurvivesHTTPAndAMQP` | unit | PASS | – | Correlation Survives HTTP And AMQP *(from the test name)* |
| `TestLoggerAddsCorrelationAndRedactsSecrets` | unit | PASS | – | Logger Adds Correlation And Redacts Secrets *(from the test name)* |
| `TestCorrelationJSONRoundTrip` | unit | PASS | – | Correlation JSON Round Trip *(from the test name)* |

##### [`pkg/retention/retention_integration_test.go`](../inventory-service/pkg/retention/retention_integration_test.go)

Package `inventory-service/pkg/retention` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestPurgeRemovesOnlyExpiredMessageBookkeeping` | integration (needs Postgres) | PASS | – | Purge Removes Only Expired Message Bookkeeping *(from the test name)* |

##### [`pkg/services/conflict_retry_test.go`](../inventory-service/pkg/services/conflict_retry_test.go)

Package `inventory-service/pkg/services` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestRetryOnConflictIsBounded` | unit | PASS | – | Retry On Conflict Is Bounded *(from the test name)* |
| `TestRetryOnConflictRetriesOnlyConflictsAndStopsOnSuccess` | unit | PASS | – | Retry On Conflict Retries Only Conflicts And Stops On Success *(from the test name)* |

##### [`pkg/services/deadlock_integration_test.go`](../inventory-service/pkg/services/deadlock_integration_test.go)

Package `inventory-service/pkg/services` · 3 test function(s) · 3 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestReversedCartsDeadlockWithoutLockOrder` | integration (needs Postgres) | PASS | – | REPRODUCTION: no sorted order, no retry (a single attempt) -> PostgreSQL aborts one with 40P01. |
| `TestReversedCartsDoNotDeadlockWithSortedLockOrder` | integration (needs Postgres) | PASS | – | FIX 1: the sorted lock order removes the cycle, so there is nothing to retry. |
| `TestDeadlockVictimIsRetriedAndSucceeds` | integration (needs Postgres) | PASS | – | FIX 2 (defence in depth): if a cycle ever forms again, the victim is re-run a bounded number of times and both checkouts succeed. |

##### [`pkg/services/oversell_integration_test.go`](../inventory-service/pkg/services/oversell_integration_test.go)

Package `inventory-service/pkg/services` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestConcurrentBuyersCannotOversellLastTenUnits` | integration (needs Postgres) | PASS | – | Two hundred buyers race for the final ten units. The conditional inventory update is the serialization point: exactly ten reservations may win. |

##### [`pkg/services/reservation_sweeper_integration_test.go`](../inventory-service/pkg/services/reservation_sweeper_integration_test.go)

Package `inventory-service/pkg/services` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestListOldReservedAndConcurrentReleaseIsIdempotent` | integration (needs Postgres) | PASS | – | List Old Reserved And Concurrent Release Is Idempotent *(from the test name)* |

##### [`pkg/services/restock_integration_test.go`](../inventory-service/pkg/services/restock_integration_test.go)

Package `inventory-service/pkg/services` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestReleasingAConfirmedReservationRestocksTheUnitsOnce` | integration (needs Postgres) | PASS | – | A checkout that was paid, then refunded and cancelled, gives its units back. |

### Tests — `order-service`

11 test files · 31 top-level test functions · 31 pass · 23 unit, 8 integration (need Postgres)

"Kind" is *integration* when the test is skipped in a plain `go test` run and only runs against a database (`DATABASE_TEST_URL` plus the matching `RUN_*_INTEGRATION=1` flag). "What it checks" is the comment above the test function; where there is none it is derived from the test name and marked so.


##### [`cmd/order-service/main_test.go`](../order-service/cmd/order-service/main_test.go)

Package `order-service/cmd/order-service` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestParseRole` | unit | PASS | – | Parse Role *(from the test name)* |

##### [`pkg/config/outbox_config_test.go`](../order-service/pkg/config/outbox_config_test.go)

Package `order-service/pkg/config` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestLoadOutboxConcurrency` | unit | PASS | – | Load Outbox Concurrency *(from the test name)* |
| `TestLoadOutboxPollInterval` | unit | PASS | – | Load Outbox Poll Interval *(from the test name)* |

##### [`pkg/config/rabbitmq_config_test.go`](../order-service/pkg/config/rabbitmq_config_test.go)

Package `order-service/pkg/config` · 3 test function(s) · 3 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestLoadRabbitMQConfigDefaultsConsumerConcurrency` | unit | PASS | – | Load Rabbit MQ Config Defaults Consumer Concurrency *(from the test name)* |
| `TestLoadRabbitMQConfigReadsConsumerConcurrency` | unit | PASS | – | Load Rabbit MQ Config Reads Consumer Concurrency *(from the test name)* |
| `TestLoadRabbitMQConfigRejectsInvalidConsumerConcurrency` | unit | PASS | 3 | Load Rabbit MQ Config Rejects Invalid Consumer Concurrency *(from the test name)* |

##### [`pkg/messaging/consumer_test.go`](../order-service/pkg/messaging/consumer_test.go)

Package `order-service/pkg/messaging` · 4 test function(s) · 4 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestConsumerAcknowledgesSuccessfulDelivery` | unit | PASS | – | Consumer Acknowledges Successful Delivery *(from the test name)* |
| `TestConsumerDeadLettersPermanentFailure` | unit | PASS | – | Consumer Dead Letters Permanent Failure *(from the test name)* |
| `TestConsumerRequeuesTransientFailureWhenRetryRepublishFails` | unit | PASS | – | Consumer Requeues Transient Failure When Retry Republish Fails *(from the test name)* |
| `TestNewConsumerValidatesRequiredValues` | unit | PASS | – | New Consumer Validates Required Values *(from the test name)* |

##### [`pkg/observability/middleware_test.go`](../order-service/pkg/observability/middleware_test.go)

Package `order-service/pkg/observability` · 3 test function(s) · 3 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestCorrelationSurvivesHTTPAndAMQP` | unit | PASS | – | Correlation Survives HTTP And AMQP *(from the test name)* |
| `TestLoggerAddsCorrelationAndRedactsSecrets` | unit | PASS | – | Logger Adds Correlation And Redacts Secrets *(from the test name)* |
| `TestCorrelationJSONRoundTrip` | unit | PASS | – | Correlation JSON Round Trip *(from the test name)* |

##### [`pkg/outbox/relay_test.go`](../order-service/pkg/outbox/relay_test.go)

Package `order-service/pkg/outbox` · 6 test function(s) · 6 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestRelayMarksConfirmedMessagePublished` | unit | PASS | – | Relay Marks Confirmed Message Published *(from the test name)* |
| `TestRelayReschedulesFailedPublish` | unit | PASS | – | Relay Reschedules Failed Publish *(from the test name)* |
| `TestPoisonMessageBecomesDeadButBrokerOutageDoesNotCount` | unit | PASS | – | Poison Message Becomes Dead But Broker Outage Does Not Count *(from the test name)* |
| `TestRetryDelayCapsExponentialBackoff` | unit | PASS | – | Retry Delay Caps Exponential Backoff *(from the test name)* |
| `TestRelayReleasesUnpublishedBatchOnShutdown` | unit | PASS | – | Relay Releases Unpublished Batch On Shutdown *(from the test name)* |
| `TestRelayConfigRequiresLeaseForWholeBatch` | unit | PASS | – | Relay Config Requires Lease For Whole Batch *(from the test name)* |

##### [`pkg/outbox/store_test.go`](../order-service/pkg/outbox/store_test.go)

Package `order-service/pkg/outbox` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestEnqueueValidatesMessage` | unit | PASS | 3 | Enqueue Validates Message *(from the test name)* |

##### [`pkg/retention/retention_integration_test.go`](../order-service/pkg/retention/retention_integration_test.go)

Package `order-service/pkg/retention` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestPurgeRemovesOnlyExpiredMessageBookkeeping` | integration (needs Postgres) | PASS | – | Purge Removes Only Expired Message Bookkeeping *(from the test name)* |

##### [`pkg/services/order_create_integration_test.go`](../order-service/pkg/services/order_create_integration_test.go)

Package `order-service/pkg/services` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestOrderCreateHandlerTransactionAndIdempotency` | integration (needs Postgres) | PASS | – | Order Create Handler Transaction And Idempotency *(from the test name)* |
| `TestOrderCreateHandlerReturnsTransientDatabaseErrors` | integration (needs Postgres) | PASS | – | Order Create Handler Returns Transient Database Errors *(from the test name)* |

##### [`pkg/services/order_create_test.go`](../order-service/pkg/services/order_create_test.go)

Package `order-service/pkg/services` · 3 test function(s) · 3 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestValidateOrderCreate` | unit | PASS | 8 | Validate Order Create *(from the test name)* |
| `TestOrderCreateHandlerClassifiesInvalidMessagesAsPermanent` | unit | PASS | – | Order Create Handler Classifies Invalid Messages As Permanent *(from the test name)* |
| `TestIsPermanentDatabaseError` | unit | PASS | 6 | Is Permanent Database Error *(from the test name)* |

##### [`pkg/services/order_failure_integration_test.go`](../order-service/pkg/services/order_failure_integration_test.go)

Package `order-service/pkg/services` · 5 test function(s) · 5 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestInvalidOrderCreateIsAnsweredFailedSoTheSagaCanCompensate` | integration (needs Postgres) | PASS | – | EDGE-11 / IMP-1: a command that can never succeed is answered Failed, not dead-lettered silently. |
| `TestCancelBeforeCreateLeavesATombstoneAndTheLateCreateIsRefused` | integration (needs Postgres) | PASS | – | EDGE-13 / IMP-2: cancel before create leaves a tombstone, and the late create does nothing. |
| `TestMarkPaidForAnOrderThatWasNeverCreatedIsAnsweredFailed` | integration (needs Postgres) | PASS | – | Mark Paid For An Order That Was Never Created Is Answered Failed *(from the test name)* |
| `TestExistingOrderWithDifferentContentsIsAnsweredFailed` | integration (needs Postgres) | PASS | – | EDGE-12 / IMP-3: an order that already exists with different contents is not silently accepted. |
| `TestPaidOrderIsCancelledOnlyWhenItsPaymentIsBeingRefunded` | integration (needs Postgres) | PASS | – | A paid order is cancelled only by the cancel of a refunded checkout. |

### Tests — `orchestrator-service`

25 test files · 85 top-level test functions · 85 pass · 46 unit, 39 integration (need Postgres)

"Kind" is *integration* when the test is skipped in a plain `go test` run and only runs against a database (`DATABASE_TEST_URL` plus the matching `RUN_*_INTEGRATION=1` flag). "What it checks" is the comment above the test function; where there is none it is derived from the test name and marked so.


##### [`pkg/admin/stuck_integration_test.go`](../orchestrator-service/pkg/admin/stuck_integration_test.go)

Package `orchestrator-service/pkg/admin` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestReviveSendsAStuckSagasStepAgainAndOnlyThat` | integration (needs Postgres) | PASS | – | Revive Sends A Stuck Sagas Step Again And Only That *(from the test name)* |

##### [`pkg/clients/inventory_client_test.go`](../orchestrator-service/pkg/clients/inventory_client_test.go)

Package `orchestrator-service/pkg/clients` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestNewInventoryClientFromEnvRequiresInventoryURL` | unit | PASS | – | New Inventory Client From Env Requires Inventory URL *(from the test name)* |

##### [`pkg/config/outbox_config_test.go`](../orchestrator-service/pkg/config/outbox_config_test.go)

Package `orchestrator-service/pkg/config` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestLoadOutboxConcurrency` | unit | PASS | – | Load Outbox Concurrency *(from the test name)* |
| `TestLoadOutboxPollInterval` | unit | PASS | – | Load Outbox Poll Interval *(from the test name)* |

##### [`pkg/config/saga_config_test.go`](../orchestrator-service/pkg/config/saga_config_test.go)

Package `orchestrator-service/pkg/config` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestLoadSagaConfigDefaults` | unit | PASS | – | Load Saga Config Defaults *(from the test name)* |
| `TestSagaConfigRejectsInvalidDurations` | unit | PASS | – | Saga Config Rejects Invalid Durations *(from the test name)* |

##### [`pkg/controllers/checkout_controller_test.go`](../orchestrator-service/pkg/controllers/checkout_controller_test.go)

Package `orchestrator-service/pkg/controllers` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestCreateCheckoutRejectsOversizedBody` | unit | PASS | – | Create Checkout Rejects Oversized Body *(from the test name)* |
| `TestGetCheckoutRejectsInvalidOrderID` | unit | PASS | – | Get Checkout Rejects Invalid Order ID *(from the test name)* |

##### [`pkg/controllers/checkout_sse_edge_test.go`](../orchestrator-service/pkg/controllers/checkout_sse_edge_test.go)

Package `orchestrator-service/pkg/controllers` · 8 test function(s) · 8 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestStreamForAnAlreadyTerminalSagaSendsOneEventAndClosesAtOnce` | unit | PASS | – | Stream For An Already Terminal Saga Sends One Event And Closes At Once *(from the test name)* |
| `TestStreamRejectsBadRequestsWithoutLeavingASubscriber` | unit | PASS | – | Stream Rejects Bad Requests Without Leaving A Subscriber *(from the test name)* |
| `TestStreamAtSubscriberCapacityAnswers503AndFreesTheSlotWhenAnotherEnds` | unit | PASS | – | Stream At Subscriber Capacity Answers503 And Frees The Slot When Another Ends *(from the test name)* |
| `TestClientDisconnectReleasesTheSubscriptionAndDoesNotLeakGoroutines` | unit | PASS | – | Client Disconnect Releases The Subscription And Does Not Leak Goroutines *(from the test name)* |
| `TestDuplicateAndStaleNotificationsNeverEmitRepeatedOrOlderVersions` | unit | PASS | – | Duplicate And Stale Notifications Never Emit Repeated Or Older Versions *(from the test name)* |
| `TestMidStreamSnapshotFailureEndsTheStreamAndReleasesTheSubscription` | unit | PASS | – | Mid Stream Snapshot Failure Ends The Stream And Releases The Subscription *(from the test name)* |
| `TestManyConcurrentStreamsAllReceiveTheTerminalEventAndLeaveNothingBehind` | unit | PASS | – | Many Concurrent Streams All Receive The Terminal Event And Leave Nothing Behind *(from the test name)* |
| `TestHeartbeatCommentsAreNotEventsAndKeepAnIdleStreamAlive` | unit | PASS | – | A raw reader makes sure the heartbeat comment is not mistaken for an event. |

##### [`pkg/controllers/checkout_sse_test.go`](../orchestrator-service/pkg/controllers/checkout_sse_test.go)

Package `orchestrator-service/pkg/controllers` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestStreamCheckoutSendsSnapshotThenCommittedChange` | unit | PASS | – | Stream Checkout Sends Snapshot Then Committed Change *(from the test name)* |

##### [`pkg/messaging/consumer_test.go`](../orchestrator-service/pkg/messaging/consumer_test.go)

Package `orchestrator-service/pkg/messaging` · 4 test function(s) · 4 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestConsumerAcknowledgesSuccessfulDelivery` | unit | PASS | – | Consumer Acknowledges Successful Delivery *(from the test name)* |
| `TestConsumerDeadLettersPermanentFailure` | unit | PASS | – | Consumer Dead Letters Permanent Failure *(from the test name)* |
| `TestConsumerRequeuesTransientFailureWhenRetryRepublishFails` | unit | PASS | – | Consumer Requeues Transient Failure When Retry Republish Fails *(from the test name)* |
| `TestNewConsumerValidatesRequiredValues` | unit | PASS | – | New Consumer Validates Required Values *(from the test name)* |

##### [`pkg/messaging/pool_test.go`](../orchestrator-service/pkg/messaging/pool_test.go)

Package `orchestrator-service/pkg/messaging` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestDesiredWorkersCanReachThirtyAndCapsThere` | unit | PASS | – | Desired Workers Can Reach Thirty And Caps There *(from the test name)* |

##### [`pkg/observability/middleware_test.go`](../orchestrator-service/pkg/observability/middleware_test.go)

Package `orchestrator-service/pkg/observability` · 3 test function(s) · 3 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestCorrelationSurvivesHTTPAndAMQP` | unit | PASS | – | Correlation Survives HTTP And AMQP *(from the test name)* |
| `TestLoggerAddsCorrelationAndRedactsSecrets` | unit | PASS | – | Logger Adds Correlation And Redacts Secrets *(from the test name)* |
| `TestCorrelationJSONRoundTrip` | unit | PASS | – | Correlation JSON Round Trip *(from the test name)* |

##### [`pkg/outbox/relay_edge_integration_test.go`](../orchestrator-service/pkg/outbox/relay_edge_integration_test.go)

Package `orchestrator-service/pkg/outbox` · 5 test function(s) · 5 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestTwoRelaysPublishEveryRowExactlyOnceWhenNothingCrashes` | integration (needs Postgres) | PASS | – | Two Relays Publish Every Row Exactly Once When Nothing Crashes *(from the test name)* |
| `TestRelayKilledAfterBrokerConfirmButBeforeTheMarkLosesNothingAndRepeatsWithTheSameMessageID` | integration (needs Postgres) | PASS | – | Relay Killed After Broker Confirm But Before The Mark Loses Nothing And Repeats With The Same Message ID *(from the test name)* |
| `TestAStaleRelayCannotMarkOrRescheduleARowAnotherRelayNowHolds` | integration (needs Postgres) | PASS | – | A Stale Relay Cannot Mark Or Reschedule A Row Another Relay Now Holds *(from the test name)* |
| `TestClaimedRowsAreNotClaimedAgainBeforeTheirLeaseExpires` | integration (needs Postgres) | PASS | – | Claimed Rows Are Not Claimed Again Before Their Lease Expires *(from the test name)* |
| `TestALeasedRowIsInvisibleToLaterClaimsUntilTheLeaseExpires` | integration (needs Postgres) | PASS | – | A Leased Row Is Invisible To Later Claims Until The Lease Expires *(from the test name)* |

##### [`pkg/outbox/relay_test.go`](../orchestrator-service/pkg/outbox/relay_test.go)

Package `orchestrator-service/pkg/outbox` · 5 test function(s) · 5 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestRelayMarksConfirmedMessagePublished` | unit | PASS | – | Relay Marks Confirmed Message Published *(from the test name)* |
| `TestRelayReschedulesFailedPublish` | unit | PASS | – | Relay Reschedules Failed Publish *(from the test name)* |
| `TestRetryDelayCapsExponentialBackoff` | unit | PASS | – | Retry Delay Caps Exponential Backoff *(from the test name)* |
| `TestRelayReleasesUnpublishedBatchOnShutdown` | unit | PASS | – | Relay Releases Unpublished Batch On Shutdown *(from the test name)* |
| `TestRelayConfigRequiresLeaseForWholeBatch` | unit | PASS | – | Relay Config Requires Lease For Whole Batch *(from the test name)* |

##### [`pkg/outbox/store_integration_test.go`](../orchestrator-service/pkg/outbox/store_integration_test.go)

Package `orchestrator-service/pkg/outbox` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestEnqueueUsesCallerTransaction` | integration (needs Postgres) | PASS | – | Enqueue Uses Caller Transaction *(from the test name)* |
| `TestUnpublishableRowStopsAtLimitAndCanBeRequeued` | integration (needs Postgres) | PASS | – | Unpublishable Row Stops At Limit And Can Be Requeued *(from the test name)* |

##### [`pkg/outbox/store_test.go`](../orchestrator-service/pkg/outbox/store_test.go)

Package `orchestrator-service/pkg/outbox` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestEnqueueValidatesMessage` | unit | PASS | 3 | Enqueue Validates Message *(from the test name)* |

##### [`pkg/retention/retention_integration_test.go`](../orchestrator-service/pkg/retention/retention_integration_test.go)

Package `orchestrator-service/pkg/retention` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestPurgeRemovesOnlyExpiredBookkeepingInBatches` | integration (needs Postgres) | PASS | – | Purge Removes Only Expired Bookkeeping In Batches *(from the test name)* |

##### [`pkg/saga/state_test.go`](../orchestrator-service/pkg/saga/state_test.go)

Package `orchestrator-service/pkg/saga` · 3 test function(s) · 3 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestNext` | unit | PASS | 27 | Next *(from the test name)* |
| `TestNextRejectsUndefinedTransitions` | unit | PASS | 3 | Next Rejects Undefined Transitions *(from the test name)* |
| `TestTransitionTableIsCompleteAndEveryDecisionCanReachATerminalState` | unit | PASS | – | Every documented step/outcome pair must be defined, and every non-terminal decision must still have a path to a terminal saga status. This makes adding a step without completing its transition table fail loudly. |

##### [`pkg/services/checkout/checkout_attempt_integration_test.go`](../orchestrator-service/pkg/services/checkout/checkout_attempt_integration_test.go)

Package `orchestrator-service/pkg/services/checkout` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestClaimCheckoutAttemptIsIdempotentAndRejectsHashConflict` | integration (needs Postgres) | PASS | – | Claim Checkout Attempt Is Idempotent And Rejects Hash Conflict *(from the test name)* |

##### [`pkg/services/checkout/checkout_query_integration_test.go`](../orchestrator-service/pkg/services/checkout/checkout_query_integration_test.go)

Package `orchestrator-service/pkg/services/checkout` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestGetCheckoutReturnsSagaAndOrderedSteps` | integration (needs Postgres) | PASS | – | Get Checkout Returns Saga And Ordered Steps *(from the test name)* |
| `TestGetCheckoutReturnsNotFound` | integration (needs Postgres) | PASS | – | Get Checkout Returns Not Found *(from the test name)* |

##### [`pkg/services/checkout/checkout_request_test.go`](../orchestrator-service/pkg/services/checkout/checkout_request_test.go)

Package `orchestrator-service/pkg/services/checkout` · 4 test function(s) · 4 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestPrepareCheckoutRequestSortsBeforeHashing` | unit | PASS | – | Prepare Checkout Request Sorts Before Hashing *(from the test name)* |
| `TestPrepareCheckoutRequestRejectsDuplicateProducts` | unit | PASS | – | Prepare Checkout Request Rejects Duplicate Products *(from the test name)* |
| `TestPrepareRejectsBadIdempotencyKeysAndLimitsWithCleanMessages` | unit | PASS | 5 | Prepare Rejects Bad Idempotency Keys And Limits With Clean Messages *(from the test name)* |
| `TestInventoryFailuresAreShownWithoutInventorysOwnText` | unit | PASS | – | Inventory Failures Are Shown Without Inventorys Own Text *(from the test name)* |

##### [`pkg/services/checkout/checkout_service_integration_test.go`](../orchestrator-service/pkg/services/checkout/checkout_service_integration_test.go)

Package `orchestrator-service/pkg/services/checkout` · 4 test function(s) · 4 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestCreateCheckoutCommitsSagaStepsOutboxAndAttempt` | integration (needs Postgres) | PASS | – | Create Checkout Commits Saga Steps Outbox And Attempt *(from the test name)* |
| `TestCreateCheckoutStoresDefinitiveInventoryFailure` | integration (needs Postgres) | PASS | – | Create Checkout Stores Definitive Inventory Failure *(from the test name)* |
| `TestCreateCheckoutLeavesAmbiguousInventoryFailureInProgress` | integration (needs Postgres) | PASS | – | Create Checkout Leaves Ambiguous Inventory Failure In Progress *(from the test name)* |
| `TestCreateCheckoutConcurrentDuplicateReturnsWinningResponse` | integration (needs Postgres) | PASS | – | Create Checkout Concurrent Duplicate Returns Winning Response *(from the test name)* |

##### [`pkg/services/reply/order_reply_integration_test.go`](../orchestrator-service/pkg/services/reply/order_reply_integration_test.go)

Package `orchestrator-service/pkg/services/reply` · 6 test function(s) · 6 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestHandlerAdvancesSagaAndIsIdempotent` | integration (needs Postgres) | PASS | – | Handler Advances Saga And Is Idempotent *(from the test name)* |
| `TestHandlerIgnoresReplyForInactiveStep` | integration (needs Postgres) | PASS | – | Handler Ignores Reply For Inactive Step *(from the test name)* |
| `TestHandlerAcceptsLatePaymentSuccess` | unit | PASS | 3 | Handler Accepts Late Payment Success *(from the test name)* |
| `TestHandlerMarksConflictingPaymentSuccessStuck` | unit | PASS | 2 | Handler Marks Conflicting Payment Success Stuck *(from the test name)* |
| `TestHandlerTreatsMissingSagaAsPermanentAndRollsBackMessage` | integration (needs Postgres) | PASS | – | Handler Treats Missing Saga As Permanent And Rolls Back Message *(from the test name)* |
| `TestFailStepMovesACreateOrderSagaIntoCompensationOnce` | integration (needs Postgres) | PASS | – | FailStep is what lets the sweeper compensate a CREATE_ORDER that can no longer succeed: the saga must cancel the order first (it may exist) and only then release the stock. |

##### [`pkg/services/reply/order_reply_test.go`](../orchestrator-service/pkg/services/reply/order_reply_test.go)

Package `orchestrator-service/pkg/services/reply` · 3 test function(s) · 3 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestHandlerClassifiesMalformedMessagesAsPermanent` | unit | PASS | – | Handler Classifies Malformed Messages As Permanent *(from the test name)* |
| `TestFinishedStepStatus` | unit | PASS | – | Finished Step Status *(from the test name)* |
| `TestIsPermanentDatabaseError` | unit | PASS | 6 | Is Permanent Database Error *(from the test name)* |

##### [`pkg/statusstream/hub_test.go`](../orchestrator-service/pkg/statusstream/hub_test.go)

Package `orchestrator-service/pkg/statusstream` · 3 test function(s) · 3 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestHubNotifiesMatchingSubscribersAndCleansUp` | unit | PASS | – | Hub Notifies Matching Subscribers And Cleans Up *(from the test name)* |
| `TestHubEnforcesSubscriberLimit` | unit | PASS | – | Hub Enforces Subscriber Limit *(from the test name)* |
| `TestHubConcurrentSubscribeNotifyAndCancel` | unit | PASS | – | Hub Concurrent Subscribe Notify And Cancel *(from the test name)* |

##### [`pkg/sweeper/sweeper_edge_integration_test.go`](../orchestrator-service/pkg/sweeper/sweeper_edge_integration_test.go)

Package `orchestrator-service/pkg/sweeper` · 11 test function(s) · 11 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestStuckUndoSagaWithoutAResendPayloadIsNeverRevivedAndSpendsItsCounter` | integration (needs Postgres) | PASS | – | Stuck Undo Saga Without A Resend Payload Is Never Revived And Spends Its Counter *(from the test name)* |
| `TestAdminReviveRefusesWhenThereIsNothingToResendAndChangesNothing` | integration (needs Postgres) | PASS | – | Admin Revive Refuses When There Is Nothing To Resend And Changes Nothing *(from the test name)* |
| `TestAdminReviveOnlyTouchesStuckSagasAndPicksTheRightRunningStatus` | integration (needs Postgres) | PASS | – | Admin Revive Only Touches Stuck Sagas And Picks The Right Running Status *(from the test name)* |
| `TestRevivedUndoSagaIsActuallyResentByTheNextSweep` | integration (needs Postgres) | PASS | – | Revived Undo Saga Is Actually Resent By The Next Sweep *(from the test name)* |
| `TestTwoSweepersNeverResendTheSameStepTwice` | integration (needs Postgres) | PASS | – | Two Sweepers Never Resend The Same Step Twice *(from the test name)* |
| `TestTwoSweepersMarkAnExhaustedSagaStuckOnce` | integration (needs Postgres) | PASS | – | Two Sweepers Mark An Exhausted Saga Stuck Once *(from the test name)* |
| `TestResendDelayGrowsThenStopsAtTheCapWithBoundedJitter` | unit | PASS | – | Resend Delay Grows Then Stops At The Cap With Bounded Jitter *(from the test name)* |
| `TestStepResendAtTheBackoffCapNeverSchedulesBeyondTheCap` | integration (needs Postgres) | PASS | – | Step Resend At The Backoff Cap Never Schedules Beyond The Cap *(from the test name)* |
| `TestReservationSweeperReleasesOnlyCompensatedAndOrphanedReservations` | integration (needs Postgres) | PASS | – | Reservation Sweeper Releases Only Compensated And Orphaned Reservations *(from the test name)* |
| `TestReservationSweeperKeepsGoingAfterOneReleaseFailsAndReportsIt` | integration (needs Postgres) | PASS | – | Reservation Sweeper Keeps Going After One Release Fails And Reports It *(from the test name)* |
| `TestReservationSweeperIsIdempotentAcrossRepeatedSweeps` | integration (needs Postgres) | PASS | – | Reservation Sweeper Is Idempotent Across Repeated Sweeps *(from the test name)* |

##### [`pkg/sweeper/sweeper_integration_test.go`](../orchestrator-service/pkg/sweeper/sweeper_integration_test.go)

Package `orchestrator-service/pkg/sweeper` · 9 test function(s) · 9 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestCreateOrderThatRunsOutOfAttemptsIsCompensatedNotStuck` | integration (needs Postgres) | PASS | – | Create Order That Runs Out Of Attempts Is Compensated Not Stuck *(from the test name)* |
| `TestCreateOrderPastItsDeadlineIsCompensatedAndLaterStepsAreStuck` | integration (needs Postgres) | PASS | – | Create Order Past Its Deadline Is Compensated And Later Steps Are Stuck *(from the test name)* |
| `TestWithoutAStepFailerTheOldBehaviourIsKept` | integration (needs Postgres) | PASS | – | Without A Step Failer The Old Behaviour Is Kept *(from the test name)* |
| `TestClaimOrphanClosesTheAttemptSoItsKeyAnswersCleanly` | integration (needs Postgres) | PASS | – | Claim Orphan Closes The Attempt So Its Key Answers Cleanly *(from the test name)* |
| `TestClaimOrphanLeavesALiveSagasReservationAlone` | integration (needs Postgres) | PASS | – | Claim Orphan Leaves A Live Sagas Reservation Alone *(from the test name)* |
| `TestClaimOrphanAllowsReleaseWhenNoSagaCanAppear` | integration (needs Postgres) | PASS | – | Claim Orphan Allows Release When No Saga Can Appear *(from the test name)* |
| `TestSagaStuckAfterPaymentIsRefundedOnlyAfterTheConfiguredTime` | integration (needs Postgres) | PASS | – | Saga Stuck After Payment Is Refunded Only After The Configured Time *(from the test name)* |
| `TestUnwindingAPaidCheckoutRunsRefundThenCancelThenReleaseAndALateReplyCannotCompleteIt` | integration (needs Postgres) | PASS | – | Unwinding A Paid Checkout Runs Refund Then Cancel Then Release And A Late Reply Cannot Complete It *(from the test name)* |
| `TestSagaStuckOnAnUndoStepIsRevivedAfterTheDelayAtMostThreeTimes` | integration (needs Postgres) | PASS | – | Saga Stuck On An Undo Step Is Revived After The Delay At Most Three Times *(from the test name)* |

### Tests — `payment-service`

22 test files · 106 top-level test functions · 106 pass · 58 unit, 48 integration (need Postgres)

"Kind" is *integration* when the test is skipped in a plain `go test` run and only runs against a database (`DATABASE_TEST_URL` plus the matching `RUN_*_INTEGRATION=1` flag). "What it checks" is the comment above the test function; where there is none it is derived from the test name and marked so.


##### [`cmd/payment-service/main_test.go`](../payment-service/cmd/payment-service/main_test.go)

Package `payment-service/cmd/payment-service` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestParseRole` | unit | PASS | – | Parse Role *(from the test name)* |

##### [`pkg/admin/review_integration_test.go`](../payment-service/pkg/admin/review_integration_test.go)

Package `payment-service/pkg/admin` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestRetryReviewRequiresANoteAndRefusesRecordsHoldingAProviderRefund` | integration (needs Postgres) | PASS | – | Retry Review Requires A Note And Refuses Records Holding A Provider Refund *(from the test name)* |
| `TestParkedEventsAreListedAndReplayedOnce` | integration (needs Postgres) | PASS | – | Parked Events Are Listed And Replayed Once *(from the test name)* |

##### [`pkg/config/webhook_config_test.go`](../payment-service/pkg/config/webhook_config_test.go)

Package `payment-service/pkg/config` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestRazorpayWithoutWebhookSecretRefusesToStartUnlessDisabledByName` | unit | PASS | – | Razorpay Without Webhook Secret Refuses To Start Unless Disabled By Name *(from the test name)* |
| `TestOtherProvidersNeverNeedAWebhookSecret` | unit | PASS | – | Other Providers Never Need A Webhook Secret *(from the test name)* |

##### [`pkg/config/worker_config_test.go`](../payment-service/pkg/config/worker_config_test.go)

Package `payment-service/pkg/config` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestLoadWorkerConfigDefaults` | unit | PASS | – | Load Worker Config Defaults *(from the test name)* |
| `TestWorkerConfigRejectsHandlerTimeoutAtLeastLease` | unit | PASS | – | Worker Config Rejects Handler Timeout At Least Lease *(from the test name)* |

##### [`pkg/jobs/backoff_test.go`](../payment-service/pkg/jobs/backoff_test.go)

Package `payment-service/pkg/jobs` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestRetryBackoffGrowthJitterAndStrictCap` | unit | PASS | – | Retry Backoff Growth Jitter And Strict Cap *(from the test name)* |

##### [`pkg/jobs/store_integration_test.go`](../payment-service/pkg/jobs/store_integration_test.go)

Package `payment-service/pkg/jobs` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestClaimConcurrentWorkers` | integration (needs Postgres) | PASS | – | Claim Concurrent Workers *(from the test name)* |

##### [`pkg/jobs/worker_test.go`](../payment-service/pkg/jobs/worker_test.go)

Package `payment-service/pkg/jobs` · 3 test function(s) · 3 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestWorkerMapsPaymentOutcomes` | unit | PASS | 4 | Worker Maps Payment Outcomes *(from the test name)* |
| `TestWorkerRecoversPaymentHandlerPanic` | unit | PASS | – | Worker Recovers Payment Handler Panic *(from the test name)* |
| `TestWorkerDrainsClaimedJobAfterShutdownSignal` | unit | PASS | – | Worker Drains Claimed Job After Shutdown Signal *(from the test name)* |

##### [`pkg/observability/middleware_test.go`](../payment-service/pkg/observability/middleware_test.go)

Package `payment-service/pkg/observability` · 3 test function(s) · 3 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestCorrelationSurvivesHTTPAndAMQP` | unit | PASS | – | Correlation Survives HTTP And AMQP *(from the test name)* |
| `TestLoggerAddsCorrelationAndRedactsSecrets` | unit | PASS | – | Logger Adds Correlation And Redacts Secrets *(from the test name)* |
| `TestCorrelationJSONRoundTrip` | unit | PASS | – | Correlation JSON Round Trip *(from the test name)* |

##### [`pkg/payment_gateway/razorpay_test.go`](../payment-service/pkg/payment_gateway/razorpay_test.go)

Package `payment-service/pkg/payment_gateway` · 11 test function(s) · 11 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestNewRazorpayValidatesCredentials` | unit | PASS | – | New Razorpay Validates Credentials *(from the test name)* |
| `TestCreateChargeCreatesAnOrderThatAwaitsPayment` | unit | PASS | – | Create Charge Creates An Order That Awaits Payment *(from the test name)* |
| `TestCreateChargeClassifiesFailures` | unit | PASS | 5 | Create Charge Classifies Failures *(from the test name)* |
| `TestCreateChargeTimeoutIsAmbiguous` | unit | PASS | – | Create Charge Timeout Is Ambiguous *(from the test name)* |
| `TestCreateChargeDuplicateReceiptReturnsTheExistingOrder` | unit | PASS | – | Create Charge Duplicate Receipt Returns The Existing Order *(from the test name)* |
| `TestCreateChargeDuplicateReceiptWithAnotherAmountIsRejected` | unit | PASS | – | Create Charge Duplicate Receipt With Another Amount Is Rejected *(from the test name)* |
| `TestLookupOrder` | unit | PASS | 4 | Lookup Order *(from the test name)* |
| `TestRefund` | unit | PASS | 5 | Refund *(from the test name)* |
| `TestRefundFetchListAndPaymentLookup` | unit | PASS | – | Refund Fetch List And Payment Lookup *(from the test name)* |
| `TestLookupOrderToleratesSeveralOrdersWithOneReceipt` | unit | PASS | – | Lookup Order Tolerates Several Orders With One Receipt *(from the test name)* |
| `TestGatewayNameSeparatesSimulatedFromRealEndpoints` | unit | PASS | – | Gateway Name Separates Simulated From Real Endpoints *(from the test name)* |

##### [`pkg/payment_gateway/testGateway_test.go`](../payment-service/pkg/payment_gateway/testGateway_test.go)

Package `payment-service/pkg/payment_gateway` · 6 test function(s) · 6 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestSimulatorSuccessRange` | unit | PASS | – | Simulator Success Range *(from the test name)* |
| `TestSimulatorDeclineRange` | unit | PASS | – | Simulator Decline Range *(from the test name)* |
| `TestSimulatorTimeoutRangeWaitsForContext` | unit | PASS | – | Simulator Timeout Range Waits For Context *(from the test name)* |
| `TestSimulatorHonorsCancellationDuringLatency` | unit | PASS | – | Simulator Honors Cancellation During Latency *(from the test name)* |
| `TestNewSimulatorFromEnv` | unit | PASS | – | New Simulator From Env *(from the test name)* |
| `TestNewSimulatorDefaultsToSuccess` | unit | PASS | – | New Simulator Defaults To Success *(from the test name)* |

##### [`pkg/retention/retention_integration_test.go`](../payment-service/pkg/retention/retention_integration_test.go)

Package `payment-service/pkg/retention` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestPurgeKeepsFinancialRecordsAndUnfinishedWork` | integration (needs Postgres) | PASS | – | Purge Keeps Financial Records And Unfinished Work *(from the test name)* |

##### [`pkg/services/compensation_worker_test.go`](../payment-service/pkg/services/compensation_worker_test.go)

Package `payment-service/pkg/services` · 11 test function(s) · 11 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestLateCaptureBecomesAConfirmedProcessedRefund` | integration (needs Postgres) | PASS | – | Late Capture Becomes A Confirmed Processed Refund *(from the test name)* |
| `TestPendingRefundStaysUnresolvedUntilProcessed` | integration (needs Postgres) | PASS | – | Pending Refund Stays Unresolved Until Processed *(from the test name)* |
| `TestLostSuccessfulResponseIsReconciledWithoutAnotherSubmission` | integration (needs Postgres) | PASS | – | Lost Successful Response Is Reconciled Without Another Submission *(from the test name)* |
| `TestWorkerStoppingAtAnyPointRecoversSafely` | unit | PASS | 3 | Worker Stopping At Any Point Recovers Safely *(from the test name)* |
| `TestUnknownOutcomeIsNeverReadAsNoRefund` | unit | PASS | 3 | Unknown Outcome Is Never Read As No Refund *(from the test name)* |
| `TestDefinitiveFailureFollowsTheRecoveryPolicy` | unit | PASS | 2 | Definitive Failure Follows The Recovery Policy *(from the test name)* |
| `TestConcurrentWorkersCannotOverRefund` | integration (needs Postgres) | PASS | – | Concurrent Workers Cannot Over Refund *(from the test name)* |
| `TestOnlyEstablishedRecordsAreRefunded` | unit | PASS | 3 | Only Established Records Are Refunded *(from the test name)* |
| `TestRefundWebhooksAndPollingCauseOneBusinessEffect` | integration (needs Postgres) | PASS | 1 | Refund Webhooks And Polling Cause One Business Effect *(from the test name)* |
| `TestGatewayEnvironmentsNeverCrossRoutes` | integration (needs Postgres) | PASS | 2 | Gateway Environments Never Cross Routes *(from the test name)* |
| `TestSettledAttemptAuditFindsAnExtraCaptureOnlyInsideTheWindow` | integration (needs Postgres) | PASS | – | Settled Attempt Audit Finds An Extra Capture Only Inside The Window *(from the test name)* |

##### [`pkg/services/payment_flow_test.go`](../payment-service/pkg/services/payment_flow_test.go)

Package `payment-service/pkg/services` · 8 test function(s) · 8 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestPaymentFlowOutcomes` | integration (needs Postgres) | PASS | 3 | Payment Flow Outcomes *(from the test name)* |
| `TestReclaimedPendingPaymentBecomesUnknownWithoutGatewayCall` | integration (needs Postgres) | PASS | – | Reclaimed Pending Payment Becomes Unknown Without Gateway Call *(from the test name)* |
| `TestSuccessfulGatewayResultUsesFreshWriteContext` | integration (needs Postgres) | PASS | – | Successful Gateway Result Uses Fresh Write Context *(from the test name)* |
| `TestRepeatedCreateReturnsSameAttemptAndOneProcessJob` | integration (needs Postgres) | PASS | – | Repeated Create Returns Same Attempt And One Process Job *(from the test name)* |
| `TestConcurrentCreateYieldsOnePaymentAndOneProcessJob` | integration (needs Postgres) | PASS | – | Concurrent Create Yields One Payment And One Process Job *(from the test name)* |
| `TestProcessingFinalPaymentDoesNotCallGatewayAgain` | integration (needs Postgres) | PASS | – | Processing Final Payment Does Not Call Gateway Again *(from the test name)* |
| `TestCreatePaymentValidation` | unit | PASS | 3 | Create Payment Validation *(from the test name)* |
| `TestJobHandlerNeverCallsGatewayForAnotherEnvironmentsAttempt` | integration (needs Postgres) | PASS | – | Job Handler Never Calls Gateway For Another Environments Attempt *(from the test name)* |

##### [`pkg/services/payment_initiate_integration_test.go`](../payment-service/pkg/services/payment_initiate_integration_test.go)

Package `payment-service/pkg/services` · 5 test function(s) · 5 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestSameInitiateMessageRedeliveredManyTimesCreatesOnePaymentAndOneJob` | integration (needs Postgres) | PASS | – | Same Initiate Message Redelivered Many Times Creates One Payment And One Job *(from the test name)* |
| `TestSameInitiateMessageDeliveredConcurrentlyCreatesOnePayment` | integration (needs Postgres) | PASS | – | Same Initiate Message Delivered Concurrently Creates One Payment *(from the test name)* |
| `TestResentInitiateWithANewMessageIdStillCreatesOnePayment` | integration (needs Postgres) | PASS | – | Resent Initiate With A New Message Id Still Creates One Payment *(from the test name)* |
| `TestResentInitiateForAnAlreadyFinishedPaymentRepeatsTheReplyButNeverTheCharge` | integration (needs Postgres) | PASS | – | Resent Initiate For An Already Finished Payment Repeats The Reply But Never The Charge *(from the test name)* |
| `TestInvalidInitiateCommandsAreRejectedPermanentlyAndLeaveNoTrace` | integration (needs Postgres) | PASS | – | Invalid Initiate Commands Are Rejected Permanently And Leave No Trace *(from the test name)* |

##### [`pkg/services/payment_processor_test.go`](../payment-service/pkg/services/payment_processor_test.go)

Package `payment-service/pkg/services` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestPaymentResultUpdates` | unit | PASS | 3 | Payment Result Updates *(from the test name)* |

##### [`pkg/services/payment_reconciler_test.go`](../payment-service/pkg/services/payment_reconciler_test.go)

Package `payment-service/pkg/services` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestPaymentResultUpdatesForAsyncGateway` | unit | PASS | – | Payment Result Updates For Async Gateway *(from the test name)* |
| `TestDecideReconcile` | unit | PASS | 23 | Decide Reconcile *(from the test name)* |

##### [`pkg/services/refund_after_payment_integration_test.go`](../payment-service/pkg/services/refund_after_payment_integration_test.go)

Package `payment-service/pkg/services` · 5 test function(s) · 5 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestRefundCommandRecordsTheFundingPaymentOnceAndTheWorkerRefundsItExactlyOnce` | integration (needs Postgres) | PASS | – | The saga asks for the refund of the payment that funded a checkout it could not complete. |
| `TestTheFundingPaymentIsNeverRefundedForAnyOtherReason` | integration (needs Postgres) | PASS | – | Only the saga's explicit request lets the funding payment be refunded: a polling finding never does. |
| `TestRefundCommandAnswersByWhatIsKnownAboutThePayment` | integration (needs Postgres) | PASS | 3 | Refund Command Answers By What Is Known About The Payment *(from the test name)* |
| `TestTheDispatcherRoutesByRoutingKeyAndRejectsOthers` | unit | PASS | – | The Dispatcher Routes By Routing Key And Rejects Others *(from the test name)* |
| `TestRejectedRefundInReviewIsRetriedAutomaticallyAFewTimesThenLeftToAPerson` | integration (needs Postgres) | PASS | – | Rejected Refund In Review Is Retried Automatically A Few Times Then Left To A Person *(from the test name)* |

##### [`pkg/services/unconfirmed_integration_test.go`](../payment-service/pkg/services/unconfirmed_integration_test.go)

Package `payment-service/pkg/services` · 6 test function(s) · 6 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestUnreachableGatewayPastWindowPlusGraceFailsTheAttempt` | integration (needs Postgres) | PASS | – | IMP-6: when the gateway cannot be asked for longer than the window plus the grace, the payment service decides FAILED (UNCONFIRMED) and tells the saga. |
| `TestUnreachableGatewayInsideTheGraceKeepsAskingAndNeverGuesses` | integration (needs Postgres) | PASS | – | Unreachable Gateway Inside The Grace Keeps Asking And Never Guesses *(from the test name)* |
| `TestUnconfirmedRuleCanBeSwitchedOff` | integration (needs Postgres) | PASS | – | Unconfirmed Rule Can Be Switched Off *(from the test name)* |
| `TestCaptureFoundAfterAnUnconfirmedFailureIsRefundedNotAccepted` | integration (needs Postgres) | PASS | – | A capture that turns up after the attempt was failed as UNCONFIRMED is refunded, never accepted. |
| `TestConnectionRefusedIsNotSentButATimeoutAfterSendingIsNot` | unit | PASS | – | Connection Refused Is Not Sent But A Timeout After Sending Is Not *(from the test name)* |
| `TestNotSentRequestIsRetriedAndLeavesTheAttemptCreated` | integration (needs Postgres) | PASS | – | Not Sent Request Is Retried And Leaves The Attempt Created *(from the test name)* |

##### [`pkg/services/webhook_processor_edge_integration_test.go`](../payment-service/pkg/services/webhook_processor_edge_integration_test.go)

Package `payment-service/pkg/services` · 8 test function(s) · 8 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestDecideReconcileTreatsTheExactDeadlineInstantAsLate` | unit | PASS | – | Decide Reconcile Treats The Exact Deadline Instant As Late *(from the test name)* |
| `TestDecideReconcileWithNoGatewayOrderExpiresExactlyAtTheDeadline` | unit | PASS | – | Decide Reconcile With No Gateway Order Expires Exactly At The Deadline *(from the test name)* |
| `TestDecideReconcileFallsBackToCreationTimePlusWindowWhenThereIsNoExpiry` | unit | PASS | – | Decide Reconcile Falls Back To Creation Time Plus Window When There Is No Expiry *(from the test name)* |
| `TestWebhookStormAcrossManyPaymentsAppliesEachCaptureExactlyOnce` | integration (needs Postgres) | PASS | – | Webhook Storm Across Many Payments Applies Each Capture Exactly Once *(from the test name)* |
| `TestWebhookStormOnOneOrderWithFourProcessorsCreatesOneReply` | integration (needs Postgres) | PASS | – | Webhook Storm On One Order With Four Processors Creates One Reply *(from the test name)* |
| `TestReplayingProcessedEventsChangesNothing` | integration (needs Postgres) | PASS | – | Replaying Processed Events Changes Nothing *(from the test name)* |
| `TestRepeatedCapturesOnACancelledCheckoutOweOneRefundPerPayment` | integration (needs Postgres) | PASS | – | Repeated Captures On A Cancelled Checkout Owe One Refund Per Payment *(from the test name)* |
| `TestCaptureForAnAmountThatDoesNotMatchIsNeverAcceptedEvenInAStorm` | integration (needs Postgres) | PASS | – | Capture For An Amount That Does Not Match Is Never Accepted Even In A Storm *(from the test name)* |

##### [`pkg/services/webhook_processor_integration_test.go`](../payment-service/pkg/services/webhook_processor_integration_test.go)

Package `payment-service/pkg/services` · 15 test function(s) · 15 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestCapturedWebhookCompletesThePaymentOnceUnderConcurrentDiscovery` | integration (needs Postgres) | PASS | – | Captured Webhook Completes The Payment Once Under Concurrent Discovery *(from the test name)* |
| `TestOlderAuthorizedEventDoesNotRegressACapturedPayment` | integration (needs Postgres) | PASS | – | Older Authorized Event Does Not Regress A Captured Payment *(from the test name)* |
| `TestFailedPaymentAttemptDoesNotFailTheCheckout` | integration (needs Postgres) | PASS | – | Failed Payment Attempt Does Not Fail The Checkout *(from the test name)* |
| `TestEventBeforeTheGatewayOrderMappingIsRetriedNotDiscarded` | integration (needs Postgres) | PASS | – | Event Before The Gateway Order Mapping Is Retried Not Discarded *(from the test name)* |
| `TestEventThatNeverMapsIsParkedAfterBoundedRetries` | integration (needs Postgres) | PASS | – | Event That Never Maps Is Parked After Bounded Retries *(from the test name)* |
| `TestDeadlinePolicyUsesOurObservationTimeNotProviderTimestamps` | unit | PASS | 3 | Deadline Policy Uses Our Observation Time Not Provider Timestamps *(from the test name)* |
| `TestCaptureNotifiedAfterCancellationDoesNotResurrectAndIsTracked` | integration (needs Postgres) | PASS | – | Capture Notified After Cancellation Does Not Resurrect And Is Tracked *(from the test name)* |
| `TestAdditionalCapturesGetTheirOwnCompensation` | unit | PASS | 2 | Additional Captures Get Their Own Compensation *(from the test name)* |
| `TestParkedEventIsReplayedAfterTheMappingAppears` | integration (needs Postgres) | PASS | – | Parked Event Is Replayed After The Mapping Appears *(from the test name)* |
| `TestOnlyNoMappingParkedEventsAreReplayed` | integration (needs Postgres) | PASS | – | Only No Mapping Parked Events Are Replayed *(from the test name)* |
| `TestExpiryAndWebhookRaceLeaveOneConsistentOutcome` | integration (needs Postgres) | PASS | – | The expiry decision and the webhook worker race for the same attempt, started together behind a barrier. Whoever commits first wins, and the other must leave one consistent outcome: never two replies, never captured money without a record. |
| `TestExpiryIsDeferredWhileAnOnTimeCaptureWaitsInTheInbox` | integration (needs Postgres) | PASS | – | Expiry Is Deferred While An On Time Capture Waits In The Inbox *(from the test name)* |
| `TestCapturedAmountOrCurrencyMismatchIsNotAccepted` | unit | PASS | 2 | Captured Amount Or Currency Mismatch Is Not Accepted *(from the test name)* |
| `TestProcessedMarkAndEffectsCommitTogether` | integration (needs Postgres) | PASS | – | Processed Mark And Effects Commit Together *(from the test name)* |
| `TestExpiryAndWebhookInEitherOrderAreConsistent` | unit | PASS | 2 | Both interleavings of the same race, forced explicitly rather than hoped for: the decision to expire is taken from a stale read in each case. |

##### [`pkg/services/webhook_processor_loop_test.go`](../payment-service/pkg/services/webhook_processor_loop_test.go)

Package `payment-service/pkg/services` · 4 test function(s) · 4 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestWebhookProcessorDrainsConsecutiveBatchesWithoutIdleDelay` | unit | PASS | – | Webhook Processor Drains Consecutive Batches Without Idle Delay *(from the test name)* |
| `TestWebhookProcessorWaitsWhenInboxIsIdle` | unit | PASS | – | Webhook Processor Waits When Inbox Is Idle *(from the test name)* |
| `TestNextWebhookErrorDelayBacksOffAndCaps` | unit | PASS | – | Next Webhook Error Delay Backs Off And Caps *(from the test name)* |
| `TestWaitForWebhookPassStopsPromptlyOnCancellation` | unit | PASS | – | Wait For Webhook Pass Stops Promptly On Cancellation *(from the test name)* |

##### [`pkg/webhooks/handler_test.go`](../payment-service/pkg/webhooks/handler_test.go)

Package `payment-service/pkg/webhooks` · 8 test function(s) · 8 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestVerifySignature` | unit | PASS | 7 | Verify Signature *(from the test name)* |
| `TestInvalidSignatureChangesNothing` | unit | PASS | – | Invalid Signature Changes Nothing *(from the test name)* |
| `TestAcceptedEventIsRecordedExactly` | unit | PASS | – | Accepted Event Is Recorded Exactly *(from the test name)* |
| `TestDuplicateDeliveryIsAcknowledgedWithoutASecondRecord` | unit | PASS | – | Duplicate Delivery Is Acknowledged Without A Second Record *(from the test name)* |
| `TestUnsupportedEventsAreAcknowledgedAndIgnoredDeliberately` | unit | PASS | – | Unsupported Events Are Acknowledged And Ignored Deliberately *(from the test name)* |
| `TestDurableAcceptanceFailureIsRetryable` | unit | PASS | – | Durable Acceptance Failure Is Retryable *(from the test name)* |
| `TestRequestValidation` | unit | PASS | – | Request Validation *(from the test name)* |
| `TestNewHandlerRequiresASecret` | unit | PASS | – | New Handler Requires A Secret *(from the test name)* |

### Tests — `tools`

9 test files · 24 top-level test functions · 24 pass · 24 unit, 0 integration (need Postgres)

"Kind" is *integration* when the test is skipped in a plain `go test` run and only runs against a database (`DATABASE_TEST_URL` plus the matching `RUN_*_INTEGRATION=1` flag). "What it checks" is the comment above the test function; where there is none it is derived from the test name and marked so.


##### [`correctness/docker_control_test.go`](../tools/correctness/docker_control_test.go)

Package `tools/correctness` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestDockerContainerActionThroughUnixSocket` | unit | PASS | – | Docker Container Action Through Unix Socket *(from the test name)* |
| `TestCorrectnessResetRequested` | unit | PASS | – | Correctness Reset Requested *(from the test name)* |

##### [`correctness/http_test.go`](../tools/correctness/http_test.go)

Package `tools/correctness` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestWaitPaymentContinuesThroughAwaitingPayment` | unit | PASS | – | Wait Payment Continues Through Awaiting Payment *(from the test name)* |

##### [`correctness/report_test.go`](../tools/correctness/report_test.go)

Package `tools/correctness` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestConsolidateReportsAcceptsCollections` | unit | PASS | – | Consolidate Reports Accepts Collections *(from the test name)* |

##### [`load-test/runner/container_metrics_test.go`](../tools/load-test/runner/container_metrics_test.go)

Package `tools/load-test/runner` · 3 test function(s) · 3 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestRecordComputesCPUFromConsecutiveSamples` | unit | PASS | – | A container with a large lifetime CPU total but no CPU use between two samples must read as idle: the old one-shot formula reported lifetime averages instead. |
| `TestFirstSampleOnlyEstablishesBaseline` | unit | PASS | – | First Sample Only Establishes Baseline *(from the test name)* |
| `TestMissingPostgresContainerIsOptionalAndExplained` | unit | PASS | – | Missing Postgres Container Is Optional And Explained *(from the test name)* |

##### [`load-test/runner/http_correlation_test.go`](../tools/load-test/runner/http_correlation_test.go)

Package `tools/load-test/runner` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestLoadClientAddsTestRunHeader` | unit | PASS | – | Load Client Adds Test Run Header *(from the test name)* |

##### [`load-test/runner/http_sse_test.go`](../tools/load-test/runner/http_sse_test.go)

Package `tools/load-test/runner` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestStreamTerminalReadsCompletedEvent` | unit | PASS | – | Stream Terminal Reads Completed Event *(from the test name)* |

##### [`load-test/runner/percentiles_test.go`](../tools/load-test/runner/percentiles_test.go)

Package `tools/load-test/runner` · 1 test function(s) · 1 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestSummarizeLatencies` | unit | PASS | – | Summarize Latencies *(from the test name)* |

##### [`load-test/runner/report_test.go`](../tools/load-test/runner/report_test.go)

Package `tools/load-test/runner` · 12 test function(s) · 12 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestCalculateBaselineVarianceIgnoresSmoke` | unit | PASS | – | Calculate Baseline Variance Ignores Smoke *(from the test name)* |
| `TestRunPassedRequiresExactCompletionAndCleanInvariants` | unit | PASS | – | Run Passed Requires Exact Completion And Clean Invariants *(from the test name)* |
| `TestRunPassedAcceptsExactC5bSellout` | unit | PASS | – | Run Passed Accepts Exact C5b Sellout *(from the test name)* |
| `TestSweepDetection` | unit | PASS | – | Sweep Detection *(from the test name)* |
| `TestExperimentMatrix` | unit | PASS | – | Experiment Matrix *(from the test name)* |
| `TestScenarioPointsSelectAndFlattenCases` | unit | PASS | – | Scenario Points Select And Flatten Cases *(from the test name)* |
| `TestRepeatHistoryKeepsEarlierRunsAndSpread` | unit | PASS | – | Repeat History Keeps Earlier Runs And Spread *(from the test name)* |
| `TestCheckDatabaseTargetRefusesUnlistedRemoteHosts` | unit | PASS | – | Check Database Target Refuses Unlisted Remote Hosts *(from the test name)* |
| `TestCustomSweepSteps` | unit | PASS | – | Custom Sweep Steps *(from the test name)* |
| `TestLoadConfigAcceptsC3SweepSteps` | unit | PASS | – | C3 takes its client count from each sweep step, so the case itself has no client count; this must not be rejected as "load parameters must be positive". |
| `TestClientOverrideAndConnectionCap` | unit | PASS | – | Client Override And Connection Cap *(from the test name)* |
| `TestParseCPUStatUsage` | unit | PASS | – | Parse CPU Stat Usage *(from the test name)* |

##### [`load-test/runner/workload_test.go`](../tools/load-test/runner/workload_test.go)

Package `tools/load-test/runner` · 2 test function(s) · 2 pass

| Test | Kind | Result | Subtests | What it checks |
|---|---|---|---:|---|
| `TestSkewedSelectorIsExactEightyTwenty` | unit | PASS | – | Skewed Selector Is Exact Eighty Twenty *(from the test name)* |
| `TestMultiItemSelectorUsesDistinctHotProducts` | unit | PASS | – | Multi Item Selector Uses Distinct Hot Products *(from the test name)* |

## Part 2: Correctness tests

### What is being tested

Whether the system keeps its **invariants** — not how fast it is. The rules under test:

- Stock is never negative and reserved never exceeds on-hand; concurrent buyers cannot oversell.
- Prices come from the server, never from the client.
- A repeated request with the same idempotency key creates one order, one reservation and one payment; the same key with different input is rejected.
- An order is PAID only if its payment SUCCEEDED; a declined payment cancels the order and releases the stock.
- A gateway timeout is *ambiguous*: the payment stays `UNKNOWN`, stock stays held, nothing is guessed or retried blindly.
- Crashes of a service in the middle of a step are recovered by the saga sweeper.
- No database transaction stays open while waiting on another service.
- Error responses map to stable HTTP status codes.

### What has been done

1. **Cloud correctness suite** — 22 numbered phases (20 active; phases 11 and 12 were retired when the `/pay` endpoint was removed) run against the live AWS staging stack in three gateway modes. Four runs are recorded: success, decline (default payment window), decline (rerun with a 3 s window) and timeout. The suite is [`tools/correctness`](../tools/correctness); each phase has its own file below.
2. **Oversell test** — 500 concurrent buyers for 10 units on the cloud stack: `oversell.md`.
3. **Final database audit** after every load and fault test: `final-audit.md`.

The same invariants are also covered at unit and integration level; see `../unit/`.

### Verdict

**PASS, with caveats.** Every one of the 20 active phases passed at least once, and the database audit was clean after all four runs. It was **not** one clean run:

- No single run covers every phase. A phase that does not apply to a gateway mode reports SKIP, not PASS (phases 08, 13, 16, 19 need `success`; 14 needs `decline`; 15 needs `timeout`).
- The first decline run failed (phases 09, 14, 21, 22) because the default payment window had not elapsed within the phase; it passed on a rerun with a 3 s window and 1 s reconciliation. The decline result therefore holds for those timings only.
- The dead queue grew by 2 rejected `reply.payment` messages in each of the decline and timeout runs; the cause is not verified.
- `DatabasePoolSaturated` fired in every run (see the known issues in `../REPORT.md`).

### Phases (status in each recorded run)

| Phase | Test | success | decline (default window) | decline (3 s window) | timeout |
|---:|---|:---:|:---:|:---:|:---:|
| 01 | Environment and migrations | PASS | PASS | PASS | PASS |
| 02 | Inventory validation and pricing | PASS | PASS | PASS | PASS |
| 03 | Atomic inventory rollback | PASS | PASS | PASS | PASS |
| 04 | Inventory idempotency | PASS | PASS | PASS | PASS |
| 05 | Confirm and release transitions | PASS | PASS | PASS | PASS |
| 06 | Checkout validation | PASS | PASS | PASS | PASS |
| 07 | Checkout idempotency and concurrency | PASS | PASS | PASS | PASS |
| 08 | Order command recovery | PASS | skip | skip | skip |
| 09 | Payment gateway outcome | PASS | **FAIL** | PASS | PASS |
| 10 | Payment idempotency and concurrency | PASS | PASS | PASS | PASS |
| 13 | Successful checkout end to end | PASS | skip | skip | skip |
| 14 | Declined payment end to end | skip | **FAIL** | PASS | skip |
| 15 | Ambiguous payment timeout | skip | skip | skip | PASS |
| 16 | Payment command recovery | PASS | skip | skip | skip |
| 17 | Concurrent limited-stock checkout | PASS | PASS | PASS | PASS |
| 18 | Multi-item deadlock resistance | PASS | PASS | PASS | PASS |
| 19 | Terminal order state idempotency | PASS | skip | skip | skip |
| 20 | HTTP error mapping | PASS | PASS | PASS | PASS |
| 21 | Transaction boundary | PASS | **FAIL** | PASS | PASS |
| 22 | Database invariants | PASS | **FAIL** | PASS | PASS |

Phases 11 and 12 are retired (they tested the removed `/pay` endpoint).

### Other tests in this folder

| Test | Verdict |
|---|---|
| Oversell: 500 buyers, 10 units | PASS |
| Final database audit | PASS |

### Phase 01 — Environment and migrations

**What is tested:** Checks service reachability, database schemas, inventory seed count, and the required empty payment table.

**Test code:** [`tools/correctness/phase01_environment.go`](../tools/correctness/phase01_environment.go)

**Counted result:** passes in 4 of 4 recorded runs (success; decline, default payment window; decline, 3 s window (rerun); timeout).

#### Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 0.05 | 4 | 5.515 |
| decline, default payment window | PASS | 0.03 | 4 | 5.231 |
| decline, 3 s window (rerun) | PASS | 0.07 | 4 | 5.646 |
| timeout | PASS | 0.04 | 4 | 3.669 |

#### Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| order service health | PASS | HTTP status=OK |
| orchestrator service health | PASS | HTTP status=OK |
| payment service health | PASS | HTTP status=OK |
| inventory service and seed | PASS | HTTP status=OK |
| 50 inventory products were seeded | PASS | expected exactly 50 |
| payments start empty | PASS | payment seeding must remain disabled |

### Phase 02 — Inventory validation and pricing

**What is tested:** Exercises valid reservation pricing plus empty, zero, negative, excessive, duplicate, and unknown-product requests.

**Test code:** [`tools/correctness/phase02_inventory_validation.go`](../tools/correctness/phase02_inventory_validation.go)

**Counted result:** passes in 4 of 4 recorded runs (success; decline, default payment window; decline, 3 s window (rerun); timeout).

#### Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 0.07 | 8 | 21.675 |
| decline, default payment window | PASS | 0.06 | 8 | 19.902 |
| decline, 3 s window (rerun) | PASS | 0.06 | 8 | 17.08 |
| timeout | PASS | 0.06 | 8 | 17.295 |

#### Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| valid reservation is created | PASS | HTTP status=Created |
| price and total come from inventory | PASS | client cannot choose price |
| empty cart is rejected | PASS | HTTP status=Bad Request |
| zero quantity is rejected | PASS | HTTP status=Bad Request |
| negative quantity is rejected | PASS | HTTP status=Bad Request |
| quantity over 100 is rejected | PASS | HTTP status=Bad Request |
| duplicate product is rejected | PASS | HTTP status=Bad Request |
| unknown product is rejected | PASS | HTTP status=Not Found |

### Phase 03 — Atomic inventory rollback

**What is tested:** Attempts a two-product reservation whose second line lacks stock and verifies no stock or reservation row is partially committed.

**Test code:** [`tools/correctness/phase03_inventory_rollback.go`](../tools/correctness/phase03_inventory_rollback.go)

**Counted result:** passes in 4 of 4 recorded runs (success; decline, default payment window; decline, 3 s window (rerun); timeout).

#### Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 0.19 | 1 | 12.161 |
| decline, default payment window | PASS | 0.11 | 1 | 11.424 |
| decline, 3 s window (rerun) | PASS | 0.16 | 1 | 10.622 |
| timeout | PASS | 0.17 | 1 | 11.525 |

#### Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| insufficient stock returns conflict | PASS | HTTP status=Conflict |
| first stock update rolled back | PASS | first product inventory must be unchanged |
| second stock is unchanged | PASS | second product inventory must be unchanged |
| reservation claim rolled back | PASS | no partial reservation |

### Phase 04 — Inventory idempotency

**What is tested:** Repeats the same order/hash, then changes the hash, checking response reuse, conflict detection, and a single stock mutation.

**Test code:** [`tools/correctness/phase04_inventory_idempotency.go`](../tools/correctness/phase04_inventory_idempotency.go)

**Counted result:** passes in 4 of 4 recorded runs (success; decline, default payment window; decline, 3 s window (rerun); timeout).

#### Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 0.11 | 4 | 36.153 |
| decline, default payment window | PASS | 0.06 | 4 | 18.017 |
| decline, 3 s window (rerun) | PASS | 0.08 | 4 | 17.012 |
| timeout | PASS | 0.08 | 4 | 14.849 |

#### Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| first request creates reservation | PASS | HTTP status=Created |
| same request reuses reservation | PASS | HTTP status=OK |
| different hash conflicts | PASS | HTTP status=Conflict |
| stock reserved once | PASS | reserved delta must equal 2 |

### Phase 05 — Confirm and release transitions

**What is tested:** Confirms one reservation and releases another twice, verifying idempotency and preventing contradictory terminal transitions.

**Test code:** [`tools/correctness/phase05_reservation_transitions.go`](../tools/correctness/phase05_reservation_transitions.go)

**Counted result:** passes in 4 of 4 recorded runs (success; decline, default payment window; decline, 3 s window (rerun); timeout).

#### Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 0.18 | 8 | 18.85 |
| decline, default payment window | PASS | 0.14 | 8 | 15.567 |
| decline, 3 s window (rerun) | PASS | 0.16 | 8 | 18.883 |
| timeout | PASS | 0.19 | 8 | 16.661 |

#### Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| confirm is idempotent | PASS | both calls return CONFIRMED |
| confirm moves stock once | PASS | on_hand decreases and reserved returns |
| confirmed reservation can be compensated | PASS | HTTP status=OK |
| confirmed compensation is idempotent | PASS | stock is restored exactly once |
| release is idempotent | PASS | both calls return RELEASED |
| release restores availability | PASS | all inventory counters return to starting values |

### Phase 06 — Checkout validation

**What is tested:** Validates a normal checkout and rejects missing idempotency keys, invalid quantities, duplicates, unknown products, and insufficient stock.

**Test code:** [`tools/correctness/phase06_checkout_validation.go`](../tools/correctness/phase06_checkout_validation.go)

**Counted result:** passes in 4 of 4 recorded runs (success; decline, default payment window; decline, 3 s window (rerun); timeout).

#### Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 0.15 | 8 | 49.032 |
| decline, default payment window | PASS | 0.1 | 8 | 40.137 |
| decline, 3 s window (rerun) | PASS | 0.12 | 8 | 39.231 |
| timeout | PASS | 0.09 | 8 | 22.561 |

#### Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| valid checkout is accepted by the saga | PASS | HTTP status=Accepted |
| missing idempotency key is rejected | PASS | HTTP status=Bad Request |
| empty cart mapping | PASS | HTTP status=Bad Request |
| zero quantity mapping | PASS | HTTP status=Bad Request |
| negative quantity mapping | PASS | HTTP status=Bad Request |
| duplicate product mapping | PASS | HTTP status=Bad Request |
| unknown product mapping | PASS | HTTP status=Not Found |
| insufficient stock mapping | PASS | HTTP status=Conflict |

### Phase 07 — Checkout idempotency and concurrency

**What is tested:** Checks sequential response replay, canonical item ordering, key/body conflicts, and concurrent retries of one logical checkout.

**Test code:** [`tools/correctness/phase07_checkout_idempotency.go`](../tools/correctness/phase07_checkout_idempotency.go)

**Counted result:** passes in 4 of 4 recorded runs (success; decline, default payment window; decline, 3 s window (rerun); timeout).

#### Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 0.23 | 17 | 81.262 |
| decline, default payment window | PASS | 0.2 | 17 | 50.95 |
| decline, 3 s window (rerun) | PASS | 0.21 | 17 | 50.893 |
| timeout | PASS | 0.17 | 17 | 51.465 |

#### Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| sequential retry replays the stored response | PASS | one stable order ID |
| key reuse with different body is rejected | PASS | HTTP status=Unprocessable Entity |
| item order is canonicalized | PASS | reordered cart is same request |
| concurrent retries share one saga | PASS | every retry returns 202 with the same order ID |
| concurrent retries reserve once | PASS | reserved delta must equal 1 |

### Phase 08 — Order command recovery

**What is tested:** Stops order-service, starts a checkout, waits for the saga sweeper to resend order.create, restarts order-service, and verifies one order is created and the saga completes.

**Test code:** [`tools/correctness/phase08_checkout_recovery.go`](../tools/correctness/phase08_checkout_recovery.go)

**Counted result:** passes in 1 of 4 recorded runs (success).

#### Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 34.26 | 18 | 9.648 |
| decline, default payment window | SKIP | 0.0 | 0 | 0 |
| decline, 3 s window (rerun) | SKIP | 0.0 | 0 | 0 |
| timeout | SKIP | 0.0 | 0 | 0 |

#### Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| checkout is accepted while order-service is down | PASS | HTTP status=Accepted |
| sweeper resends order.create | PASS | CREATE_ORDER attempts must reach 2 |
| no order exists while the participant is down | PASS | order creation is pending |
| saga completes after recovery | PASS | HTTP status=OK |
| resends create exactly one order | PASS | one order row |
| saga is completed | PASS | COMPLETED saga |
| stock moves exactly once | PASS | on_hand -1; reserved restored |

#### Skipped in other runs

- decline, 3 s window (rerun): Run with GATEWAY_MODE=success so the recovered saga reaches PAID.
- decline, default payment window: Run with GATEWAY_MODE=success so the recovered saga reaches PAID.
- timeout: Run with GATEWAY_MODE=success so the recovered saga reaches PAID.

### Phase 09 — Payment gateway outcome

**What is tested:** Creates a payment, observes asynchronous processing, and checks the terminal status for the configured simulator mode.

**Test code:** [`tools/correctness/phase09_payment_outcome.go`](../tools/correctness/phase09_payment_outcome.go)

**Counted result:** passes in 3 of 4 recorded runs (success; decline, 3 s window (rerun); timeout).

#### Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 1.07 | 22 | 4.649 |
| decline, default payment window | FAIL | 7.05 | 136 | 3.053 |
| decline, 3 s window (rerun) | PASS | 3.72 | 72 | 3.736 |
| timeout | PASS | 3.05 | 59 | 4.694 |

#### Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| payment attempt is created | PASS | HTTP status=Created |
| gateway result matches mode | PASS | expected SUCCEEDED, got SUCCEEDED |

#### Run that did not pass: decline, default payment window

Assertions that failed:

- **gateway result matches mode** — expected FAILED, got AWAITING_PAYMENT

In this run the payment stayed in `AWAITING_PAYMENT` because the default payment window had not elapsed, so the decline outcome did not become visible in time. The phase passed on the rerun with a 3 s payment window and 1 s reconciliation interval, so the decline result holds for those timings only.

### Phase 10 — Payment idempotency and concurrency

**What is tested:** Sends concurrent create-payment requests for one order and verifies one database row, one payment ID, and one asynchronous result.

**Test code:** [`tools/correctness/phase10_payment_idempotency.go`](../tools/correctness/phase10_payment_idempotency.go)

**Counted result:** passes in 4 of 4 recorded runs (success; decline, default payment window; decline, 3 s window (rerun); timeout).

#### Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 1.42 | 43 | 18.483 |
| decline, default payment window | PASS | 7.07 | 150 | 14.426 |
| decline, 3 s window (rerun) | PASS | 3.97 | 91 | 14.586 |
| timeout | PASS | 3.27 | 78 | 16.784 |

#### Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| one request creates and retries reuse | PASS | expected one 201, remaining 200, one ID |
| one payment row exists | PASS | database uniqueness |
| payment leaves CREATED/PENDING | PASS | SUCCEEDED |

### Phase 13 — Successful checkout end to end

**What is tested:** Runs the checkout saga through payment and inventory confirmation, then cross-checks order, payment, reservation, and stock records.

**Test code:** [`tools/correctness/phase13_success_e2e.go`](../tools/correctness/phase13_success_e2e.go)

**Counted result:** passes in 1 of 4 recorded runs (success).

#### Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 1.59 | 12 | 5.656 |
| decline, default payment window | SKIP | 0.0 | 0 | 0 |
| decline, 3 s window (rerun) | SKIP | 0.0 | 0 | 0 |
| timeout | SKIP | 0.0 | 0 | 0 |

#### Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| saga finishes its inventory step | PASS | saga status COMPLETED |
| order becomes paid | PASS | HTTP status=OK |
| one successful payment exists | PASS | payment must succeed |
| reservation is confirmed | PASS | reservation terminal state |
| confirmed stock is accounted once | PASS | on_hand -2, reserved restored |

#### Skipped in other runs

- decline, 3 s window (rerun): Run with GATEWAY_MODE=success to exercise the deterministic success path.
- decline, default payment window: Run with GATEWAY_MODE=success to exercise the deterministic success path.
- timeout: Run with GATEWAY_MODE=success to exercise the deterministic success path.

### Phase 14 — Declined payment end to end

**What is tested:** Checks that a declined payment cancels the order and releases, rather than consumes, reserved inventory.

**Test code:** [`tools/correctness/phase14_decline_e2e.go`](../tools/correctness/phase14_decline_e2e.go)

**Counted result:** passes in 1 of 4 recorded runs (decline, 3 s window (rerun)).

#### Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | SKIP | 0.0 | 0 | 0 |
| decline, default payment window | FAIL | 60.18 | 289 | 5.498 |
| decline, 3 s window (rerun) | PASS | 4.64 | 41 | 4.883 |
| timeout | SKIP | 0.0 | 0 | 0 |

#### Assertions (recorded in the decline, 3 s window (rerun) run)

| Assertion | Result | Detail |
|---|---|---|
| saga finishes its inventory step | PASS | saga status COMPENSATED |
| decline cancels order | PASS | HTTP status=OK |
| payment is failed | PASS | terminal payment |
| reservation is released | PASS | terminal reservation |
| decline consumes no stock | PASS | inventory returns to start |

#### Run that did not pass: decline, default payment window

Assertions that failed:

- **saga finishes its inventory step** — saga status COMPENSATED
- **decline cancels order** — HTTP status=OK
- **payment is failed** — terminal payment
- **reservation is released** — terminal reservation
- **decline consumes no stock** — inventory returns to start

In this run the payment stayed in `AWAITING_PAYMENT` because the default payment window had not elapsed, so the decline outcome did not become visible in time. The phase passed on the rerun with a 3 s payment window and 1 s reconciliation interval, so the decline result holds for those timings only.

#### Skipped in other runs

- success: Run a fresh disposable stack with GATEWAY_MODE=decline to execute this phase.
- timeout: Run a fresh disposable stack with GATEWAY_MODE=decline to execute this phase.

### Phase 15 — Ambiguous payment timeout

**What is tested:** Checks that an unknown gateway outcome keeps the order pending and its stock reserved for later reconciliation.

**Test code:** [`tools/correctness/phase15_timeout_e2e.go`](../tools/correctness/phase15_timeout_e2e.go)

**Counted result:** passes in 1 of 4 recorded runs (timeout).

#### Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | SKIP | 0.0 | 0 | 0 |
| decline, default payment window | SKIP | 0.0 | 0 | 0 |
| decline, 3 s window (rerun) | SKIP | 0.0 | 0 | 0 |
| timeout | PASS | 8.21 | 79 | 5.505 |

#### Assertions (recorded in the timeout run)

| Assertion | Result | Detail |
|---|---|---|
| timeout keeps order pending | PASS | HTTP status=OK |
| payment is unknown | PASS | ambiguous terminal payment |
| reservation remains reserved | PASS | do not release uncertain stock |
| stock remains held | PASS | reserved +2 |

#### Skipped in other runs

- decline, 3 s window (rerun): Run a fresh disposable stack with GATEWAY_MODE=timeout to execute this phase.
- decline, default payment window: Run a fresh disposable stack with GATEWAY_MODE=timeout to execute this phase.
- success: Run a fresh disposable stack with GATEWAY_MODE=timeout to execute this phase.

### Phase 16 — Payment command recovery

**What is tested:** Stops payment-service, starts a checkout, waits for the saga sweeper to resend payment.initiate, restarts payment-service, and verifies one payment and a completed saga.

**Test code:** [`tools/correctness/phase16_settlement_recovery.go`](../tools/correctness/phase16_settlement_recovery.go)

**Counted result:** passes in 1 of 4 recorded runs (success).

#### Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 35.96 | 24 | 9.483 |
| decline, default payment window | SKIP | 0.0 | 0 | 0 |
| decline, 3 s window (rerun) | SKIP | 0.0 | 0 | 0 |
| timeout | SKIP | 0.0 | 0 | 0 |

#### Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| checkout is accepted while payment-service is down | PASS | HTTP status=Accepted |
| sweeper resends payment.initiate | PASS | TAKE_PAYMENT attempts must reach 2 |
| order stays pending while payment is unavailable | PASS | never paid without a confirmed payment |
| stock is held but not consumed | PASS | reserved +2; on_hand unchanged |
| saga completes after recovery | PASS | HTTP status=OK |
| resends create one successful payment | PASS | exactly one payment attempt |
| reservation becomes confirmed | PASS | settlement repaired |
| stock moves exactly once | PASS | on_hand -2; reserved restored |

#### Skipped in other runs

- decline, 3 s window (rerun): Run with GATEWAY_MODE=success so the recovered saga reaches PAID.
- decline, default payment window: Run with GATEWAY_MODE=success so the recovered saga reaches PAID.
- timeout: Run with GATEWAY_MODE=success so the recovered saga reaches PAID.

### Phase 17 — Concurrent limited-stock checkout

**What is tested:** Runs 50 distinct checkouts against ten units and verifies exactly ten winners, no oversell, and no negative counters.

**Test code:** [`tools/correctness/phase17_limited_stock.go`](../tools/correctness/phase17_limited_stock.go)

**Counted result:** passes in 4 of 4 recorded runs (success; decline, default payment window; decline, 3 s window (rerun); timeout).

#### Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 0.39 | 50 | 222.112 |
| decline, default payment window | PASS | 0.33 | 50 | 186.981 |
| decline, 3 s window (rerun) | PASS | 0.31 | 50 | 149.268 |
| timeout | PASS | 0.3 | 50 | 138.295 |

#### Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| exactly available stock wins | PASS | expected 10 created and 40 conflicts |
| stock is not oversold | PASS | on_hand=10 reserved=10 allocated=0 |
| ten orders were created | PASS | one order per unit |

### Phase 18 — Multi-item deadlock resistance

**What is tested:** Submits concurrent two-item carts in opposite input order and verifies canonical locking prevents deadlocks and timeouts.

**Test code:** [`tools/correctness/phase18_deadlock.go`](../tools/correctness/phase18_deadlock.go)

**Counted result:** passes in 4 of 4 recorded runs (success; decline, default payment window; decline, 3 s window (rerun); timeout).

#### Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 0.33 | 20 | 244.987 |
| decline, default payment window | PASS | 0.28 | 20 | 215.73 |
| decline, 3 s window (rerun) | PASS | 0.28 | 20 | 202.104 |
| timeout | PASS | 0.32 | 20 | 203.804 |

#### Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| all opposite-order carts complete | PASS | no deadlocks, transport timeouts, or conflicts |
| both products reserved exactly | PASS | each product reserved 20 |

### Phase 19 — Terminal order state idempotency

**What is tested:** Replays the checkout of a PAID order and verifies the same order ID, stable state, one payment, and one inventory movement.

**Test code:** [`tools/correctness/phase19_terminal_states.go`](../tools/correctness/phase19_terminal_states.go)

**Counted result:** passes in 1 of 4 recorded runs (success).

#### Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 4.74 | 27 | 8.484 |
| decline, default payment window | SKIP | 0.0 | 0 | 0 |
| decline, 3 s window (rerun) | SKIP | 0.0 | 0 | 0 |
| timeout | SKIP | 0.0 | 0 | 0 |

#### Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| order reaches paid | PASS | HTTP status=OK |
| replay returns the same order | PASS | stable order ID |
| replay leaves the order paid | PASS | stable terminal state |
| replay creates no payment | PASS | one payment attempt |
| replay moves stock once | PASS | single-unit movement |

#### Skipped in other runs

- decline, 3 s window (rerun): The default terminal-state assertion is deterministic under GATEWAY_MODE=success; decline is covered by phase 14.
- decline, default payment window: The default terminal-state assertion is deterministic under GATEWAY_MODE=success; decline is covered by phase 14.
- timeout: The default terminal-state assertion is deterministic under GATEWAY_MODE=success; decline is covered by phase 14.

### Phase 20 — HTTP error mapping

**What is tested:** Sends malformed IDs, missing resources, invalid payment data, and checkout conflicts to verify stable client-facing status codes.

**Test code:** [`tools/correctness/phase20_http_mapping.go`](../tools/correctness/phase20_http_mapping.go)

**Counted result:** passes in 4 of 4 recorded runs (success; decline, default payment window; decline, 3 s window (rerun); timeout).

#### Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 0.06 | 8 | 37.642 |
| decline, default payment window | PASS | 0.05 | 8 | 35.021 |
| decline, 3 s window (rerun) | PASS | 0.06 | 8 | 34.681 |
| timeout | PASS | 0.05 | 8 | 29.986 |

#### Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| bad order ID | PASS | HTTP status=Bad Request |
| missing order | PASS | HTTP status=Not Found |
| bad payment ID | PASS | HTTP status=Bad Request |
| missing payment | PASS | HTTP status=Not Found |
| invalid payment amount | PASS | HTTP status=Bad Request |
| invalid currency length | PASS | HTTP status=Bad Request |
| idempotency body conflict | PASS | HTTP status=Unprocessable Entity |

### Phase 21 — Transaction boundary

**What is tested:** Samples pg_stat_activity during the checkout saga and fails if an application connection waits on network I/O while idle inside a database transaction.

**Test code:** [`tools/correctness/phase21_transaction_scope.go`](../tools/correctness/phase21_transaction_scope.go)

**Counted result:** passes in 3 of 4 recorded runs (success; decline, 3 s window (rerun); timeout).

#### Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 4.16 | 22 | 4.885 |
| decline, default payment window | FAIL | 60.1 | 145 | 6.469 |
| decline, 3 s window (rerun) | PASS | 10.07 | 43 | 5.237 |
| timeout | PASS | 39.18 | 144 | 5.439 |

#### Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| actionable background jobs drain before sampling | PASS | timed out waiting for the worker backlog |
| no idle transaction crosses service calls | PASS | observed idle-in-transaction samples=0 |

#### Run that did not pass: decline, default payment window

Assertions that failed:

- **actionable background jobs drain before sampling** — timed out waiting for the worker backlog

In this run the worker backlog from the earlier phases had not drained when the phase ran, so it timed out. The phase passed on the rerun with a 3 s payment window and 1 s reconciliation interval, so the decline result holds for those timings only.

### Phase 22 — Database invariants

**What is tested:** Scans all three schemas for negative/oversold stock, duplicate business keys, and cross-service terminal-state contradictions.

**Test code:** [`tools/correctness/phase22_invariants.go`](../tools/correctness/phase22_invariants.go)

**Counted result:** passes in 3 of 4 recorded runs (success; decline, 3 s window (rerun); timeout).

#### Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 0.57 | 0 | 0 |
| decline, default payment window | FAIL | 10.1 | 0 | 0 |
| decline, 3 s window (rerun) | PASS | 0.72 | 0 | 0 |
| timeout | PASS | 0.2 | 0 | 0 |

#### Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| actionable background jobs drain before audit | PASS | timed out waiting for the worker backlog |
| inventory counters stay within bounds | PASS | violations=0 |
| one reservation per order | PASS | violations=0 |
| one payment per order | PASS | violations=0 |
| one product line per order | PASS | violations=0 |
| paid orders have successful payment | PASS | violations=0 |
| cancelled orders have failed payment | PASS | violations=0 |
| paid reservations are confirmed | PASS | violations=0 |
| cancelled reservations are released | PASS | violations=0 |
| unknown payments retain pending orders | PASS | violations=0 |
| payment amounts match order totals in paise | PASS | violations=0 |

#### Run that did not pass: decline, default payment window

Assertions that failed:

- **actionable background jobs drain before audit** — timed out waiting for the worker backlog

In this run the worker backlog from the earlier phases had not drained when the phase ran, so it timed out. The phase passed on the rerun with a 3 s payment window and 1 s reconciliation interval, so the decline result holds for those timings only.

### Correctness test — Oversell on the cloud stack

**Result:** PASS

#### What is tested

That concurrent buyers can never be sold more units than exist. 500 clients compete for one product that has only 10 units. Exactly 10 must succeed and the other 490 must be refused with HTTP 409; stock must never go below zero.

#### How it is run

Case `c5b-hot-product-sellout`: `LOAD_CLIENTS=500`, `LOAD_ORDERS=500`, `LOAD_HOT_STOCK=10`, `LOAD_MAX_CONNS=1000`, run from the load-generator host against the AWS staging stack with the gateway simulator in `success` mode. The unit-level counterpart is `TestConcurrentBuyersCannotOversellLastTenUnits` ([`oversell_integration_test.go`](../inventory-service/pkg/services/oversell_integration_test.go)), 200 goroutines buying the last 10 units.

#### Result

| Check | Result |
|---|---|
| Attempts offered | 500 |
| `HTTP 202` accepted | **10** |
| `HTTP 409` out of stock | **490** |
| Transport errors, 5xx, decode errors | 0 |
| Completed | 10 (10 paid, 0 cancelled, 0 pending at the drain) |
| Invariant "consumes exactly the available stock, rejects the remainder with 409" | **PASS** |
| Lock waiters, deadlocks | 0 |
| Checkout latency p50 / p95 / p99 | 465 / 546 / 552 ms |
| End-to-end latency for the 10 sold (p50 / p95) | 7.4 s / 13.0 s |
| Phase-22 database audit | **PASS** (1 of 1) |

Only 10 checkouts needed the full saga; the other 490 ended at the inventory step. The end-to-end figures of 7 to 13 s come from 500 clients hitting a few services at once, not from the payment path.

#### Limits

- One sample: no repeat, so no variance figure.
- Simulated gateway; one product and one load shape. Multi-product and multi-item contention is covered by correctness phases 17 and 18.
- The other case run by the same command (500 clients on one hot product, throughput) refused 151 requests with 503; see failure case 11.

### Correctness test — Final database audit

**Result:** PASS

#### What is tested

After all load and fault tests, a cross-schema audit (correctness phase 22) checks that the database contains no contradictory or duplicated state, and an observability audit checks counters (stuck sagas, dead outbox rows, unknown payments).

#### How it is run

The phase-22 audit ran against the staging database after the soak, so it covers the data of the last load run; each load run resets the data first, so earlier runs were audited immediately after they finished (their audits are in the other tests' data folders). The 8 invariant queries used by the fault harness also ran before and after every fault scenario.

#### Result

| Check | Result |
|---|---|
| Phase-22 audit, all assertions | **PASS**, 0 violations in each |
| Observability audit | PASS, no violations, no alerts firing at the time |
| Stuck sagas, dead outbox rows, dead-queue messages, unknown or unconfirmed payments, incomplete refunds, stock violations | all 0 |
| Sagas | 12,736, all COMPLETED |
| Orders / payments / reservations | 12,736 PAID / 12,736 SUCCEEDED / 12,736 CONFIRMED |
| Minimum free stock; products with negative stock | 999,745; none |
| Dead queue `distri.commands.dead` | 0 messages |

Invariants checked: stock counters within bounds; one reservation per order; one payment per order; no duplicate product line per order; paid orders have a successful payment; cancelled orders have a failed payment; paid reservations are confirmed; cancelled reservations are released.

#### Limits

- The audit reads the data of one run (the soak).
- It checks database invariants and observability counters; it does not check the broker volume, backups or restores.
- The audit's first assertion prints "timed out waiting for the worker backlog" next to PASS. The same text appears in earlier passing audits and looks like a fixed label; the audit code was not read to confirm.

## Part 3: Load tests

### What is being tested

How throughput and latency change as load grows, where they stop scaling, and whether the system stays correct and stable while loaded. Two numbers are always reported separately: **accepted/s** (requests answered with HTTP 202) and **completed/s** (checkouts that reached a final saga state). A fast 202 with a growing backlog is not scaling.

### What has been done

Six load tests on the AWS staging stack on 1 November 2026 (one host per service, one broker host, Neon database, single replicas), driven from a separate load-generator host by the Go load generator in [`tools/load-test`](../tools/load-test).

**Every result here uses the simulated gateway.** The simulator was configured with a 5 s pay delay, so about 5 s of each checkout (about 7 s end to end when idle) is a configured delay that no application change removes. These figures are not Razorpay throughput and not real payment latency.

Two measurement styles are used:

- **Closed loop:** a fixed number of clients, each starting a new checkout when its previous one finishes. Throughput is roughly clients ÷ time per checkout.
- **Open loop:** checkouts arrive at a fixed rate whether or not earlier ones finished. This reveals backlog and collapse.

### Verdict

| Result | Value | Peak or sustained |
|---|---|---|
| Idle baseline, 10 clients | 1.55 completed/s, end-to-end p95 11.3 s | sustained (3 × 60 s) |
| Poll-mode peak | 36.9/s at 300 clients, then falling | peak, single run |
| SSE-mode peak | 50.8/s at 1000 clients (tested up to 1000, not a ceiling) | peak, single run |
| 500 clients, SSE | 37.6/s (sweep) and 42.9/s (3-run baseline) | range across runs |
| Open-loop capacity | about 30/s sustainable; backlog from 40/s; collapse at 60/s | single run per rate |
| Soak, 700 clients | 42.45 accepted/s and 42.45 completed/s | sustained over 5 minutes |
| Tuning variables | none of four moved throughput | 3 repetitions each |

### Tests

| # | Test | Verdict |
|---|---|---|
| 01 | Idle baseline (10 clients, 3 repetitions) | PASS |
| 02 | Client sweep, poll completion (1 to 600 clients) | PASS; peak at 300 clients, fall caused by polling load |
| 03 | Client sweep, SSE completion (500, 1000 clients) | PASS; 50.8/s at 1000 clients |
| 04 | Rate sweep, open loop (10 to 60 per second) | PASS; ~30/s sustainable |
| 05 | Bottleneck experiment (4 variables × 3 runs) | PASS as an experiment; no variable moved throughput |
| 06 | Soak (700 clients, 5 minutes) | PASS; 5 minutes only |

The overload run (500 clients on a single hot product, which refused 151 requests with 503) is documented as failure case 11. The oversell run is a correctness test.

### Limits

- Simulated gateway; not a statement about real payment throughput.
- Single replica of every service; one broker and one database.
- Most sweep points are single runs; repeating the 200-client step gave 25.1 and 31.2/s, so single points carry about 20% noise.
- The soak lasted 5 minutes; slow leaks and database growth are not ruled out.
- The limit behind the ~43/s closed-loop figure at 500-700 clients was not identified.
- Run records (git commit, image tag, Neon branch) were not captured for these runs.

### Load test 01 — Idle baseline (10 clients)

**Result:** PASS. Not a target: a reference point for later tests.

#### What is tested

How the deployed stack behaves at low load: how long one checkout takes when nothing is saturated, and whether any invariant fails.

#### How it is run

- Case C2: 10 closed-loop clients, 10 s warm-up, 60 s measured, repeated 3 times.
- Between repetitions the runner resets transactional data and inventory. Purchases are spread round-robin over all 50 products.
- Run from the load-generator host against the AWS staging services; gateway simulator in `success` mode with 200 ms API latency and a 5 s pay delay.

#### Results

| | Run 1 | Run 2 | Run 3 | Mean | Spread (CV) |
|---|---:|---:|---:|---:|---:|
| Offered = accepted = completed | 94 | 97 | 88 | 93 | – |
| Accepted = completed per second | 1.57 | 1.62 | 1.47 | **1.55** | 4.0% |
| End-to-end p95 | 11.1 s | 10.2 s | 12.6 s | **11.3 s** | 8.5% |
| Pending at the drain | 0 | 0 | 0 | – | – |

Run 1 in detail: checkout p50 / p95 / p99 = 38 / 54 / 84 ms; end-to-end p50 / p95 / p99 = 6.9 / 11.1 / 15.7 s. All 94 checkouts returned HTTP 202 with no 5xx, no transport errors and no cancelled orders. Little's law holds (10.9 workflows in flight observed against 10.9 expected), so the measurement is internally consistent.

#### Where the time goes

Mean saga step durations over the three repetitions (342 sagas, Prometheus):

| Step | Mean |
|---|---:|
| create order | 0.16 s |
| confirm stock | 0.33 s |
| mark order paid | 0.14 s |
| **take payment** | **6.16 s** |
| saga total | 6.75 s |

The simulator's configured 5 s pay delay (`SIM_PAY_DELAY`) accounts for most of the payment step. About 1.2 s of the payment step is not explained by what was measured. The accept call is fast (about 66 ms p95 including a database write), so database distance is not the explanation for the latency.

| Service (run 1) | CPU mean / max | DB pool waits |
|---|---|---:|
| orchestrator | 7% / 13% | 65 (1.1 s total) |
| order | 2.5% / 7.8% | 0 |
| inventory | 1.4% / 6.8% | 0 |
| payment | 3.5% / 9.4% | 0 |

No service is busy. Throughput here is clients divided by latency (10 / 6.9 s ≈ 1.5 per second).

#### Invariants and audit

Stock bounds, one reservation and payment per order, paid orders have a successful payment and a confirmed reservation, cancelled orders have failed payments: all pass in every repetition. The phase-22 audit afterwards passes. The observability audit shows 0 stuck sagas, 0 dead outbox rows, 0 unknown or unconfirmed payments, 0 stock violations.

#### Limits

- Simulated gateway; the 7 s median is mostly the simulator's pay delay.
- 10 clients only: this says nothing about behaviour near the limit.
- Three short repetitions (88-97 workflows each).
- The `DatabasePoolSaturated` and `ServiceDown` alerts fired during this run (not diagnosed).
- Git commit, image tag and Neon branch were not recorded.

### Load test 02 — Client sweep, poll completion

**Result:** PASS. Throughput peaks at 300 clients, then falls; the fall is caused by the measurement method.

#### What is tested

How completed checkouts per second grow as the number of concurrent clients increases, with the load generator discovering completion by polling the orchestrator's status endpoint every 100 ms.

#### How it is run

- Case C3, closed loop: 1, 2, 5, 10, 20, 50, 100, 200 (twice), 300, 400 and 600 clients; 10 s warm-up, 60 s measured per step; one run per point.
- AWS staging stack; gateway simulator in `success` mode with its 5 s pay delay.
- The sweep ran as two invocations on the same build (1-200 and 200-600 clients).

#### Results

| Clients | 1 | 2 | 5 | 10 | 20 | 50 | 100 | 200 | 200 (repeat) | 300 | 400 | 600 |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| Accepted = completed per s | 0.17 | 0.30 | 0.85 | 1.63 | 3.32 | 7.85 | 15.8 | 25.1 | 31.2 | **36.9** | 31.1 | 20.9 |
| End-to-end p95 (s) | 8.1 | 10.6 | 10.6 | 10.7 | 9.9 | 10.5 | 11.2 | 15.7 | 11.0 | 13.7 | 20.1 | 43.4 |

Throughput is near-linear up to 100 clients (each checkout spends about 6 s in the simulator, so throughput ≈ clients / 6 s), then bends. The 200-client step ran twice and gave 25.1 and 31.2/s, so **single points vary by about 20%**. Every workflow that was accepted completed in every step; accepted equals completed.

#### Why throughput falls after 300 clients

| Orchestrator, 600 clients, poll mode | |
|---|---:|
| CPU mean / max | 98% / 132% |
| Requests during the step | 197,692 |
| Database pool waits (pool of 10) | 401,606 |

The orchestrator is busy only because the clients' own status polling sent it about 200,000 requests and exhausted its connection pool. The fall after 300 clients is the measurement loading the application, not the application's limit. See load test 03 for the same stack measured without polling.

#### Limits

- One run per point except the 200-client step.
- Simulated gateway with a 5 s pay delay.
- Poll numbers are a lower bound; use SSE numbers for capacity.

### Load test 03 — Client sweep, SSE completion (peak)

**Result:** PASS. Peak **50.8 completed/s at 1000 clients** (single run, tested up to 1000 clients). 1000 is where the test stopped, not a measured ceiling.

#### What is tested

How far throughput scales when the orchestrator pushes completion to clients over Server-Sent Events, so the load generator does not load the application with polling.

#### How it is run

Case C3, closed loop, 500 and 1000 clients; 10 s warm-up, 60 s measured; one run per point. AWS staging stack; gateway simulator with its 5 s pay delay.

#### Results

| Clients | 500 | 1000 |
|---|---:|---:|
| Accepted = completed per s | 37.6 | **50.8** |
| End-to-end p95 / p99 (s) | 20.0 / 23.8 | 31.6 / 36.4 |
| Checkout p50 / p95 (ms) | 33 / 1503 | 37 / 1845 |
| Completed; pending at drain | 2,258; 0 | 3,045; 0 |
| SSE attempts / terminal events / polling fallbacks (1000 clients) | – | 3,045 / 3,045 / 0 |

At 500 clients the same configuration gave 37.6/s here and 42.9/s in the bottleneck baseline (three repetitions, spread 3.3%). Both are reported; the gap is within the roughly 20% noise of single points.

#### What saturates (1000 clients, SSE)

| Service | |
|---|---|
| orchestrator CPU mean / max | 30% / 104% |
| orchestrator requests during the step | 6,090 |
| orchestrator DB pool waits | 8,015 |
| inventory CPU mean / max | 16% / **95%** |
| inventory requests in flight (max) | 369 |
| payment, order | idle (payment 24% mean) |

Inventory-service is the service nearest its limit. That is an inference from metrics, not a profile of the code. Postgres showed no lock waits or deadlocks.

#### Limits

- Single runs; an earlier run of 29 October on an older build reached 60/s at 1000 clients, and no reason for the difference was established.
- No SSE run above 1000 clients, so where SSE throughput flattens is unknown.
- Latency includes the simulator's 5 s pay delay.
- The load generator host was not checked for saturation at 1000 clients.

### Load test 04 — Rate sweep (open loop)

**Result:** PASS as a measurement. A sustainable rate is about **30/s**; a backlog builds from 40/s; the system collapses at 60/s.

#### What is tested

How many checkouts per second the stack can absorb when arrivals are fixed (open loop), regardless of whether earlier checkouts have finished. This exposes the difference between *accepted* and *completed* rates.

#### How it is run

Case C4: offered rates of 10, 20, 30, 40, 50 and 60 per second; 10 s warm-up, 60 s measured; poll completion; one run per rate. AWS staging stack; simulated gateway with its 5 s pay delay.

#### Results

| Offered/s | 10 | 20 | 30 | 40 | 50 | 60 |
|---|---:|---:|---:|---:|---:|---:|
| Accepted per s | 10.0 | 20.0 | 30.0 | 40.0 | 50.0 | 60.0 |
| Completed per s | 10.0 | 20.0 | 30.0 | 40.0 | 50.0 | **19.0** |
| End-to-end p99 (s) | 13.8 | 13.9 | 13.7 | 51.7 | 130.6 | 175.9 |
| Max in flight | 79 | 145 | 261 | 858 | 1190 | 1641 |
| 5xx / transport errors | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 |

- Up to 30/s the system keeps up with flat latency.
- From 40/s a backlog builds: p99 rises from 14 s to 52 s, then 131 s at 50/s, although every offered request is still completed during the run.
- At 60/s: 1,801 checkouts accepted, **571 completed, 1,230 still pending at the drain**. A fast 202 with a growing backlog is not capacity.

#### Reading this next to the closed-loop numbers

Closed loop (a fixed number of clients, each waiting for its own checkout) shows about 43/s at 500-700 clients, because clients slow down as latency grows. Open loop (fixed arrivals) shows about 30/s is the rate the stack sustains without a backlog. They measure different things.

#### Limits

- One run per rate; poll completion also loads the orchestrator.
- "About 30/s" is read from the jump in p99 between 30/s and 40/s, not from a search for the exact edge.
- Simulated gateway.

### Load test 05 — Bottleneck experiment

**Result:** the experiment ran as designed and its answer is negative: **none of the four variables moved throughput**. The limit was not found.

#### What is tested

Whether any of four obvious tuning knobs is the limit at 500 clients. One variable is changed per run, everything else stays at its default, and each setting is repeated three times to see run-to-run spread.

#### How it is run

- Load: case C2 at 500 closed-loop clients, SSE completion, 60 s measured, 3 repetitions per setting; AWS staging stack, simulated gateway with its 5 s pay delay.
- After each experiment the setting was reset and the service restarted with the original values.

#### Results

Completed = accepted per second; every offered workflow completed in every repetition, with 0 pending at the drain and 0 5xx.

| Setting (one change from the baseline) | Run 1 | Run 2 | Run 3 | Mean | CV | End-to-end p95 (mean) | vs baseline |
|---|---:|---:|---:|---:|---:|---:|---|
| **Baseline** (defaults) | 41.8 | 41.9 | 44.8 | **42.9** | 4.0% | 18.1 s | – |
| Orchestrator DB pool `DB_MAX_OPEN_CONNS` 10 → 20 | 39.1 | 40.1 | 40.2 | 39.8 | 1.5% | 19.2 s | −7%, borderline |
| Orchestrator outbox poll 100 ms → 20 ms | 40.4 | 45.0 | 43.7 | 43.0 | 5.6% | 18.2 s | 0% |
| Payment `WORKER_CONCURRENCY` 4 → 8 | 44.1 | 45.2 | 42.8 | 44.0 | 2.7% | 17.5 s | +3% |
| Inventory `INVENTORY_CONSUMER_MAX_CONCURRENCY` 4 → 8 | 41.5 | 44.0 | 47.2 | 44.2 | 6.5% | 18.2 s | +3% |

Three repetitions give a spread of 2-7% of the mean, so a change counts only if it is clearly larger. The three small positive and zero differences are inside the spread. The DB-pool run stands apart (about 3 completed/s lower, tight spread), but with three runs it is borderline and is not called a regression.

#### What it means

- The system is latency-bound, not worker-bound, at this load. In a closed loop, completed per second is clients divided by time per workflow: 500 clients at about 11-12 s per workflow gives about 43/s (Little's law).
- Where the extra waiting comes from is not identified. The median workflow takes about 6.9 s with 10 clients (mostly the simulator's pay delay) and about 14.7 s with 500 clients in flight, so roughly 8 s of waiting appears between 10 and 500 clients.
- Following the project's rule, concurrency was not raised further without evidence of a saturated resource.

#### Limits

- Four variables, each changed once and by one step (a factor of 2 or 5). A combination, a different step size, the fixed consumer prefetch of 10, or the broker could still matter.
- Three repetitions: differences under about 7% cannot be separated from noise.
- One load point (500 clients) and one workload (uniform over 50 products); simulated gateway.
- No service profile was taken; RabbitMQ queue depths were not examined in this run.

### Load test 06 — Soak (5 minutes at 700 clients)

**Result:** PASS for a 5-minute soak. This does not meet the planned 30-60 minutes.

#### What is tested

Whether the stack holds a steady rate at about 70% of the 1000-client test point without throughput or latency drifting, the outbox backlog growing, or invariants breaking.

#### How it is run

Case C2 with 700 closed-loop clients, SSE completion, 10 s warm-up, 5 minutes measured, one repetition, from the load-generator host. Nothing was changed on the services, and one orchestrator replica was running. Simulated gateway with its 5 s pay delay.

#### Results

| Check | Result |
|---|---|
| Offered = accepted = completed | **12,736** (0 pending at the drain, 0 5xx, 0 transport errors) |
| Accepted and completed per second | **42.45 / 42.45** (sustained over 5 minutes) |
| Checkout latency p50 / p95 / p99 | 31 ms / 444 ms / 1.18 s |
| End-to-end latency p50 / p95 / p99 | 16.8 s / 21.8 s / 25.2 s |
| In-flight workflows (mean / max) | 721.9 / 700; Little's law matches (0.00% difference) |
| Phase-22 database audit | PASS |
| Observability audit | PASS; 0 stuck sagas, 0 dead outbox rows, 0 dead-queue messages |

##### Drift check, per minute (Prometheus)

| Minute | 0 (ramp) | 1 | 2 | 3 | 4 | 5 | 6 (end) | 7 (drain) |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| Completed per second | 10.8 | 37.9 | 39.6 | 40.4 | 40.7 | 42.0 | 14.5 | 0.0 |
| Accepted per second | 11.1 | 41.6 | 39.5 | 40.4 | 40.6 | 41.7 | 4.1 | 0.0 |
| Saga p95 (s) | 28.4 | 28.8 | 29.0 | 29.0 | 29.0 | 29.0 | 29.0 | – |
| Outbox pending rows (max) | 689 | 32 | 45 | 30 | 19 | 31 | 14 | 0 |

- Throughput does not drift down: minutes 1-5 rise slowly from 37.9 to 42.0 completed/s.
- Latency is flat at about 29 s p95 in the Prometheus histogram. That value sits near the top of the histogram's range, so it is a resolution ceiling; the runner's own end-to-end p95 is 21.8 s.
- The outbox backlog peaks at 689 rows during ramp-up, then stays between 19 and 45 and drains to 0.

State read from the database afterwards: 12,736 sagas COMPLETED, 12,736 orders PAID, 12,736 payments SUCCEEDED, 12,736 reservations CONFIRMED, no negative stock, dead queue 0.

#### Limits

- 5 minutes, not 30-60: memory growth, connection leaks and table growth cannot be judged.
- Database growth was not measured (the runner reports the same database size before and after every run, so that figure does not track the service databases).
- One run, no repeat; simulated gateway; single replica of each service.
- `DatabasePoolSaturated`, `HighLatencyP95` and `ServiceDown` alerts fired (not investigated).

## Part 4: Failure-case tests

### What is being tested

What the system does when something breaks. The goal is not that nothing fails, but that a failure never produces wrong state: no duplicate payment, no duplicate reservation, no lost or stuck saga, no growth of the dead-letter queue, and no oversold stock. A request that was never accepted is allowed to be refused; a request that was accepted must reach a final state.

Three kinds of failure are covered:

1. **Infrastructure faults under load** — a service is killed with `kill -9`, the broker restarts, the database connection drops, the network is slow.
2. **Payment-gateway failures** — the gateway declines, or it times out and leaves the outcome ambiguous.
3. **Overload and scaling limits** — too many clients on one row, or two orchestrator replicas.

### What has been done

All seven fault scenarios were run on the AWS staging stack (one host per service, one broker host, Neon database, gateway simulator) on 1 November 2026. Each scenario offered 200 checkouts at 50 per second from 30 concurrent clients; the fault was injected about 1.5 s in, with 56-57 sagas in flight, then removed. The harness waited for every accepted saga to finish and compared 8 database invariant queries before and after. Gateway failures were run through the correctness suite in `decline` and `timeout` mode. The harness is [`tools/fault-injection/chaos.py`](../tools/fault-injection/chaos.py).

### Verdict

**PASS, 7 of 7 fault scenarios.** Every accepted checkout finished, nothing was duplicated, and the dead queue did not grow. Read the limits before treating this as production evidence.

### Cases

| # | Failure | Accepted | Refused while down | In flight at fault | Drain (s) | Result |
|---|---|---:|---:|---:|---:|:---:|
| 01 | payment-service `kill -9` during gateway calls | 200 | 0 | 57 | 9.5 | PASS |
| 02 | orchestrator `kill -9` mid-burst | 143 | 57 | 57 | 11.9 | PASS |
| 03 | order-service `kill -9` mid-message | 200 | 0 | 56 | 21.0 | PASS |
| 04 | inventory-service `kill -9` mid-message | 143 | 57 | 57 | 11.8 | PASS |
| 05 | RabbitMQ restart | 200 | 0 | 57 | 9.5 | PASS |
| 06 | database connection drop (20 s) | 200 | 0 | 57 | 46.5 | PASS |
| 07 | network delay 200 ms ± 50 ms (40 s) | 200 | 0 | 57 | 16.6 | PASS |
| 08 | two orchestrator replicas | 200 | 0 | – | 11.9 | PASS (correctness); throughput not measured |
| 09 | gateway declines the payment | – | – | – | – | PASS on rerun (3 s window) |
| 10 | gateway timeout, ambiguous outcome | – | – | – | – | PASS |
| 11 | 500 clients on one hot product | 2,033 | 151 (503) | – | – | accounting check failed; no invariant violated |

**Why some checkouts were refused:** when the orchestrator is down it cannot answer; when inventory-service is down the checkout is refused because the stock reservation is a synchronous call. In both cases 57 of 200 requests were refused and the other 143 completed. A refused request was never promised. With the broker down nothing was refused, because accepting a checkout writes only to the Postgres outbox and the broker catches up later.

### Limits of this folder

- Each fault scenario ran once, at one load size, with the gateway simulator in `success` mode. No repeats and no variance figures.
- No ambiguous (UNKNOWN) payment arose during the kill scenarios, so the path "gateway outcome unknown, never retried blindly" is covered by case 10 and by unit tests, not by a cloud fault run.
- Neon autosuspend was not tested; only a connection drop on the payment host. The network delay applied to the payment host only.
- Every service ran as a single replica with one broker and one database: a learning deployment, not a high-availability one.
- Not covered: backup and restore, loss of the broker volume, alerting on stuck sagas, and an operator runbook drill.

### Failure case 01 — payment-service killed (`kill -9`) during gateway calls

**Scenario id:** `payment-kill9-mid-flight`  
**Result:** PASS

#### What is tested

The payment service dies while payment commands and gateway calls are in flight. Recovery relies on unacknowledged RabbitMQ messages being redelivered, idempotent payment creation, and reconciliation of payments whose gateway outcome is unknown.

#### How the fault is injected

`docker kill` on the payment-service container, then restart after the fault window.

#### Load during the test

- 200 checkouts offered at 50 per second from 30 concurrent clients, against the AWS staging stack and the gateway simulator in `success` mode.
- The fault starts about 1.5 s into the burst; 57 sagas were in flight when it started.
- The harness then waits for every accepted saga to reach a final state and re-checks the database invariants.

#### Expected behaviour

Checkouts keep being accepted because the orchestrator, inventory and order services are up; their payment steps wait and are resent after the restart.

#### Result

| Measure | Value |
|---|---:|
| Checkouts accepted (HTTP 202) | 200 |
| Refused while the service was down | 0 |
| Sagas in flight when the fault started | 57 |
| Final saga status | 200 COMPLETED |
| Unfinished sagas | 0 |
| Unfinished with an unknown payment | 0 |
| Payments beyond one per order | 0 |
| Reservations beyond one per order | 0 |
| Accepted sagas without an order | 0 |
| Dead-queue growth | 0 |
| Fault duration (s) | 4.2 |
| Time for the last saga to finish after the burst (s) | 9.5 |
| Scenario duration (s) | 33.4 |

#### Database invariants (violations found, before / after the fault)

| Invariant | Before | After |
|---|---:|---:|
| inventory counters within bounds | 0 | 0 |
| one reservation per order | 0 | 0 |
| one payment per order | 0 | 0 |
| one order row per saga | 0 | 0 |
| paid orders have a succeeded payment | 0 | 0 |
| paid orders have a confirmed reservation | 0 | 0 |
| cancelled orders have a released reservation | 0 | 0 |
| unknown payments keep the order pending | 0 | 0 |

#### Limits of this test

- Run once; no repeats or variance figure.
- Only checkouts the orchestrator accepted are judged. A refused request was never promised.
- The gateway is the simulator; no ambiguous (UNKNOWN) payment arose in this run, so the "outcome unknown, never retried blindly" path was exercised by tests, not by this scenario.
- Single replica of every service, one broker, one database.

### Failure case 02 — orchestrator-service killed (`kill -9`) mid-burst

**Scenario id:** `orchestrator-kill9-mid-burst`  
**Result:** PASS

#### What is tested

The service that owns the saga state machine and the outbox dies while sagas are running. Saga state and outbox rows live in PostgreSQL, so the restarted process resumes from the database.

#### How the fault is injected

`docker kill` on the orchestrator container, then restart.

#### Load during the test

- 200 checkouts offered at 50 per second from 30 concurrent clients, against the AWS staging stack and the gateway simulator in `success` mode.
- The fault starts about 1.5 s into the burst; 57 sagas were in flight when it started.
- The harness then waits for every accepted saga to reach a final state and re-checks the database invariants.

#### Expected behaviour

While the orchestrator is down it cannot answer, so checkout requests during the fault are refused (never accepted). Everything accepted must still finish.

#### Result

| Measure | Value |
|---|---:|
| Checkouts accepted (HTTP 202) | 143 |
| Refused while the service was down | 57 |
| Sagas in flight when the fault started | 57 |
| Final saga status | 143 COMPLETED |
| Unfinished sagas | 0 |
| Unfinished with an unknown payment | 0 |
| Payments beyond one per order | 0 |
| Reservations beyond one per order | 0 |
| Accepted sagas without an order | 0 |
| Dead-queue growth | 0 |
| Fault duration (s) | 4.1 |
| Time for the last saga to finish after the burst (s) | 11.9 |
| Scenario duration (s) | 37.0 |

#### Database invariants (violations found, before / after the fault)

| Invariant | Before | After |
|---|---:|---:|
| inventory counters within bounds | 0 | 0 |
| one reservation per order | 0 | 0 |
| one payment per order | 0 | 0 |
| one order row per saga | 0 | 0 |
| paid orders have a succeeded payment | 0 | 0 |
| paid orders have a confirmed reservation | 0 | 0 |
| cancelled orders have a released reservation | 0 | 0 |
| unknown payments keep the order pending | 0 | 0 |

#### Limits of this test

- Run once; no repeats or variance figure.
- Only checkouts the orchestrator accepted are judged. A refused request was never promised.
- The gateway is the simulator; no ambiguous (UNKNOWN) payment arose in this run, so the "outcome unknown, never retried blindly" path was exercised by tests, not by this scenario.
- Single replica of every service, one broker, one database.

### Failure case 03 — order-service killed (`kill -9`) mid-message

**Scenario id:** `order-service-kill9-mid-message`  
**Result:** PASS

#### What is tested

The order service dies while consuming `order.create` and order-state commands. Commands are acknowledged only after their effect is committed, so unacknowledged messages are redelivered and consumers are idempotent.

#### How the fault is injected

`docker kill` on the order-service container, then restart.

#### Load during the test

- 200 checkouts offered at 50 per second from 30 concurrent clients, against the AWS staging stack and the gateway simulator in `success` mode.
- The fault starts about 1.5 s into the burst; 56 sagas were in flight when it started.
- The harness then waits for every accepted saga to reach a final state and re-checks the database invariants.

#### Expected behaviour

Checkouts are still accepted (the order step is asynchronous); in this run none were refused.

#### Result

| Measure | Value |
|---|---:|
| Checkouts accepted (HTTP 202) | 200 |
| Refused while the service was down | 0 |
| Sagas in flight when the fault started | 56 |
| Final saga status | 200 COMPLETED |
| Unfinished sagas | 0 |
| Unfinished with an unknown payment | 0 |
| Payments beyond one per order | 0 |
| Reservations beyond one per order | 0 |
| Accepted sagas without an order | 0 |
| Dead-queue growth | 0 |
| Fault duration (s) | 4.2 |
| Time for the last saga to finish after the burst (s) | 21.0 |
| Scenario duration (s) | 45.2 |

#### Database invariants (violations found, before / after the fault)

| Invariant | Before | After |
|---|---:|---:|
| inventory counters within bounds | 0 | 0 |
| one reservation per order | 0 | 0 |
| one payment per order | 0 | 0 |
| one order row per saga | 0 | 0 |
| paid orders have a succeeded payment | 0 | 0 |
| paid orders have a confirmed reservation | 0 | 0 |
| cancelled orders have a released reservation | 0 | 0 |
| unknown payments keep the order pending | 0 | 0 |

#### Limits of this test

- Run once; no repeats or variance figure.
- Only checkouts the orchestrator accepted are judged. A refused request was never promised.
- The gateway is the simulator; no ambiguous (UNKNOWN) payment arose in this run, so the "outcome unknown, never retried blindly" path was exercised by tests, not by this scenario.
- Single replica of every service, one broker, one database.

### Failure case 04 — inventory-service killed (`kill -9`) mid-message

**Scenario id:** `inventory-kill9-mid-message`  
**Result:** PASS

#### What is tested

The inventory service dies. Stock reservation is a synchronous HTTP call from the orchestrator, so checkouts are refused while it is down; confirm and release commands are redelivered after the restart.

#### How the fault is injected

`docker kill` on the inventory-service container, then restart.

#### Load during the test

- 200 checkouts offered at 50 per second from 30 concurrent clients, against the AWS staging stack and the gateway simulator in `success` mode.
- The fault starts about 1.5 s into the burst; 57 sagas were in flight when it started.
- The harness then waits for every accepted saga to reach a final state and re-checks the database invariants.

#### Expected behaviour

Requests during the fault are refused with an error and never promised; accepted checkouts must finish.

#### Result

| Measure | Value |
|---|---:|
| Checkouts accepted (HTTP 202) | 143 |
| Refused while the service was down | 57 |
| Sagas in flight when the fault started | 57 |
| Final saga status | 143 COMPLETED |
| Unfinished sagas | 0 |
| Unfinished with an unknown payment | 0 |
| Payments beyond one per order | 0 |
| Reservations beyond one per order | 0 |
| Accepted sagas without an order | 0 |
| Dead-queue growth | 0 |
| Fault duration (s) | 4.2 |
| Time for the last saga to finish after the burst (s) | 11.8 |
| Scenario duration (s) | 36.9 |

#### Database invariants (violations found, before / after the fault)

| Invariant | Before | After |
|---|---:|---:|
| inventory counters within bounds | 0 | 0 |
| one reservation per order | 0 | 0 |
| one payment per order | 0 | 0 |
| one order row per saga | 0 | 0 |
| paid orders have a succeeded payment | 0 | 0 |
| paid orders have a confirmed reservation | 0 | 0 |
| cancelled orders have a released reservation | 0 | 0 |
| unknown payments keep the order pending | 0 | 0 |

#### Limits of this test

- Run once; no repeats or variance figure.
- Only checkouts the orchestrator accepted are judged. A refused request was never promised.
- The gateway is the simulator; no ambiguous (UNKNOWN) payment arose in this run, so the "outcome unknown, never retried blindly" path was exercised by tests, not by this scenario.
- Single replica of every service, one broker, one database.

### Failure case 05 — RabbitMQ restart mid-burst

**Scenario id:** `broker-restart-mid-burst`  
**Result:** PASS

#### What is tested

The message broker disappears and returns. Accepting a checkout writes only to the PostgreSQL outbox, so nothing is refused; the outbox relay retries publishing until the broker is back.

#### How the fault is injected

`docker restart` on the RabbitMQ container.

#### Load during the test

- 200 checkouts offered at 50 per second from 30 concurrent clients, against the AWS staging stack and the gateway simulator in `success` mode.
- The fault starts about 1.5 s into the burst; 57 sagas were in flight when it started.
- The harness then waits for every accepted saga to reach a final state and re-checks the database invariants.

#### Expected behaviour

No checkout is refused. The outbox logged publish failures during the outage, and no row was marked dead because a broker outage is not counted as a permanent failure.

#### Result

| Measure | Value |
|---|---:|
| Checkouts accepted (HTTP 202) | 200 |
| Refused while the service was down | 0 |
| Sagas in flight when the fault started | 57 |
| Final saga status | 200 COMPLETED |
| Unfinished sagas | 0 |
| Unfinished with an unknown payment | 0 |
| Payments beyond one per order | 0 |
| Reservations beyond one per order | 0 |
| Accepted sagas without an order | 0 |
| Dead-queue growth | 0 |
| Fault duration (s) | 4.2 |
| Time for the last saga to finish after the burst (s) | 9.5 |
| Scenario duration (s) | 33.3 |

#### Database invariants (violations found, before / after the fault)

| Invariant | Before | After |
|---|---:|---:|
| inventory counters within bounds | 0 | 0 |
| one reservation per order | 0 | 0 |
| one payment per order | 0 | 0 |
| one order row per saga | 0 | 0 |
| paid orders have a succeeded payment | 0 | 0 |
| paid orders have a confirmed reservation | 0 | 0 |
| cancelled orders have a released reservation | 0 | 0 |
| unknown payments keep the order pending | 0 | 0 |

#### Limits of this test

- Run once; no repeats or variance figure.
- Only checkouts the orchestrator accepted are judged. A refused request was never promised.
- The gateway is the simulator; no ambiguous (UNKNOWN) payment arose in this run, so the "outcome unknown, never retried blindly" path was exercised by tests, not by this scenario.
- Single replica of every service, one broker, one database.

### Failure case 06 — Database connection drop (20 s)

**Scenario id:** `db-connection-drop`  
**Result:** PASS

#### What is tested

The payment service loses its database for 20 s while serving traffic. Pools reconnect; commands that failed are redelivered or retried.

#### How the fault is injected

On the payment host, reset TCP connections to port 5432 with `iptables` for 20 s.

#### Load during the test

- 200 checkouts offered at 50 per second from 30 concurrent clients, against the AWS staging stack and the gateway simulator in `success` mode.
- The fault starts about 1.5 s into the burst; 57 sagas were in flight when it started.
- The harness then waits for every accepted saga to reach a final state and re-checks the database invariants.

#### Expected behaviour

Other services stay up, so no checkout is refused.

#### Result

| Measure | Value |
|---|---:|
| Checkouts accepted (HTTP 202) | 200 |
| Refused while the service was down | 0 |
| Sagas in flight when the fault started | 57 |
| Final saga status | 200 COMPLETED |
| Unfinished sagas | 0 |
| Unfinished with an unknown payment | 0 |
| Payments beyond one per order | 0 |
| Reservations beyond one per order | 0 |
| Accepted sagas without an order | 0 |
| Dead-queue growth | 0 |
| Fault duration (s) | 20.5 |
| Time for the last saga to finish after the burst (s) | 46.5 |
| Scenario duration (s) | 77.4 |

#### Database invariants (violations found, before / after the fault)

| Invariant | Before | After |
|---|---:|---:|
| inventory counters within bounds | 0 | 0 |
| one reservation per order | 0 | 0 |
| one payment per order | 0 | 0 |
| one order row per saga | 0 | 0 |
| paid orders have a succeeded payment | 0 | 0 |
| paid orders have a confirmed reservation | 0 | 0 |
| cancelled orders have a released reservation | 0 | 0 |
| unknown payments keep the order pending | 0 | 0 |

#### Limits of this test

- Run once; no repeats or variance figure.
- Only checkouts the orchestrator accepted are judged. A refused request was never promised.
- The gateway is the simulator; no ambiguous (UNKNOWN) payment arose in this run, so the "outcome unknown, never retried blindly" path was exercised by tests, not by this scenario.
- Single replica of every service, one broker, one database.

### Failure case 07 — Network delay (200 ms ± 50 ms, 40 s)

**Scenario id:** `network-delay`  
**Result:** PASS

#### What is tested

Slow network between the payment service and its dependencies (database, broker, gateway). Timeouts must not turn into duplicate charges or lost sagas.

#### How the fault is injected

On the payment host, add 200 ms ± 50 ms delay to all outgoing traffic with `tc` for 40 s.

#### Load during the test

- 200 checkouts offered at 50 per second from 30 concurrent clients, against the AWS staging stack and the gateway simulator in `success` mode.
- The fault starts about 1.5 s into the burst; 57 sagas were in flight when it started.
- The harness then waits for every accepted saga to reach a final state and re-checks the database invariants.

#### Expected behaviour

Other hosts were not delayed, so no checkout is refused.

#### Result

| Measure | Value |
|---|---:|
| Checkouts accepted (HTTP 202) | 200 |
| Refused while the service was down | 0 |
| Sagas in flight when the fault started | 57 |
| Final saga status | 200 COMPLETED |
| Unfinished sagas | 0 |
| Unfinished with an unknown payment | 0 |
| Payments beyond one per order | 0 |
| Reservations beyond one per order | 0 |
| Accepted sagas without an order | 0 |
| Dead-queue growth | 0 |
| Fault duration (s) | 43.0 |
| Time for the last saga to finish after the burst (s) | 16.6 |
| Scenario duration (s) | 70.3 |

#### Database invariants (violations found, before / after the fault)

| Invariant | Before | After |
|---|---:|---:|
| inventory counters within bounds | 0 | 0 |
| one reservation per order | 0 | 0 |
| one payment per order | 0 | 0 |
| one order row per saga | 0 | 0 |
| paid orders have a succeeded payment | 0 | 0 |
| paid orders have a confirmed reservation | 0 | 0 |
| cancelled orders have a released reservation | 0 | 0 |
| unknown payments keep the order pending | 0 | 0 |

#### Limits of this test

- Run once; no repeats or variance figure.
- Only checkouts the orchestrator accepted are judged. A refused request was never promised.
- The gateway is the simulator; no ambiguous (UNKNOWN) payment arose in this run, so the "outcome unknown, never retried blindly" path was exercised by tests, not by this scenario.
- Single replica of every service, one broker, one database.

### Failure case 08 — Two orchestrator replicas

**Result:** PASS for correctness. Throughput with two replicas was **not measured**.

#### What is tested

Two orchestrator processes (same image, environment, database and broker) run side by side and compete for the same RabbitMQ queues, outbox rows and sweeper work. Work distribution relies on `FOR UPDATE SKIP LOCKED` claims and idempotent consumers, so the second replica must share the work without causing duplicate effects.

#### How it is run

A second orchestrator container was started on the orchestrator host (no published port) and 200 checkouts were sent at 50 per second from 30 clients to the first replica. After the burst the harness waited for every saga to finish and checked the invariants; the second replica was then removed.

#### Result

| Measure | Value |
|---|---:|
| Checkouts accepted | 200 |
| Final saga status | 200 COMPLETED |
| Unfinished sagas | 0 |
| Payments beyond one per order | 0 |
| Reservations beyond one per order | 0 |
| Accepted sagas without an order | 0 |
| Dead-queue growth | 0 |
| Time for the last saga to finish after the burst (s) | 11.9 |
| Saga steps completed by the second replica | 400 of 800 (200 sagas × 4 steps) |

The first replica's event count in the JSON (80,060) is its lifetime total since it started and is not comparable.

#### Finding: SSE completion is replica-local

A first attempt to compare throughput (500 clients, SSE completion, two replicas) was stopped and is not part of the results. Its first two repetitions completed 30.3/s and 5.0/s, far below the single-replica baseline of about 43/s, with no warnings or errors in either replica. The cause is in the code: `orchestrator-service/pkg/statusstream/hub.go` is an in-process hub for a single replica, so a completion handled by one replica never reaches clients waiting on the other until their fallback. The database and the sagas were correct throughout; only the push notification is replica-local. This was confirmed from the code and wiring, not isolated by an experiment.

#### Limits

- Throughput with two replicas was not measured.
- One burst of 200 checkouts; no fault injected while two replicas ran.
- Both replicas shared one host (two vCPUs), so this says nothing about scaling across hosts.
- All other services, the broker and the database remained single instances.

### Failure case 09 — Payment declined by the gateway

**Result:** PASS in the decline-mode rerun (3 s payment window, 1 s reconciliation). The first decline-mode run, with the default payment window, did not pass (see below).

#### What is tested

The gateway simulator is set to `decline`: the customer's payment fails. A confirmed failure must be treated differently from an ambiguous one: the saga compensates (the order is cancelled, the reserved stock is released, and no stock is consumed), and the payment is recorded as failed.

#### How the fault is injected

`GATEWAY_MODE=decline` on the gateway simulator, running the correctness suite phases 09 and 14 against the AWS staging stack.

#### Phase 14 — Declined payment end to end

**Test code:** [`phase14_decline_e2e.go`](../tools/correctness/phase14_decline_e2e.go)


| Assertion | Result | Detail |
|---|---|---|
| saga finishes its inventory step | PASS | saga status COMPENSATED |
| decline cancels order | PASS | HTTP status=OK |
| payment is failed | PASS | terminal payment |
| reservation is released | PASS | terminal reservation |
| decline consumes no stock | PASS | inventory returns to start |

#### Phase 09 — Payment gateway outcome (decline mode)

**Test code:** [`phase09_payment_outcome.go`](../tools/correctness/phase09_payment_outcome.go)


| Assertion | Result | Detail |
|---|---|---|
| payment attempt is created | PASS | HTTP status=Created |
| gateway result matches mode | PASS | expected FAILED, got FAILED |

#### Caveat: payment window

With the default payment window, a `payment.failed` webhook does not cancel the order immediately, because the customer may retry inside the window. In the first decline-mode run the payments therefore stayed in `AWAITING_PAYMENT`, phases 09 and 14 failed, and the worker backlog did not drain in time for phases 21 and 22. The rerun used a 3 s window and 1 s reconciliation interval (the payment timings were made overridable for this) and passed. The decline result therefore holds for those short timings; the default window was not tested in decline mode.

### Failure case 10 — Ambiguous payment (gateway timeout)

**Result:** PASS

#### What is tested

The gateway simulator is set to `timeout`: a payment request neither succeeds nor fails visibly. This is an *ambiguous* outcome, not a failure. The system must record the payment as `UNKNOWN`, leave the order and reservation pending/held, and never blindly retry a possibly-successful charge. A later lookup or webhook decides the outcome; after the payment window plus a grace period an unresolved attempt is failed as unconfirmed, and a capture seen later is recorded for refund rather than accepted.

#### How the fault is injected

`GATEWAY_MODE=timeout` on the gateway simulator, running the correctness suite phases 09 and 15 against the AWS staging stack.

#### Phase 15 — Ambiguous payment timeout

**Test code:** [`phase15_timeout_e2e.go`](../tools/correctness/phase15_timeout_e2e.go)


| Assertion | Result | Detail |
|---|---|---|
| timeout keeps order pending | PASS | HTTP status=OK |
| payment is unknown | PASS | ambiguous terminal payment |
| reservation remains reserved | PASS | do not release uncertain stock |
| stock remains held | PASS | reserved +2 |

#### Phase 09 — Payment gateway outcome (timeout mode)

**Test code:** [`phase09_payment_outcome.go`](../tools/correctness/phase09_payment_outcome.go)


| Assertion | Result | Detail |
|---|---|---|
| payment attempt is created | PASS | HTTP status=Created |
| gateway result matches mode | PASS | expected UNKNOWN, got UNKNOWN |

#### Related unit and integration tests

- [`unconfirmed_integration_test.go`](../payment-service/pkg/services/unconfirmed_integration_test.go): an unreachable gateway past window + grace fails the attempt; inside the grace period the service keeps asking and never guesses.
- [`refund_after_payment_integration_test.go`](../payment-service/pkg/services/refund_after_payment_integration_test.go): refund after a payment when a later saga step cannot finish.

#### Limits

- The fault-injection scenarios (kills, restarts) did not produce an `UNKNOWN` payment in the cloud; the ambiguous path is exercised here by the simulator's timeout mode and by the tests above.
- Timings of the timeout run were the deployed defaults and were not recorded.

### Failure case 11 — Overload: 500 clients on one hot product

**Result:** the run **did not pass its own accounting check**; no correctness invariant was violated.

#### What is tested

What happens when far more clients than the stack handles hit a single product row at once. The expected behaviour under overload is to refuse some requests cleanly (never to oversell, lose an accepted checkout, or leave sagas pending).

This run was not designed as a failure test. It ran as the first of two cases in one command (`LOAD_CASE=C5` with `LOAD_CLIENTS=500`); the setting meant for the sell-out case also applied to the throughput case, whose normal setting is 50 clients. It is recorded here because it shows how the system behaves past its limit on a hot row. It must not be quoted as a capacity figure.

#### How it is run

500 closed-loop clients buy one high-stock product for 60 s after a 10 s warm-up, against the AWS staging stack and the gateway simulator in `success` mode.

#### Result

| Measure | Value |
|---|---:|
| Checkouts offered | 2,184 |
| Accepted (HTTP 202) | 2,033 |
| Refused with HTTP 503 | 151 |
| Completed | 2,033 (2,033 paid, 0 cancelled, 0 pending at the drain) |
| Completed rate | 33.88/s |
| Checkout latency p50 / p95 / p99 | 150 ms / 3.08 s / 3.21 s |
| End-to-end p95 | 21.6 s |
| Lock waiters, deadlocks | 0 |
| Failed check | "one unique completed checkout attempt per offered workflow": 151 violations, exactly the refused requests |

#### What the metrics suggest (inference, not a traced cause)

- **inventory-service:** 151 errors, up to 468 requests in flight, handler p95 of 3.0 s. A p95 of almost exactly 3 s suggests a timeout, but the limit was not looked up in the code.
- **orchestrator-service:** 151 errors, up to 500 requests in flight and 397,580 database pool waits on a pool of 10 connections.
- Together they point at the inventory step on a single row and database pool waits. About 7% of checkouts were refused with 503.

#### What it shows

- Nothing was oversold, and every accepted checkout finished. Overload surfaced as refused requests, not as incorrect state.
- The cause of the 503s was not traced, and whether the `ServiceDown` alert that fired belongs to the same overload was not checked.

## Part 5: Penetration tests

### What is being tested

Whether an attacker, rather than a well-behaved client, can break the system's security properties: forge a payment or webhook, bypass price or stock checks, replay or tamper with requests, read or change another customer's order, or exhaust the service.

### What has been done

**No penetration test has been performed on this project, and no result in this folder is a security claim.** The folder exists so that the gap is visible and not hidden.

Authentication and authorization are **out of scope by decision** (the API has no login), so tests for authentication bypass, session handling and access control between users do not apply yet.

### Security-relevant controls that do exist (tested as correctness or unit tests, not as attacks)

These are ordinary functional tests. They show that specific defences were built and are exercised, not that the system resists a determined attacker.

| Control | Tested by |
|---|---|
| Client cannot choose prices; totals come from the server | Correctness phase 02 |
| Webhook signature (HMAC-SHA256 of the raw body) is verified; an invalid signature changes nothing | `TestVerifySignature`, `TestInvalidSignatureChangesNothing` in [`handler_test.go`](../payment-service/pkg/webhooks/handler_test.go) |
| The payment service refuses to start with Razorpay and no webhook secret unless webhooks are disabled by name | [`webhook_config_test.go`](../payment-service/pkg/config/webhook_config_test.go) |
| Idempotency keys are length-limited and charset-limited; checkout line and quantity limits are enforced with clean error messages | [`checkout_request_test.go`](../orchestrator-service/pkg/services/checkout/checkout_request_test.go) |
| Oversized request bodies are rejected | `TestCreateCheckoutRejectsOversizedBody` in [`checkout_controller_test.go`](../orchestrator-service/pkg/controllers/checkout_controller_test.go) |
| Replay of the same checkout creates no second order, reservation or payment | Correctness phases 07, 10, 19 |
| Error responses do not leak internal text and map to stable status codes | Correctness phase 20 |
| Secrets (`.env`, keys, Terraform state) are not tracked in git | `.gitignore`; checked by listing tracked files before release |

### Known gaps (not tested, not built)

- No authentication, authorization, TLS termination or rate limiting (recorded as parked in [`docs/backlog.md`](../docs/backlog.md)).
- No dependency vulnerability scan, container image scan or static security analysis has been run.
- No fuzzing of request bodies, headers or webhook payloads beyond the functional tests above.
- No test of webhook replay with a valid old signature, or of timing attacks on signature comparison.
- No test against real Razorpay webhooks over public HTTPS.

### What a first penetration test would cover (plan, not done)

1. Webhook forgery and replay against `payment-service`: missing, malformed and stale signatures; duplicate event ids.
2. Input abuse on `POST /v1/checkouts`: oversized and malformed JSON, negative and huge quantities, duplicate product lines, key injection, unicode and control characters in the idempotency key.
3. Enumeration and access: guessing order ids through `GET /v1/checkouts/{id}` (relevant once authentication exists).
4. Resource exhaustion: many concurrent connections and slow clients against the SSE stream.
5. Internal services exposed on the network: unauthenticated inventory, order and payment APIs reachable from outside the intended path.

Findings from any such run would be recorded in this folder with the same rules as the others: exact commands, raw output, and no claim beyond what was observed.
