# Backlog: bugs, edge cases, features (found 2026-10-24)

Scope: correctness and behaviour only. CI/deployment and load-test results are deliberately parked.
Status key: **confirmed** = reproduced by a probe on the local stack; **code-read** = found by reading code, not reproduced; **design** = needs a design decision before any code.
Most items below were fixed afterwards; see the status table.

## Status after the fix pass (2026-10-25)
"Verified" means a test that ran (unit or Postgres-backed integration tests on `distri_test`, with `-race`) and, where marked **stack**, a check against the running local stack with the simulator (checks A, B and C: 39 of 39 passed). Nothing was run against real Razorpay.

| ID | Status | What was done / why not |
|---|---|---|
| BUG-1 | **fixed** | key capped at 255 characters and limited to letters, digits and `. _ : -`. Verified (unit, stack) |
| BUG-2 | **fixed** | the orchestrator checks 1 to 20 lines and quantity 1 to 100 itself, and inventory failures reach the client without inventory's own text. Verified (unit, stack) |
| BUG-3 | **fixed** | with `GATEWAY_PROVIDER=razorpay` and no webhook secret the service refuses to start unless `WEBHOOKS_DISABLED=true` is set by name. `compose.local.yaml` sets that explicitly for local polling-only runs; production must not. Verified (unit) |
| EDGE-1, EDGE-2 | **fixed (bounded wait, then refund)** | design decision: nothing waits forever. A saga that stays STUCK on `MARK_ORDER_PAID` or `CONFIRM_STOCK` (the customer has paid) for `SAGA_STUCK_REFUND_AFTER` (default 10 min) is moved, in one guarded transaction, onto the undo path: `REFUND_PAYMENT` (the payment service records a refund for the funding payment; the compensation worker refunds it with all its usual safeguards), then `CANCEL_PAID_ORDER` (a paid order may be cancelled only by this refunded cancel), then `RELEASE_STOCK` (a confirmed reservation is released and its units restocked). Moving the saga off the stuck step first means a late reply for it is ignored, so the saga can never complete after the refund. Until then `admin stuck` and `admin revive` still allow a person to fix the cause. Cost: a checkout whose replies were only lost (everything actually succeeded) is refunded and cancelled. Verified (integration, stack: order service down, inventory down) |
| EDGE-3, EDGE-4, EDGE-5 | **accepted trade-off, and the unbounded waits are closed** | a delayed or missed observation refunds a payment that was on time (the refund logic IS the outcome); clock skew is an operations matter (NTP). What could wait forever now cannot: unreachable gateway (IMP-6, decided at window + grace), stuck after payment (above), rejected refunds (below) |
| EDGE-6 | **fixed for rejected refunds, a person for the rest** | a refund that went to REVIEW only because the gateway kept rejecting it (nothing was created) is retried automatically after `COMPENSATION_REVIEW_RETRY_AFTER` (30 min), at most `COMPENSATION_MAX_REVIEW_RETRIES` (3) times, each recorded in the history. A refund whose outcome is unknown, or that holds a provider refund id, is never retried automatically (that would be a blind retry of a possibly-successful call); those stay for a person (`admin review`, `review-retry`). Verified (integration) |
| EDGE-7 | **partly** | parked events can be listed and replayed (`admin parked`, `parked-replay`); parked events older than 30 days are purged |
| EDGE-8 | **fixed for message bookkeeping** | a purge loop in every service removes published outbox rows, processed-message markers, finished jobs, processed webhook events (14 days), unresolved parked events (30 days), and in the orchestrator completed checkout attempts and finished sagas (30 days). Orders, reservations, payment attempts and refund records are never purged (a business decision). Verified (integration) |
| EDGE-9, EDGE-10 | **fixed** | the reservation sweeper claims the orphan attempt with one guarded UPDATE before releasing stock; the attempt row is the arbiter. A retried key gets a clear 409 "expired". Verified (integration, stack) |
| EDGE-11 | **fixed** | the order service replies `Failed` for a command that can never succeed; before payment, exhausted attempts or the deadline compensate (cancel the order, then release the stock) instead of going `STUCK`. Verified (integration, stack). Not done: a hook that fails the step the moment the consumer dead-letters it (the sweeper path covers it within the retry window) |
| EDGE-12 | **fixed** | an existing order with different contents is answered `Failed`; an identical repeat is a normal repeat. Verified (integration) |
| EDGE-13 | **fixed** | a cancel for an unknown order records a tombstone and succeeds; a late `order.create` is then refused; other transitions of a missing order are answered `Failed`. Verified (integration, stack) |
| EDGE-14 | **fixed** | a request that never left the process (connection refused, DNS, dial timeout) is retried quickly and, if the gateway stays down, fails in about 16 s with the saga compensating, instead of waiting out the window. Verified (unit, integration, stack) |
| EDGE-15 | **mitigated** | the resends are harmless; the payment service now decides at window plus grace (default about 10 min), before the saga's step gives up at about 12.5 min. Verified (stack, with shortened timers) |
| FEAT-1 | **done** | `orchestrator-service/cmd/admin` (`stuck`, `revive`) and `payment-service/cmd/admin` (`review`, `review-show`, `review-retry`, `parked`, `parked-replay`, `dead-jobs`). The RabbitMQ dead queue is not covered (use the management UI). Verified (integration; read-only commands run on the local database) |
| FEAT-2, FEAT-3, FEAT-4, FEAT-5 | **not done** | new product features (refund a completed order, retry payment for the same order, cancel a checkout, non-INR) that need API and state-machine design decisions, not fixes |
| IMP-1 to IMP-8, IMP-11, IMP-13 | **done** | see the rows above |
| IMP-9 | **done (log alert only)** | an outbox row that fails 20 times is logged at ERROR on every further failure; there is still no metric or paging |
| IMP-10 | **done** | see EDGE-8 |
| IMP-12 | **done locally** | the two empty leftover tables were dropped from the local database; the cloud database was not checked |
| IMP-14 | **done** | the bounded wait, then refund and unwind of a saga stuck after payment (EDGE-1, EDGE-2): new command `payment.refund`, saga steps `REFUND_PAYMENT` and `CANCEL_PAID_ORDER`, order service cancels a paid order only for a refunded cancel, inventory restocks on release of a confirmed reservation |
| IMP-15 | **done** | bounded automatic retry of refunds the gateway kept rejecting (EDGE-6) |
| IMP-6 | **done** | `PAYMENT_UNCONFIRMED_GRACE` (default 5 min). Verified (integration, stack) |

