# Unit and integration tests

## What is being tested

The behaviour of each Go module in isolation, close to the code: saga state transitions, idempotency, outbox and relay behaviour, retry and backoff, reservation arithmetic and concurrency, payment reconciliation and refunds, webhook handling, configuration guards, and the load-generator's own helpers. These are the tests that run on every push in CI.

Two kinds, labelled in every file below:

- **Unit** — runs with no external service.
- **Integration (needs Postgres)** — runs against a real PostgreSQL database. In a plain `go test ./...` these skip themselves; they run when `DATABASE_TEST_URL` points at a throwaway `*_test` database and the matching `RUN_*_INTEGRATION=1` flag is set (done by [`tools/integration/run-integration.sh`](../../tools/integration/run-integration.sh) and the `integration` job in CI).

The `*_test.go` files stay next to the code they test. This folder documents them: one file per module, one section per test file, one row per test function.

## What has been done

Run on 8 November 2026 with `go test ./... -count=1 -v`, every test binary started from an empty directory (so no stray `.env` is read):

| Run | Passed | Failed | Skipped |
|---|---:|---:|---:|
| Plain run, no database | 334 | 0 | 130 |
| With a disposable PostgreSQL 17 container, all integration flags on, `-race`, one package at a time | **470** | **0** | **0** |

Counts include subtests. Counting top-level test functions only: 292 in total, all of which pass with the database. The 130 skips in the plain run are exactly the database-gated tests.

## Modules

| Module | Test files | Test functions | Unit | Integration (Postgres) | Result with database |
|---|---:|---:|---:|---:|---|
| [`gateway-simulator`](gateway-simulator.md) | 4 | 34 | 34 | 0 | all pass |
| [`inventory-service`](inventory-service.md) | 7 | 12 | 5 | 7 | all pass |
| [`order-service`](order-service.md) | 11 | 31 | 23 | 8 | all pass |
| [`orchestrator-service`](orchestrator-service.md) | 25 | 85 | 46 | 39 | all pass |
| [`payment-service`](payment-service.md) | 22 | 106 | 58 | 48 | all pass |
| [`tools`](tools.md) (load generator, correctness suite) | 9 | 24 | 24 | 0 | all pass |
| **Total** | **78** | **292** | **190** | **102** | **all pass** |

## Tests that map directly to a Step-3 change or a headline claim

| Behaviour | Test |
|---|---|
| No oversell of the last 10 units under 200 concurrent buyers | `TestConcurrentBuyersCannotOversellLastTenUnits` — [inventory-service](inventory-service.md) |
| A poisoned outbox row becomes dead; a broker outage does not count | `TestPoisonMessageBecomesDeadButBrokerOutageDoesNotCount` — [order-service](order-service.md) |
| Unreachable gateway past window + grace fails the attempt; inside grace it never guesses | `TestUnreachableGatewayPastWindowPlusGraceFailsTheAttempt`, `TestUnreachableGatewayInsideTheGraceKeepsAskingAndNeverGuesses` — [payment-service](payment-service.md) |
| Bounded refund after payment | `refund_after_payment_integration_test.go` — [payment-service](payment-service.md) |
| Idempotency-key and checkout-limit validation | `TestPrepareRejectsBadIdempotencyKeysAndLimitsWithCleanMessages` — [orchestrator-service](orchestrator-service.md) |
| Webhook secret required unless disabled by name | `TestRazorpayWithoutWebhookSecretRefusesToStartUnlessDisabledByName` — [payment-service](payment-service.md) |

## Limits

- Passing tests show the scenarios they encode behave as written; they do not prove the absence of other defects.
- Mutation testing was not re-run for this report, so no claim is made about how many defects the tests would catch.
- Integration tests ran against PostgreSQL 17 in Docker, not Neon, and on a single machine.
- Database-backed results here are from the 8 November run; the cloud campaign of 1 November is documented in the other folders.
