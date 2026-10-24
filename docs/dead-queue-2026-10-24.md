# `distri.commands.dead` investigation (2026-10-24)

State when inspected: 379 messages, all ready, no consumers. Read non-destructively through the management
API (`ack_requeue_true`; every message was put back, count still 379; the `redelivered` flag is now true on them).
Nothing was purged, replayed or republished.

## Groups

| # | Count | Routing key / origin queue | Payload | Time (UTC, 2026-10-23) | Reason |
|---|---|---|---|---|---|
| A | 247 | `reply.payment` ← `distri.orchestrator.replies` | `TAKE_PAYMENT` / `Failed` | 19:57 – 20:09 | rejected (permanent): *load order reply saga: record not found* |
| B | 83 | same | `TAKE_PAYMENT` / `Succeeded` | 14:54 – 20:09 | same |
| C | 48 | same | `TAKE_PAYMENT` / `Ambiguous` | 19:59 – 20:09 | same |
| D | 1 | `order.create` ← `distri.order.commands` | order create command | 15:15:46 | 5 delivery attempts exhausted (tier 5s→30s→2m→2m→dead) |

Every message has a unique order id (379 distinct). The orchestrator log shows 376 `permanent RabbitMQ delivery failure`
lines of the same reason in 17:00 (2), 19:00 (27) and 20:00 (347) hours; the other three are older than its container start.

## Causes

**Groups A–C (378 messages) — confirmed mechanism, hypothesis for the exact test runs.**
- Confirmed: none of these order ids exists in `orchestrator.sagas` (0 of 378), `orders.orders` or `inventory.reservations`;
  3 still have a `payment_attempts` row. The orchestrator therefore had no saga to apply the reply to and rejected it as permanent —
  the intended behaviour for a reply about an unknown saga.
- Confirmed: the payment-service integration tests (`webhook_processor_integration_test.go`, `payment_flow_test.go`) open
  `DATABASE_TEST_URL` and insert `payments.payment_attempts` + `payments.outbox` rows for random order ids, then delete them in
  `t.Cleanup`. The test database used was the stack's own `distri` database, where the running payment-service's outbox relay
  publishes every unpublished outbox row to RabbitMQ within a second. The tests' outcome mix (late captures → `Failed`, successes →
  `Succeeded`, timeouts → `Ambiguous`) and the 12-trial race tests run in four rounds at ~20:00–20:09 match the group sizes.
- Hypothesis (not provable from what remains): which individual test run produced which message; the cleanup deleted the rows,
  so only the messages are left.
- Not the cause: direct production traffic, simulator restarts, invalid payloads (all 379 are valid JSON with message ids),
  duplicate handling (each order id appears once).

**Group D (1 message) — confirmed.** The OR8 fault scenario of 2026-10-23 (`r3.log`): the orders table was deliberately made
unavailable, `order.create` retried through 5 s → 30 s → 2 m → 2 m and was dead-lettered at 15:20:21 (log line
*RabbitMQ delivery exhausted its attempts*). The saga's own sweeper then resent the command (CREATE_ORDER attempts=2) and the
saga reached COMPLETED. Nothing is outstanding.

## Fix

The defect was in the test setup, not in the services (the orchestrator and relay behaved as designed). The payment-service test
helper now refuses to run against a database whose name does not end in `_test` (override `ALLOW_SHARED_TEST_DB=1`
only if no relay runs against it), so test outbox rows can no longer be published. A dedicated `distri_test` database exists in the
local Postgres container (`payments` schema migrated with `cmd/migrate`); tests run with
`DATABASE_TEST_URL='postgres://distri:distri@localhost:5432/distri_test?sslmode=disable&search_path=payments'`.
Verified: the same tests against `distri` skip with that message, and the full suite passes against `distri_test`.
Not yet done: the order-service and orchestrator integration tests also write outbox rows (`order_create_integration_test.go`,
`store_integration_test.go`, `checkout_service_integration_test.go`, `order_reply_integration_test.go`) and have no such guard;
they did not contribute to this queue but carry the same risk.

## Disposition (nothing is replayed)

| Group | Disposition | Why |
|---|---|---|
| A, B, C | Keep for now; safe to discard after you approve | Replaying is pointless and harmless: the sagas do not exist, so the orchestrator would reject them again. They hold no side effects to recover. |
| D | Keep for now; safe to discard after you approve | Already resolved by the saga resend; replaying `order.create` would be an idempotent no-op at best. |

A later purge is a separate decision for you; it was not done here. Dead-queue count after this work is recorded in `memory.md`.

Also noticed (not part of the queue): `payments.jobs` has 2 `dead` rows (`process_payment`, 2026-10-23, from the OR8 fault runs — one has
no attempt row, one is `UNKNOWN`). They are fault-test leftovers.
