# Architecture design

This is a text file showing how the entire data flow takes place.
We are starting from the user.

## USER ENTRY FROM HERE

The User will enter from the point of `POST /v1/checkouts` in the `orchestrator service`.

The User will have a request body {(product ids and quantities) plus an Idempotency-Key header}.

### The steps that will be followed

1. **Controller:** read and shape the request.
2. **`prepareCheckoutRequest`:** validate and fingerprint.
3. **`claimCheckoutAttempt`:**
    1. Here the order id is created (`uuid.new()`).
    2. One SQL insert on `checkout_attempts`, with `ON CONFLICT (idempotency_key)`:
        - `New key`: a row is created.
        - `Same key and same hash`: the existing row comes back, with its original order id. This is the replay case.
        - `Same key and a different hash`: nothing is returned, and the request gets 422.
4. **`reserveCheckoutInventory`:** reserve stock over HTTP.
    - The orchestrator calls inventory-service synchronously with the order id, the request hash and the items.
    - Inventory checks stock, reserves it, and returns the trusted prices and total. Prices come from inventory, never from the client.
    - Definitive failures end the request: 404 unknown product, 409 not enough stock, 400 invalid input.
    - If inventory can't be reached, the request fails with 503 without a stored answer, so a retry with the same key continues safely. Reservations are idempotent on the order id.
5. **`createSagaFromReservation`:** one transaction starts the saga. Inside a single database transaction:
    - Create the `sagas` row (status `RUNNING`, current step `CREATE_ORDER`, with a deadline of now + 15 min).
    - Record `RESERVE_STOCK` as already `SUCCEEDED`.
    - Create the `CREATE_ORDER` step as `RUNNING`.
    - Write the `order.create` command to the outbox.
    - Mark the checkout attempt completed with the `202` response body.