Side effects of the verification run (local database): one more dead job (the gateway-down check, by design), 0 new dead queue messages (379 before and after), and a few test checkouts, some compensated on purpose.

## Bugs

| ID | Bug | Status | Evidence | Suggested fix |
|---|---|---|---|---|
| BUG-1 | Idempotency key length is unbounded (a 300-char key is accepted and stored) | confirmed | probe returned 202 | cap ~255 chars, restrict charset (orchestrator checkout controller) |
| BUG-2 | Checkout limits (qty > 100, > 20 lines) are enforced only by inventory-service; the client sees `inventory returned status 400: inventory rejected the r…` (internal text leaks) | confirmed | probe output | validate in the orchestrator first, return a clean message |
| BUG-3 | Webhooks silently turn off when `RAZORPAY_WEBHOOK_SECRET` is unset (one log line, runs on polling only) | code-read | `payment-service/cmd/payment-service/main.go` ~line 92 | fail closed in production (refuse to start or require explicit opt-out) |

## Edge cases not handled

| ID | Situation | What happens today | Status |
|---|---|---|---|
| EDGE-1 | Payment succeeded but order-service cannot mark the order paid | `MARK_ORDER_PAID` failed → saga `STUCK`; money taken, no automatic refund | code-read (`saga/state.go`), design |
| EDGE-2 | Payment and order fine, but stock confirm fails (e.g. reservation released) | `CONFIRM_STOCK` failed → `STUCK`; order paid, stock not consumed | code-read, design |
| EDGE-3 | payment-service down longer than `PAYMENT_WINDOW` | on recovery every in-flight capture is first seen after the deadline → all refunded even if customers paid on time | code-read (follows from the lateness rule), not run, design |
| EDGE-4 | Delayed webhook makes an on-time payment look late | refunded unnecessarily (documented trade-off) | design |
| EDGE-5 | Clock skew between replicas/hosts | deadline compares app-clock times; a few seconds of skew can flip a close call | code-read |
| EDGE-6 | Refund records in REVIEW | wait for a person forever; nothing re-checks them | code-read |
| EDGE-7 | Unmatched webhook events | retried 12× then parked permanently; never cleaned up | code-read |
| EDGE-8 | Idempotency records | kept forever; table only grows (same for published outbox rows, processed_messages, webhook_events, saga_steps) | confirmed by row counts |
| EDGE-9 | **Orphaned checkout attempt stays `in_progress` after its reservation is released.** Crash after inventory reserved but before the saga transaction committed; the reservation sweeper later releases the stock but never closes the attempt. A retry with the same key re-reserves, inventory returns the existing reservation as `RELEASED`, the client maps it to `ErrReservationNotReserved`, a definitive 409 "checkout reservation is not active" is stored, and that key is dead forever. | code-read (`reservation_sweeper.go`, `inventory_client.go` line ~195, `checkout_completion.go`), not reproduced, design |
| EDGE-10 | **Reservation sweeper decides on a stale read.** It reads "no saga" and then calls inventory release with nothing atomic in between; a same-key retry creating the saga in that gap would have its reservation released from under a live saga (later `CONFIRM_STOCK` fails → `STUCK` with money taken). Window is tiny and only for reservations older than the TTL. | code-read, not reproduced |
| EDGE-11 | **order.create can never fail cleanly.** The order service only ever replies `Succeeded` for `order.create`. A command it cannot process (validation failure, poison payload) is dead-lettered with no reply; the saga step is resent up to 5 times, then the saga goes `STUCK` and keeps its stock reserved (a `STUCK` saga owns its reservation; the reservation sweeper never releases it). The `CREATE_ORDER, Failed` transition exists in `saga/state.go` but nothing produces it. The same happens when the order service is down for the whole retry window. | code-read (`order_create.go`, `order_commands.go`, `saga/state.go`, `reservation_sweeper.go`), not reproduced |
| EDGE-12 | **Existing order never compared with the command.** `order.create` inserts with `ON CONFLICT (id) DO NOTHING` and replies `Succeeded` even if the existing order has different items or total. Unlikely (the order id is generated once), but silent. | code-read |
| EDGE-13 | **order.mark-paid / order.cancel for an unknown order is a permanent failure with no reply** (`ErrOrderNotFound`). So compensating a checkout whose order was never created dead-letters the cancel and the saga hangs; and a late `order.create` can still create an order for a checkout that was given up on. | code-read (`order_commands.go`) |
| EDGE-14 | **Gateway completely down (request never sent) is treated like a timeout.** Any transport error from `CreateCharge` becomes `UNKNOWN`; the reconciler finds no gateway order, so the customer only learns of the failure after the full 5-minute window (`NO_GATEWAY_ORDER`). | code-read (`razorpay.go`, `payment_job_handler.go`) |
| EDGE-15 | **The saga resends `payment.initiate` while the customer is still paying.** `TAKE_PAYMENT` has no reply until payment resolves, so its 30 s timer resends the command about 4 times (30, 90, 210, 450 s). Harmless (the payment service finds the existing attempt and does nothing), but if payment is still unresolved at about 12.5 minutes the saga goes `STUCK`. | code-read (`saga_sweeper.go` `resendDelay`) |

