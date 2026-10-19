# Load generator and correctness testing

The local correctness suite is a destructive integration test. It is intentionally run only against the disposable development database created by `compose.local.yaml`. Every run starts from migrations plus the inventory seed; there is no payment seed.

The implementation is in [`correctnesstesting/local`](correctnesstesting/local). Every numbered phase has its own Go file, and the comment at the top of that file states the property being tested.

## Correctness phases

1. **Environment and migrations** — services respond, schemas exist, 50 products are seeded, and payments start empty.
2. **Inventory validation and pricing** — input boundaries, product existence, and server-controlled prices/totals.
3. **Atomic inventory rollback** — a failed multi-line reservation leaves no partial rows or stock changes.
4. **Inventory idempotency** — identical retries reuse one reservation; a changed hash conflicts; stock moves once.
5. **Confirm and release transitions** — terminal inventory operations are idempotent and contradictory transitions fail.
6. **Checkout validation** — checkout inputs and inventory errors map to the expected public behavior.
7. **Checkout idempotency and concurrency** — sequential and concurrent retries create one order and reserve once.
8. **Checkout crash recovery** — reserved for a deterministic crash failpoint; currently reported as skipped.
9. **Payment gateway outcome** — asynchronous processing reaches the outcome selected by `GATEWAY_MODE`.
10. **Payment idempotency and concurrency** — concurrent payment creates yield one attempt and one ID.
11. **Order/payment consistency** — wrong amount or currency cannot settle an order.
12. **Payment polling and timing** — `/pay` returns a terminal 200 or an explicitly pending 202 consistently.
13. **Successful checkout end to end** — success produces `PAID`, `SUCCEEDED`, `CONFIRMED`, and one stock movement.
14. **Declined payment end to end** — decline produces `CANCELLED`, `FAILED`, `RELEASED`, and no consumed stock.
15. **Ambiguous payment timeout** — timeout produces `UNKNOWN` while the order and reservation stay pending/held.
16. **Inventory settlement recovery** — reserved for deterministic inventory fault injection; currently skipped.
17. **Concurrent limited-stock checkout** — 50 buyers compete for ten units without overselling.
18. **Multi-item deadlock resistance** — reversed carts complete under contention without deadlock.
19. **Terminal order idempotency** — retrying settlement creates no second payment or stock movement.
20. **HTTP error mapping** — representative invalid, missing, conflict, and validation errors keep stable status codes.
21. **Transaction boundary** — database transactions are not left idle while waiting on service/network work.
22. **Database invariants** — final cross-schema audit for stock bounds, duplicates, and contradictory terminal states.

## Run locally

From the repository root:

```sh
docker compose -f compose.local.yaml down --volumes
docker compose -f compose.local.yaml up --build --wait
mkdir -p test-results
LOCAL_UID=$(id -u) LOCAL_GID=$(id -g) \
  docker compose -f compose.local.yaml --profile correctness run --rm --no-deps local-correctness
docker compose -f compose.local.yaml down --volumes
```

The default gateway mode is `success`. Run the suite on a newly rebuilt database with `GATEWAY_MODE=decline` or `GATEWAY_MODE=timeout` to activate the corresponding outcome-specific phase. A phase that does not apply to the selected mode is marked `SKIP`, not `PASS`.

## Results and metrics

Each run writes both Markdown and machine-readable JSON to:

```text
test-results/correctness-local/
```

The report contains the description, assertions, notes, status, wall-clock duration, request count, transport/decode error count, and HTTP latency `min`, `mean`, `p50`, `p95`, `p99`, and `max` for every phase. Percentiles use the nearest ranked observed request duration. These latency samples make regressions visible, but this suite is for correctness—not capacity sizing; sustained throughput belongs in the load-test suite.

The process exits non-zero when any phase fails. Skips are documented in the report and do not make the run fail.
