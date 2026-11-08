# gateway-simulator

A stateful stand-in for the parts of the Razorpay API this project uses, for testing **our**
backend under repeatable payment scenarios. It is not Razorpay and says nothing about
Razorpay's production latency, capacity or account settings. Every timing is an assumption, not a measurement of Razorpay.

State is in memory: a restart resets orders, payments, refunds and **pending webhook
deliveries**.

## Start the stack against the simulator

```sh
docker compose -f compose.local.yaml -f compose.simulator.yaml --profile simulator up -d --build
```

`compose.simulator.yaml` sets simulation-only values for both services (provider `razorpay`,
base URL `http://gateway-simulator:9090`, API key `rzp_test_simulator` / `simulator_secret`,
webhook destination `http://payment-service:8082/v1/webhooks/razorpay`, webhook secret
`simulator_webhook_secret`, accelerated webhook retries). The real `RAZORPAY_*` values in
`.env` are overridden and never sent to the simulator. Polling stays enabled as the recovery path.

Attempts created through the simulator are stored with gateway `razorpay-sim`, so a stack later restarted against the real API (whatever `.env` selects) does not poll or refund them.

Back to whatever `.env` selects:

```sh
docker compose -f compose.local.yaml --profile simulator stop gateway-simulator
docker compose -f compose.local.yaml up -d payment-service
```

## Scenarios

| Scenario | What the simulated customer does |
|---|---|
| `capture` | pays: `payment.authorized`, then automatic capture: `payment.captured` + `order.paid` |
| `decline` | the attempt fails: `payment.failed` (the order stays payable) |
| `none` | never pays |
| `authorize_only` | authorizes; waits for a manual capture |

A checkout's scenario comes from the seeded mix (`SIM_SCENARIO_MIX`, e.g. `capture=70,decline=20,none=10`)
and depends only on the seed and the receipt, so it is the same on every run. Control endpoints
(unauthenticated, outside the business API):

- `PUT /_sim/scenario/{receipt}?name=…` — pin a checkout before its gateway order exists
- `PUT /_sim/default-scenario?name=…` — pin every unpinned checkout
- `PUT /_sim/order-scenario/{gatewayOrderID}?name=…` — pin one existing order, including a duplicate
- `GET /_sim/webhooks?payment_id=…` — delivery state of recent events
- `POST /_sim/webhooks/{eventID}/redeliver` — send an event again (same id, same bytes)
- `GET /_sim/metrics` — requests, in-flight, scheduler lag, store counts, webhook backlog and attempts, heap

## Plans: scripted timing and delivery

A plan scripts one checkout exactly: when each payment attempt happens and what it does, and,
independently, when each webhook event is delivered, delayed, duplicated or never sent. It replaces the
seeded randomness for that checkout, so scenarios do not depend on sleeps or scheduling. All plan
durations are exact (not divided by `SIM_TIME_SCALE`); attempt offsets count from gateway-order
creation, delivery delays from event creation.

```sh
curl -X PUT localhost:9090/_sim/plan/<receipt> -d '{
  "attempts": [
    {"at": "2s", "outcome": "decline"},
    {"at": "7s", "outcome": "capture", "capture_after": "1s"}
  ],
  "delivery": {
    "payment.authorized": {"delay": "8s"},
    "payment.captured":   {"delay": "0s", "duplicates": ["500ms", "2s"]},
    "order.paid":         {"omit": true}
  }
}'
```

`outcome` is `decline`, `authorize` or `capture`; delivery rules are per event name
(`payment.authorized`, `payment.failed`, `payment.captured`, `order.paid`). `PUT /_sim/order-plan/{gatewayOrderID}`
scripts an existing order (offsets count from now). An omitted event is listed by `GET /_sim/webhooks`
with state `omitted`. Two events with the same delay are delivered concurrently by different workers.

## Configuration (environment)

| Variable | Default | Meaning |
|---|---|---|
| `SIM_SEED` | 1 | seeds every decision |
| `SIM_API_LATENCY` | zero | `zero`, `normal` (median 60 ms) or `slow` (800 ms) — assumed |
| `SIM_PAY_DELAY` / `SIM_CAPTURE_DELAY` | 2s / 500ms | time to the payment attempt / to auto-capture — assumed |
| `SIM_LOOKUP_DELAY_MIN` / `_MAX` | 0 / 0 | hide new orders from list-by-receipt (real API lagged ~17 s once) |
| `SIM_AUTO_CAPTURE` | true | account-level capture setting |
| `SIM_ENFORCE_RECEIPT_UNIQUE` | false | `true` = documented behaviour (reject), `false` = observed (accept) |
| `SIM_DUPLICATE_PAYS` | first | **simulator policy**: which duplicate orders the customer pays (`first` or `all`) |
| `SIM_TIME_SCALE` | 1 | divides scheduled delays; accelerated test time, not a wall-clock profile |
| `SIM_WEBHOOK_URL` / `SIM_WEBHOOK_SECRET` | empty | webhook destination and the dedicated simulation secret (empty URL disables) |
| `SIM_WEBHOOK_PROFILE` | standard | `standard` retries 5s,25s,2m,10m,1h (**assumed**); `accelerated` 1s,2s,4s,8s |
| `SIM_WEBHOOK_TIMEOUT` | 5s | acknowledgement timeout (documented) |
| `SIM_WEBHOOK_MAX_AGE` | 24h | stop retrying after this (documented window) |
| `SIM_WEBHOOK_WORKERS` | 4 | bounded delivery concurrency |
| `SIM_WEBHOOK_DELAY` | 300ms | median delay from transition to first attempt — assumed |

## Refunds (stage 5)

- API: `POST /v1/payments/{id}/refund` (honours `X-Refund-Idempotency`; receipt dedupe is off unless `EnforceRefundReceiptUnique`), `GET /v1/refunds/{id}`, `GET /v1/refunds`, `GET /v1/payments/{id}/refunds`. Full-refund cap and over-amount rejection as documented.
- Refund status `pending` → `processed`/`failed` after a configurable delay; `refund.created`, `refund.processed`, `refund.failed` webhooks go through the same delivery rules as payment events.
- Plans can script refund rules per payment/receipt: response faults (`lost` via connection hijack, `slow`, `reject`, `error_before` = nothing created, `error_after` = refund created but the caller sees a 5xx), delayed visibility of the created refund, and a final provider status.
- **State is in memory.** A simulator restart forgets every refund. Compensation records that were PENDING at that moment end in `REVIEW` ("the gateway does not know refund … that it earlier accepted"); that is correct behaviour and unrecoverable against the new instance. Persistence was deliberately not added to clean up test history.

## Not implemented yet

429/5xx and outage injection beyond the refund/response faults above, persistence, load profiles.