Suggested fix for EDGE-9 and EDGE-10 together (recommended option A): the orchestrator-owned sweeper first claims the orphan with ONE guarded statement, `UPDATE checkout_attempts SET status='completed', response_code=409, response_body=<expired, retry with a new key> WHERE order_id=? AND status='in_progress' AND NOT EXISTS (SELECT 1 FROM sagas WHERE id=?)`, and only if exactly one row changed does it call inventory release. `completeCheckoutAttempt` is already guarded on `status='in_progress'` and rolls back the saga transaction when it affects 0 rows, so the attempt row is the arbiter: whoever flips it first wins. Alternatives considered: B) let inventory re-reserve a released reservation for the same order id; C) never release while an attempt is `in_progress` (stock held longer).

## Missing features

| ID | Feature | Why it matters |
|---|---|---|
| FEAT-1 | Admin tooling: list/inspect/resolve REVIEW refunds, STUCK sagas, dead messages, dead jobs (safe replay) | today: 13 REVIEW refunds, 379 dead messages, 2 dead jobs, no safe way to act |
| FEAT-2 | Refund of a completed order (merchant/customer, incl. partial) | refunds exist only for failed or extra captures |
| FEAT-3 | Retry payment for the same order after a failure | `order_id` is unique per attempt, so a failed payment needs a brand-new checkout |
| FEAT-4 | Cancel a checkout before payment completes | not available |
| FEAT-5 | Non-INR currencies and amount edge cases beyond mismatch handling | untested beyond INR |

