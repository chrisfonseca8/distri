# Razorpay contract reference (for the gateway simulator)

Written 2026-10-23 from the official docs and from calls against the real Razorpay
Test Mode API. It lists what **our application** uses, not all of Razorpay.

Evidence labels (every row below carries one):

- **D** — Documented by an official Razorpay page (URL in the source list).
- **O** — Observed against the real Test Mode API, with sanitized evidence below.
- **A** — Assumed: the simulator chooses a behaviour the docs do not specify. Never a provider guarantee.
- **F** — Injected fault: deliberately introduced by a simulator scenario.

Nothing here is a statement about production capacity, latency, or account-specific settings.

## Sources

Fetched 2026-10-23: `razorpay.com/docs/api/` — `authentication`, `orders/create`, `orders/fetch-all`, `orders/fetch-with-id`, `orders/fetch-payments`, `payments/fetch-with-id`, `payments/fetch-all-payments`, `payments/capture`, `refunds/create-normal`, `refunds/fetch-all`; `razorpay.com/docs/payments/payments/capture-settings` (+ `/api`), `payments/refunds`; `razorpay.com/docs/webhooks/` (overview, `best-practices`, `validate-test`, `payloads/payments`, `payloads/refunds`). Pages that returned 404 (`refunds/fetch-specific`, `refunds/fetch-multiple`) are **not** relied on.

## Authentication and errors

| Item | Contract | Label |
|---|---|---|
| Scheme | HTTP Basic, key id as user, key secret as password | D |
| Test vs live | chosen in the Dashboard; the docs describe no key-id prefix. `rzp_test_…` is what our keys look like | D (mode) / O (prefix) |
| Wrong secret | HTTP **401**, description "Authentication failed" | O |
| Error envelope | `{"error":{"code","description","source","step","reason","metadata","field"}}`; code `BAD_REQUEST_ERROR` | D |
| Status of validation errors | HTTP 400 for every listed Orders/Refunds/Payments error, including unknown ids | D |
| Rate limits | not documented; no 429 body documented | D (absence) |
| 429 / 5xx bodies | simulator uses the same envelope with synthetic descriptions | F |

## Orders

| Item | Contract | Label |
|---|---|---|
| Create | `POST /v1/orders`; `amount` (int, subunits) and `currency` required; `receipt` ≤ 40 chars; `notes` ≤ 15 pairs × 256 chars; optional `payment` object (`capture`, `capture_options`) overrides the account capture setting | D |
| Minimum amount | INR 1.00 (100 paise); the docs disagree on ">100" vs ">=100" | D (conflict); simulator accepts 100 | A |
| Fields returned | `id`, `entity:"order"`, `amount`, `amount_paid`, `amount_due`, `currency`, `receipt`, `offer_id`, `status`, `attempts`, `notes`, `created_at` | D |
| Status | `created` (no attempt) → `attempted` (a payment attempt exists) → `paid` (a payment captured; final) | D |
| Fetch one | `GET /v1/orders/{id}`; unknown id → HTTP 400 "The id provided does not exist" | D |
| List | `GET /v1/orders`; filters `receipt`, `authorized`, `from`, `to`, `expand[]`; `count` default 10, max 100; `skip`; envelope `{entity:"collection",count,items}` | D |
| Payments of an order | `GET /v1/orders/{id}/payments` → collection of payment entities | D |
| **Receipt uniqueness** | Docs: unique, a repeat is rejected with 400 "Duplicate request…". **Real API accepted a second create with the same receipt and made a second order** (two orders, same receipt) | D vs **O** — conflict |
| **Receipt lookup consistency** | Docs silent. Real API returned no items for the receipt right after a create; the order appeared about **17 s** later (one sample) | **O** (n = 1) |
| Idempotency header | none documented for Orders. `X-Payout-Idempotency` exists for RazorpayX payouts only | D |
| Order id after a lost create response | not recoverable except by lookup (list by receipt) | A |

## Payments

