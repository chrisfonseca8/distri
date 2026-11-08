# Penetration tests

## What is being tested

Whether an attacker, rather than a well-behaved client, can break the system's security properties: forge a payment or webhook, bypass price or stock checks, replay or tamper with requests, read or change another customer's order, or exhaust the service.

## What has been done

**No penetration test has been performed on this project, and no result in this folder is a security claim.** The folder exists so that the gap is visible and not hidden.

Authentication and authorization are **out of scope by decision** (the API has no login), so tests for authentication bypass, session handling and access control between users do not apply yet.

## Security-relevant controls that do exist (tested as correctness or unit tests, not as attacks)

These are ordinary functional tests. They show that specific defences were built and are exercised, not that the system resists a determined attacker.

| Control | Tested by |
|---|---|
| Client cannot choose prices; totals come from the server | Correctness [phase 02](../correctness/phases/02-inventory-validation-and-pricing.md) |
| Webhook signature (HMAC-SHA256 of the raw body) is verified; an invalid signature changes nothing | `TestVerifySignature`, `TestInvalidSignatureChangesNothing` in [`handler_test.go`](../../payment-service/pkg/webhooks/handler_test.go) |
| The payment service refuses to start with Razorpay and no webhook secret unless webhooks are disabled by name | [`webhook_config_test.go`](../../payment-service/pkg/config/webhook_config_test.go) |
| Idempotency keys are length-limited and charset-limited; checkout line and quantity limits are enforced with clean error messages | [`checkout_request_test.go`](../../orchestrator-service/pkg/services/checkout/checkout_request_test.go) |
| Oversized request bodies are rejected | `TestCreateCheckoutRejectsOversizedBody` in [`checkout_controller_test.go`](../../orchestrator-service/pkg/controllers/checkout_controller_test.go) |
| Replay of the same checkout creates no second order, reservation or payment | Correctness phases [07](../correctness/phases/07-checkout-idempotency-and-concurrency.md), [10](../correctness/phases/10-payment-idempotency-and-concurrency.md), [19](../correctness/phases/19-terminal-order-state-idempotency.md) |
| Error responses do not leak internal text and map to stable status codes | Correctness [phase 20](../correctness/phases/20-http-error-mapping.md) |
| Secrets (`.env`, keys, Terraform state) are not tracked in git | `.gitignore`; checked by listing tracked files before release |

## Known gaps (not tested, not built)

- No authentication, authorization, TLS termination or rate limiting (recorded as parked in `docs/backlog.md` (git history)).
- No dependency vulnerability scan, container image scan or static security analysis has been run.
- No fuzzing of request bodies, headers or webhook payloads beyond the functional tests above.
- No test of webhook replay with a valid old signature, or of timing attacks on signature comparison.
- No test against real Razorpay webhooks over public HTTPS.

## What a first penetration test would cover (plan, not done)

1. Webhook forgery and replay against `payment-service`: missing, malformed and stale signatures; duplicate event ids.
2. Input abuse on `POST /v1/checkouts`: oversized and malformed JSON, negative and huge quantities, duplicate product lines, key injection, unicode and control characters in the idempotency key.
3. Enumeration and access: guessing order ids through `GET /v1/checkouts/{id}` (relevant once authentication exists).
4. Resource exhaustion: many concurrent connections and slow clients against the SSE stream.
5. Internal services exposed on the network: unauthenticated inventory, order and payment APIs reachable from outside the intended path.

Findings from any such run would be recorded in this folder with the same rules as the others: exact commands, raw output, and no claim beyond what was observed.
