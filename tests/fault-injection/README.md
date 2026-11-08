# Failure-case tests

## What is being tested

What the system does when something breaks. The goal is not that nothing fails, but that a failure never produces wrong state: no duplicate payment, no duplicate reservation, no lost or stuck saga, no growth of the dead-letter queue, and no oversold stock. A request that was never accepted is allowed to be refused; a request that was accepted must reach a final state.

Three kinds of failure are covered:

1. **Infrastructure faults under load** — a service is killed with `kill -9`, the broker restarts, the database connection drops, the network is slow.
2. **Payment-gateway failures** — the gateway declines, or it times out and leaves the outcome ambiguous.
3. **Overload and scaling limits** — too many clients on one row, or two orchestrator replicas.

## What has been done

All seven fault scenarios were run on the AWS staging stack (one host per service, one broker host, Neon database, gateway simulator) on 1 November 2026. Each scenario offered 200 checkouts at 50 per second from 30 concurrent clients; the fault was injected about 1.5 s in, with 56-57 sagas in flight, then removed. The harness waited for every accepted saga to finish and compared 8 database invariant queries before and after. Gateway failures were run through the correctness suite in `decline` and `timeout` mode. The harness is [`tools/fault-injection/chaos.py`](../../tools/fault-injection/chaos.py).

## Verdict

**PASS, 7 of 7 fault scenarios.** Every accepted checkout finished, nothing was duplicated, and the dead queue did not grow. Read the limits before treating this as production evidence.

## Cases

| # | Failure | Accepted | Refused while down | In flight at fault | Drain (s) | Result |
|---|---|---:|---:|---:|---:|:---:|
| [01](01-payment-kill9-mid-flight.md) | payment-service `kill -9` during gateway calls | 200 | 0 | 57 | 9.5 | PASS |
| [02](02-orchestrator-kill9-mid-burst.md) | orchestrator `kill -9` mid-burst | 143 | 57 | 57 | 11.9 | PASS |
| [03](03-order-service-kill9-mid-message.md) | order-service `kill -9` mid-message | 200 | 0 | 56 | 21.0 | PASS |
| [04](04-inventory-kill9-mid-message.md) | inventory-service `kill -9` mid-message | 143 | 57 | 57 | 11.8 | PASS |
| [05](05-broker-restart-mid-burst.md) | RabbitMQ restart | 200 | 0 | 57 | 9.5 | PASS |
| [06](06-db-connection-drop.md) | database connection drop (20 s) | 200 | 0 | 57 | 46.5 | PASS |
| [07](07-network-delay.md) | network delay 200 ms ± 50 ms (40 s) | 200 | 0 | 57 | 16.6 | PASS |
| [08](08-two-orchestrator-replicas.md) | two orchestrator replicas | 200 | 0 | – | 11.9 | PASS (correctness); throughput not measured |
| [09](09-payment-declined.md) | gateway declines the payment | – | – | – | – | PASS on rerun (3 s window) |
| [10](10-payment-timeout-ambiguous.md) | gateway timeout, ambiguous outcome | – | – | – | – | PASS |
| [11](11-overload-hot-product.md) | 500 clients on one hot product | 2,033 | 151 (503) | – | – | accounting check failed; no invariant violated |

**Why some checkouts were refused:** when the orchestrator is down it cannot answer; when inventory-service is down the checkout is refused because the stock reservation is a synchronous call. In both cases 57 of 200 requests were refused and the other 143 completed. A refused request was never promised. With the broker down nothing was refused, because accepting a checkout writes only to the Postgres outbox and the broker catches up later.

## Limits of this folder

- Each fault scenario ran once, at one load size, with the gateway simulator in `success` mode. No repeats and no variance figures.
- No ambiguous (UNKNOWN) payment arose during the kill scenarios, so the path "gateway outcome unknown, never retried blindly" is covered by case 10 and by unit tests, not by a cloud fault run.
- Neon autosuspend was not tested; only a connection drop on the payment host. The network delay applied to the payment host only.
- Every service ran as a single replica with one broker and one database: a learning deployment, not a high-availability one.
- Not covered: backup and restore, loss of the broker volume, alerting on stuck sagas, and an operator runbook drill.

**Raw data:** [`data/`](data/)