| Item | Contract | Label |
|---|---|---|
| Fetch | `GET /v1/payments/{id}`; unknown id → 400 "The id provided does not exist" | D |
| Entity | `id`, `entity:"payment"`, `amount`, `currency`, `status`, `order_id`, `method`, `captured`, `amount_refunded`, `refund_status` (`null`/`partial`/`full`), `description`, `error_code`, `error_description`, `error_source`, `error_step`, `error_reason`, `fee`, `tax`, `created_at`, `notes` | D |
| Status values | `created`, `authorized`, `captured`, `refunded`, `failed` (the docs do not define the transitions) | D |
| Transitions the simulator uses | created → authorized → captured (auto-capture); authorized → captured (manual capture); created/authorized → failed; captured → refunded (full refund) | A |
| List all | `GET /v1/payments`, filters `from`, `to`, `count` (≤100), `skip`; **no filter by order** | D |
| Capture | `POST /v1/payments/{id}/capture` with `amount` and `currency`; payment must be `authorized`; amount must equal the authorized amount; capturing a captured payment → 400 | D |
| Auto-capture | account setting (Dashboard) by default; per-order `payment.capture` overrides; applies to Orders API payments only | D |
| Uncaptured authorization | must be captured within 3 days (custom minimum 12 min) or it is auto-refunded | D |
| Payment ids / order of attempts | one order can have several payment attempts (a failed attempt can be followed by another) | D (behaviour), A (ordering) |

## Refunds

| Item | Contract | Label |
|---|---|---|
| Create | `POST /v1/payments/{id}/refund`; `amount` optional (omit = full), `speed`, `notes`, `receipt` | D |
| Eligibility | only `captured` payments; "The payment status should be captured for action to be taken." | D |
| Errors | fully refunded → 400 "The payment has been fully refunded already."; over the captured amount → 400; zero amount → 400 "Amount cannot be blank."; < INR 1.00 → 400; duplicate receipt on the same payment → 400; active dispute → 400 | D |
| Refund `receipt` as idempotency key | the error text says "receipt is treated as an idempotency key" — documented for refunds. Whether a rejected duplicate returns the original refund is **not** documented | D (statement) / A (what the rejection returns) |
| Refund entity | `id` (`rfnd_`), `entity`, `amount`, `currency`, `payment_id`, `receipt`, `status`, `speed_requested`, `speed_processed`, `created_at`, `notes`, `acquirer_data`, `batch_id` | D |
| Refund status | `pending` → `processed` (final) or `failed` | D |
| List | `GET /v1/refunds` (`from`, `to`, `count` ≤ 100, `skip`) | D |
| Fetch one / per payment | `GET /v1/refunds/{id}` and `GET /v1/payments/{id}/refunds` are implemented by the adapter and the simulator, but the fetched docs pages for them returned 404, so the paths are **not documented evidence** — treat a lookup that fails as unknown, never as "no refund" | A (paths) |
| `X-Refund-Idempotency` header | documented as a request header on refund creation; retention window and what a replay returns are **not** documented. The application sends it (`cmp-<operation id>`) but never relies on it: the stored operation id (also receipt and note), the lookup, and the full-refund cap are the real safeguards | D (header) / A (replay behaviour) |
| Reconciling an uncertain refund | `GET /v1/payments/{id}` exposes `amount_refunded` and `refund_status` — the documented lookup | D |
| Timing | normal 5–7 working days, instant "almost immediately"; simulator uses a configurable delay | D (text) / A (value) |
| Whether a pending refund already counts in `amount_refunded` | not documented | A (simulator: yes) |

## Webhooks

| Item | Contract | Label |
|---|---|---|
| Delivery | HTTP POST of JSON; URL on port 80/443 only; separate Test and Live URLs | D |
| Envelope | `entity:"event"`, `account_id`, `event`, `contains[]`, `payload`, `created_at` | D |
| Events used | `payment.authorized`, `payment.captured`, `payment.failed`, `order.paid`, `refund.created`, `refund.processed`, `refund.failed`, `refund.speed_changed` | D |
| Payload | `payload.payment.entity` (and `payload.order.entity` for `order.paid` — field names not shown in the fetched page); refund events carry `payload.refund.entity` plus `payload.payment.entity` | D (partly) |
| Signature | header `X-Razorpay-Signature` = hex HMAC-SHA256 of the **raw body** with the **webhook secret** (not the API key secret) | D |
| Event id | header `x-razorpay-event-id`, unique per event | D |
| Semantics | at-least-once; ordering "ideally in order, not guaranteed" | D |
| Acknowledge | any 2xx within **5 s**; otherwise the attempt times out and the event is resent | D |
| Retry | non-2xx → exponential backoff for **24 h** after the event; attempt count and intervals **not documented**; after 24 h of failures the webhook is disabled | D |
| Our retry schedule | simulator-chosen (default 5 s, 25 s, 2 min, 10 min, 1 h, repeating the last to 24 h); an accelerated schedule exists for tests | A |
| Event payload snapshot | each event keeps the entity as it was at event creation, even when resent later | D ("snapshot of the entity when the event occurred") |

