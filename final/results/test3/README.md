# Test 3 — fault tests on the AWS staging stack (final result)

**Date:** 1 Nov 2026, 18:01–18:34 UTC · **Environment:** `staging` (ap-southeast-2; four service hosts, one broker host, Neon database; gateway simulator) · **Run id:** `cloud-test3-20261101T180146Z`

**Data in this folder:** [`chaos-test.json`](chaos-test.json) (every scenario with all counters and fault times), [`audit.json`](audit.json) (observability audit of the run), [`run.json`](run.json).
**Full evidence** (Prometheus export 300 KB, logs 3.4 MB, console logs for each scenario): `../cloud-test3-20261101T180146Z/`.

## Verdict

**PASS, 7 of 7 scenarios.** Under 30 concurrent clients, killing each service in turn, restarting the broker, dropping the payment host's database connections and delaying its network each left the system correct: every accepted checkout finished, nothing was duplicated, and the dead queue did not grow. Read the limits below before treating this as production evidence.

## What was done

Each scenario offered a burst of **200 checkouts** from **30 concurrent clients** (paced at 50 per second) to the orchestrator. About 1.5 seconds in, with **56–57 sagas in flight**, one fault was injected on the staging stack, then removed. The harness waited for every accepted saga to finish and checked the invariants. Only checkouts the orchestrator accepted (HTTP 202) are judged.

| Step | Fault | How |
|---|---|---|
| 3a | payment-service killed | `docker kill`, then restarted |
| 3b | orchestrator, order-service, inventory-service killed, one at a time | `docker kill`, then restarted |
| 3c | RabbitMQ restarted | `docker restart` |
| 3d | database connection drop | TCP reset on port 5432 on the payment host for 20 s |
| 3e | network delay | 200 ms ± 50 ms on all traffic leaving the payment host for 40 s |

## Results

| Scenario | Result | Accepted | Refused while down | In flight at fault | Final saga status | Unfinished | Duplicates | Dead-queue growth | Fault (s) | Drain (s) |
|---|:---:|---:|---:|---:|---|---:|:---:|---:|---:|---:|
| 3a payment-service kill -9 | PASS | 200 | 0 | 57 | 200 COMPLETED | 0 | none | 0 | 4.2 | 9.5 |
| 3b orchestrator kill -9 | PASS | 143 | 57 | 57 | 143 COMPLETED | 0 | none | 0 | 4.1 | 11.9 |
| 3b order-service kill -9 | PASS | 200 | 0 | 56 | 200 COMPLETED | 0 | none | 0 | 4.2 | 21.0 |
| 3b inventory-service kill -9 | PASS | 143 | 57 | 57 | 143 COMPLETED | 0 | none | 0 | 4.2 | 11.8 |
| 3c RabbitMQ restart | PASS | 200 | 0 | 57 | 200 COMPLETED | 0 | none | 0 | 4.2 | 9.5 |
| 3d database connection drop (20 s) | PASS | 200 | 0 | 57 | 200 COMPLETED | 0 | none | 0 | 20.5 | 46.5 |
| 3e network delay (200 ms, 40 s) | PASS | 200 | 0 | 57 | 200 COMPLETED | 0 | none | 0 | 43.0 | 16.6 |

"Duplicates" means no payment beyond one per order, no reservation beyond one per order, and no COMPLETED saga without an order. Before and after every scenario all 8 invariant queries returned 0 violations (stock bounds, one reservation, payment and order per saga, PAID orders have a succeeded payment and a confirmed reservation, cancelled orders have a released reservation, unknown payments keep the order pending).

**Why some checkouts were refused:** when the orchestrator is down it cannot answer. When inventory-service is down the checkout is refused too, because the stock reservation is a synchronous call. In both runs 57 of 200 were refused and the other 143 completed. A refused request was never promised. With the broker down (3c) nothing was refused, because accepting a checkout writes only to the Postgres outbox, and the broker catches up later.

## What the observability data shows (`audit.json`)

- **Audit verdict PASS, no violations.** At the end of the run: 0 stuck sagas, 0 dead outbox rows, 0 unknown or unconfirmed payments, 0 incomplete refunds, 0 stock violations, 0 dead payment jobs, 0 parked webhooks.
- **Alerts that fired:** `ServiceDown`, `ReadinessFailed`, `OutboxBacklogAge` (all expected during the kills and the broker restart) and `DatabasePoolSaturated` (not explained; it also fired in test 2).
- **`outbox_publish_failed`:** 83 events in the logs, from the outbox retrying while RabbitMQ was down. No row was marked dead.
- **Dead-queue depth peaked at 2** at the start of the run window. These were the leftover messages from test 2, purged before the first scenario. The harness measured growth during each scenario, which was 0.
- **HTTP 5xx:** 405 in total across the run, consistent with the refused requests while services were down.

## Limits — what this does not show

1. **No ambiguous payment was produced.** The runs ended with 0 UNKNOWN payments, even after killing payment-service with 57 sagas in flight. The path "gateway outcome unknown, never retried blindly" was not exercised here.
2. **Each scenario ran once,** at one load size (200 checkouts, 30 clients), with the gateway in `success` mode against the simulator. There are no repeats and no variance figures.
3. **Neon autosuspend was not tested,** only a connection drop on one host for 20 s, and only the payment host got the network delay. The other services and the broker did not.
4. **Single replica of every service,** one broker, one database. This is a learning deployment, not a high-availability one.
5. **The full phase-22 audit** (on the load-generator host) was not run after this test. The 8 invariant queries in the harness ran after each scenario, and `audit.json` comes from the observability stack.
6. **Missing run record:** the git commit and image tag are not recorded (the report's `Git:` field is empty because the load generator has no checkout), and the payment window and reconciliation settings are the deployed defaults (5 min window + 5 min grace), not recorded for this run. The report's "Run window" shows only the last invocation (3e); the true window is in `run.json`.
7. **Grafana fault markers** were not checked.

## Not covered by this test

Backup and restore, loss of the broker volume, alerting on stuck sagas, the replica test, the soak test, and the operator runbook drill. See `final/README.md`.
