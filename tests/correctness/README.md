# Correctness tests

## What is being tested

Whether the system keeps its **invariants** — not how fast it is. The rules under test:

- Stock is never negative and reserved never exceeds on-hand; concurrent buyers cannot oversell.
- Prices come from the server, never from the client.
- A repeated request with the same idempotency key creates one order, one reservation and one payment; the same key with different input is rejected.
- An order is PAID only if its payment SUCCEEDED; a declined payment cancels the order and releases the stock.
- A gateway timeout is *ambiguous*: the payment stays `UNKNOWN`, stock stays held, nothing is guessed or retried blindly.
- Crashes of a service in the middle of a step are recovered by the saga sweeper.
- No database transaction stays open while waiting on another service.
- Error responses map to stable HTTP status codes.

## What has been done

1. **Cloud correctness suite** — 22 numbered phases (20 active; phases 11 and 12 were retired when the `/pay` endpoint was removed) run against the live AWS staging stack in three gateway modes. Four runs are recorded: success, decline (default payment window), decline (rerun with a 3 s window) and timeout. The suite is [`tools/correctness`](../../tools/correctness); each phase has its own file below.
2. **Oversell test** — 500 concurrent buyers for 10 units on the cloud stack: [`oversell.md`](oversell.md).
3. **Final database audit** after every load and fault test: [`final-audit.md`](final-audit.md).

The same invariants are also covered at unit and integration level; see [`../unit/`](../unit/README.md).

## Verdict

**PASS, with caveats.** Every one of the 20 active phases passed at least once, and the database audit was clean after all four runs. It was **not** one clean run:

- No single run covers every phase. A phase that does not apply to a gateway mode reports SKIP, not PASS (phases 08, 13, 16, 19 need `success`; 14 needs `decline`; 15 needs `timeout`).
- The first decline run failed (phases 09, 14, 21, 22) because the default payment window had not elapsed within the phase; it passed on a rerun with a 3 s window and 1 s reconciliation. The decline result therefore holds for those timings only.
- The dead queue grew by 2 rejected `reply.payment` messages in each of the decline and timeout runs; the cause is not verified.
- `DatabasePoolSaturated` fired in every run (see the known issues in [`../REPORT.md`](../REPORT.md)).

## Phases (status in each recorded run)

| Phase | Test | success | decline (default window) | decline (3 s window) | timeout |
|---:|---|:---:|:---:|:---:|:---:|
| [01](phases/01-environment-and-migrations.md) | Environment and migrations | PASS | PASS | PASS | PASS |
| [02](phases/02-inventory-validation-and-pricing.md) | Inventory validation and pricing | PASS | PASS | PASS | PASS |
| [03](phases/03-atomic-inventory-rollback.md) | Atomic inventory rollback | PASS | PASS | PASS | PASS |
| [04](phases/04-inventory-idempotency.md) | Inventory idempotency | PASS | PASS | PASS | PASS |
| [05](phases/05-confirm-and-release-transitions.md) | Confirm and release transitions | PASS | PASS | PASS | PASS |
| [06](phases/06-checkout-validation.md) | Checkout validation | PASS | PASS | PASS | PASS |
| [07](phases/07-checkout-idempotency-and-concurrency.md) | Checkout idempotency and concurrency | PASS | PASS | PASS | PASS |
| [08](phases/08-order-command-recovery.md) | Order command recovery | PASS | skip | skip | skip |
| [09](phases/09-payment-gateway-outcome.md) | Payment gateway outcome | PASS | **FAIL** | PASS | PASS |
| [10](phases/10-payment-idempotency-and-concurrency.md) | Payment idempotency and concurrency | PASS | PASS | PASS | PASS |
| [13](phases/13-successful-checkout-end-to-end.md) | Successful checkout end to end | PASS | skip | skip | skip |
| [14](phases/14-declined-payment-end-to-end.md) | Declined payment end to end | skip | **FAIL** | PASS | skip |
| [15](phases/15-ambiguous-payment-timeout.md) | Ambiguous payment timeout | skip | skip | skip | PASS |
| [16](phases/16-payment-command-recovery.md) | Payment command recovery | PASS | skip | skip | skip |
| [17](phases/17-concurrent-limited-stock-checkout.md) | Concurrent limited-stock checkout | PASS | PASS | PASS | PASS |
| [18](phases/18-multi-item-deadlock-resistance.md) | Multi-item deadlock resistance | PASS | PASS | PASS | PASS |
| [19](phases/19-terminal-order-state-idempotency.md) | Terminal order state idempotency | PASS | skip | skip | skip |
| [20](phases/20-http-error-mapping.md) | HTTP error mapping | PASS | PASS | PASS | PASS |
| [21](phases/21-transaction-boundary.md) | Transaction boundary | PASS | **FAIL** | PASS | PASS |
| [22](phases/22-database-invariants.md) | Database invariants | PASS | **FAIL** | PASS | PASS |

Phases 11 and 12 are retired (they tested the removed `/pay` endpoint).

## Other tests in this folder

| Test | Verdict |
|---|---|
| [Oversell: 500 buyers, 10 units](oversell.md) | PASS |
| [Final database audit](final-audit.md) | PASS |

**Raw data:** [`data/`](data/)