## How the simulator delivers webhooks (stage 3)

| Item | Behaviour | Label |
|---|---|---|
| State first | the gateway transition commits, then the event snapshot is built from that state and queued; a webhook never causes a transition | A (design) |
| Events emitted | `payment.authorized`, `payment.failed`, `payment.captured`, then `order.paid` (a capture produces both, as the docs list both events) | D (names) / A (that both are sent) |
| Payload | the documented envelope; `payload.payment.entity` is the payment as it was at event creation and is never regenerated; `order.paid` also carries `payload.order.entity` | D (shape) / A (field set) |
| Signing | hex HMAC-SHA256 of the exact outgoing bytes under a **dedicated simulation webhook secret**, header `X-Razorpay-Signature` | D |
| Event id | header `x-razorpay-event-id`, stable and unique per event; retries and redeliveries keep the id and the original bytes | D (header) / A (id format) |
| Acknowledgement | only a 2xx counts; a 5 s timeout, any other status and any transport error are retried | D |
| Retry schedule | **ours**: 5 s, 25 s, 2 min, 10 min, 1 h, then hourly until 24 h (the docs give only "exponential backoff for 24 hours"); `accelerated` = 1 s, 2 s, 4 s, 8 s repeating, for functional checks only | A |
| Delivery delay | median 300 ms, seeded jitter, per event | A |
| Duplicates / reordering / loss | a per-checkout plan scripts exact delivery delays, omissions and duplicate deliveries per event name; `POST /_sim/webhooks/{id}/redeliver` sends an event again | F |
| Persistence | none: pending deliveries are lost when the simulator restarts | limitation |

The application's side: events are deduplicated by `x-razorpay-event-id` (database unique index),
stored durably and acknowledged within 5 s, then applied by a worker that uses the same business
rules as polling. Business deadline: a capture counts only if the gateway captured it before the
checkout's `expires_at`; the capture time is the event's `created_at`. Anything later fails the
attempt as `PAYMENT_EXPIRED` and queues a refund. (Application policy, not a Razorpay behaviour.)

## Deadline evidence audit (Stage 4)

Which timestamp decides whether a capture met the checkout deadline, and what each candidate
actually is. Evidence labels as above.

| Timestamp | What it is | Precision | Evidence | Used for the deadline? |
|---|---|---|---|---|
| Payment entity `created_at` | when the payment (attempt) was created; constant across that payment's events in Razorpay's samples | Unix, 10 digits (seconds, inferred) | D (field exists) / D (samples) | **No.** It is creation, not capture |
| Webhook event `created_at` | top-level field of the event. The docs do not define its meaning or units; the samples are 10-digit integers; **in the documented netbanking sample the authorized and captured events carry the same value** | seconds (inferred, not stated) | D (absence of a definition) / D (sample) | **No.** Recorded in the log for audit only. (Before Stage 4 the webhook path used `min(event.created_at, now)` as the capture time: that trusted an unverified provider claim) |
| Capture time field | the payment entity has a boolean `captured` and **no `captured_at`** | — | D (absence) | n/a |
| Local webhook receipt time | `webhook_events.received_at`: our clock when the handler stored the verified event | Go `time.Time` (µs) | Ours | **Yes** — the capture time for webhook-sourced evidence |
| Local poll observation time | our clock when a gateway lookup returned showing the capture | µs | Ours | **Yes** — the capture time for poll-sourced evidence (a lookup does not say when the capture happened) |
| Local decision / transaction time | when the decision function ran (application clock, not the database clock) | µs | Ours | Only as a fallback when no observation time was supplied (never the case in the production paths) |
| Simulator internal capture time | the simulator's clock at the capture transition; it also becomes the simulated event's `created_at` | seconds in the event | Simulator | **No.** It is never exposed through the Razorpay adapter and the application must not assume the real API supplies it |

### Policy

A capture completes a checkout only if **we first observed it before the deadline** (`expires_at`,
or `created_at + PAYMENT_WINDOW`). Observation time is sound evidence in one direction: a capture
seen before the deadline was necessarily made before it, whatever any clock says. In the other
direction there is no proof: a capture first seen after the deadline may have happened earlier, but
we cannot show it, so it is treated as **late**.