## Also open (from the production review, deliberately parked)
CI, deployment/edge (TLS, auth, rate limiting — auth is a recorded scope exclusion), load-test results, metrics/alerting/tracing, versioned migrations, readiness probes on every service, backups/HA, real Razorpay capture/refund/webhook verification.

## Suggested order when we return
1. BUG-1, BUG-3 (small, clear). 2. EDGE-1/EDGE-2 after deciding auto-refund vs manual review. 3. FEAT-1 (admin tooling). EDGE-3/4 are policy questions first.

## Side effects of the probe run (local DB only)
One real order created by the 300-char-key probe; a few junk webhook events (unknown order, captured-with-no-entity, duplicate, ignored). The dead queue is unchanged.


## Improvements to make (agreed design direction, 2026-10-24)
Nothing here is implemented yet. IDs link to the edge cases above.

| ID | Where | Improvement | Closes |
|---|---|---|---|
| IMP-1 | order-service | Reply `Failed` (step `CREATE_ORDER`) when `order.create` fails permanently (validation), instead of only dead-lettering it; also from the consumer's dead-letter hook once retries are exhausted | EDGE-11 |
| IMP-2 | order-service | `order.cancel` for an unknown order records a `CANCELLED` tombstone and replies `Succeeded`; a later `order.create` for that id finds it and does nothing (cancel and create can arrive in either order) | EDGE-13, late-created order |
| IMP-3 | order-service | Compare an existing order with the command; reply `Failed` on mismatch | EDGE-12 |
| IMP-4 | orchestrator saga sweeper | Rule: **before payment** (step `CREATE_ORDER`), when attempts run out or the saga deadline passes, do NOT mark `STUCK`; compensate automatically (no money involved) | EDGE-11 |
| IMP-5 | orchestrator saga state table | Compensation for a `CREATE_ORDER` failure goes `MARK_ORDER_CANCELLED` then `RELEASE_STOCK` (the order may exist even without a reply); keep `STUCK` for steps after payment starts | EDGE-11, EDGE-13 |
| IMP-6 | payment-service reconciler | The payment service is the only decider of the payment outcome. After the payment window plus a grace period (suggested +5 min, so decided at ~10 min), if the gateway lookup keeps failing, mark the attempt `FAILED` (reason e.g. `UNCONFIRMED`) and reply `Failed`; a capture found afterwards is refunded by the existing late-capture path (add a test for exactly this). The orchestrator never gives up on `TAKE_PAYMENT` by itself (it would cancel an order that payment-service later marks paid). Saga deadline (15 min) must stay above window + grace | EDGE-3, unknown-payment handling |
| IMP-7 | orchestrator sweeper | Claim an orphan checkout attempt with ONE guarded `UPDATE ... WHERE status='in_progress' AND NOT EXISTS (saga)`, close it with a clear 409 "expired, retry with a new key", and only if exactly one row changed release the stock | EDGE-9, EDGE-10 |
| IMP-8 | orchestrator | Cap the Idempotency-Key length (~255) and restrict its characters; validate the line and quantity limits in the orchestrator and return clean messages | BUG-1, BUG-2 |
| IMP-9 | all relays | Outbox rows: maximum attempts and an alert/metric for a poison or long-unpublished row (today retried forever with backoff capped at 30 s) | relay gap |
| IMP-10 | all services | Purge old published outbox rows, processed_messages, checkout_attempts, saga_steps, webhook_events with a documented retention window | EDGE-8 |
| IMP-11 | webhooks | Fail closed when `RAZORPAY_WEBHOOK_SECRET` is unset in production | BUG-3 |
| IMP-12 | local DB housekeeping | Empty leftover tables `orders.jobs` and `orders.checkout_attempts` exist in the local database but are not created by the current migration; drop them (check the cloud database too) | housekeeping |
| IMP-13 | payment-service gateway call | Tell "request never sent" (connection refused, DNS failure before sending) apart from "sent, outcome unknown" (timeout, reset after sending). Retry the first quickly, bounded; keep the second as `UNKNOWN` | EDGE-14 |