- The deadline never extends. A provider timestamp that claims an earlier capture does not move it.
- A cancelled or expired attempt is never resurrected, however old the event claims the capture is.
- Captured money found after the attempt failed, or first seen late, is **recorded for compensation**
  in `payments.compensation_records` (one row per gateway payment id) and takes the attempt's
  refund slot; any additional distinct captured payment gets its own `REVIEW` row.
- Polling (expiry pass) and the webhook worker use the same decision function and the same guarded
  update, so the first to commit wins and the other changes nothing and sends no second reply.
- Expiry is deferred while a capture event received before the deadline is still waiting, unapplied,
  in the inbox: the webhook worker then decides it from the receipt time.

### Costs of the conservative rule (accepted)

- A customer who paid before the deadline but whose notification (or our next poll) reached us after it
  is cancelled and refunded: **unnecessary refund and a lost sale** (scenarios E and the boundary trials
  of G). The loss is bounded by webhook/poll lag around the deadline.
- If the expiry pass commits before an on-time event is applied (the event was received but the
  worker had not claimed it, and expiry was not deferred), the same outcome follows.
- A person whose capture lands exactly at the deadline is on the late side (the window is half-open:
  observation must be strictly before `expires_at`).
- Clock skew between application instances moves `expires_at` and receipt times against each other by
  the skew; it is not compensated.
- `SUCCEEDED` attempts are not polled, so an additional capture on them (for example on a duplicate
  gateway order) is found only if a webhook for it arrives.

## What the simulator does NOT claim

- Production latency, throughput, or rate limits (none documented; all timings are configuration, label **A**).
- That Razorpay returns a particular response when a duplicate refund receipt is sent.
- The order of payment attempts or exact auto-capture timing on any real account.
- Anything about `order.paid` payload keys beyond "order and payment entities".

## Observed evidence (sanitized; real Test Mode API, 2026-10-23)

- Key id shown as `rzp_test_…` + last four characters; secrets never logged.
- Create: `POST /v1/orders {amount:10000,currency:"INR",receipt:<uuid>,notes:{order_id:<uuid>}}` → 200, `status:"created"`; `GET /v1/orders/{id}` returned the same receipt and notes.
- Duplicate receipt: a second identical create → 200 with a **different** order id. `GET /v1/orders?receipt=<uuid>` later returned **both** orders (newest first).
- Lookup lag: `GET /v1/orders?receipt=<uuid>` immediately after create → no items; visible after ≈ 17 s in the measured run (`go run ./cmd/razorpay-check`).
- Wrong secret → HTTP 401 "Authentication failed".

Capture, refund and webhook behaviour have **not** been observed against the real API: that needs a payment that a customer completes in Checkout.


## Refund lifecycle as the application models it (Stage 5, 2026-10-24)

Local workflow state (`payments.compensation_records.status`) is kept apart from the provider's refund status (`provider_status`):

| Workflow state | Meaning | Leaves by |
|---|---|---|
| REQUIRED | a refund is owed; nothing sent | claim (lease) → SUBMITTING |
| SUBMITTING | operation id stored, request in flight or lost with the worker | response → PENDING / PROCESSED / FAILED; no/lost response or stale lease → UNKNOWN |
| UNKNOWN | outcome ambiguous; **never blindly resent** | lookup by operation id / `payment.amount_refunded` → adopt (PENDING/PROCESSED); same key retried only when lookup proves no refund; otherwise REVIEW |
| PENDING | provider accepted, status `pending` | fetch or `refund.processed`/`refund.failed` webhook |
| PROCESSED | provider status `processed` (final) | — |
| FAILED | provider/gateway rejected before any refund existed | bounded retry with a new operation → REQUIRED, else REVIEW |
| REVIEW | needs a person: lost provider state, failed refund, repeated rejection, legacy record | manual |

Rules: no DB transaction spans an HTTP call; a record is keyed by (gateway environment, payment id); a refund is never larger than the captured amount and the sum of refunds never exceeds it; additional captures for one order each get their own record (found by webhook or by the bounded audit poll). Every worker selects and submits only records of its own gateway label and re-checks `record.gateway == attempt.gateway` before calling out; the payment job handler refuses to call a gateway for an attempt labelled for another environment (2026-10-24 fix).

Not claimed: Razorpay's behaviour on duplicate refund keys, refund fetch paths, refund webhook ordering. The real refund path has not been exercised with live credentials.
