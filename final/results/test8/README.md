# Test 8 — replica test (final result)

**Date:** 1 Nov 2026, 21:21–21:45 UTC · **Environment:** `staging` (ap-southeast-2) · **Run:** `cloud-test8a-20261101T212117Z` (full evidence in `../cloud-test8a-20261101T212117Z/`).

**Data in this folder:** [`chaos-test.json`](chaos-test.json) / [`chaos-test.md`](chaos-test.md) (the scenario result), [`audit.json`](audit.json).

## Verdict

**Correctness with two orchestrator replicas: PASS.** The throughput comparison was **not completed**: it was dropped on the owner's decision (to be revisited with auto-scaling and horizontal scaling), after a first attempt showed it cannot be measured with the current completion mechanism (see below).

## What was done

**8a, correctness.** A second orchestrator container (same image, same environment, same database and broker, no published port) was started next to the first on the orchestrator host. A burst of 200 checkouts at 50 per second from 30 clients went to the first replica. Both replicas consumed from the same RabbitMQ queues and ran the same outbox and sweeper work. After the burst the harness waited for every saga to finish and checked the invariants. The second replica was then removed.

| Check | Result |
|---|---|
| Checkouts accepted | 200 |
| Final saga status | 200 COMPLETED |
| Unfinished or stuck sagas | 0 |
| Duplicate payments, duplicate reservations, completed sagas without an order | 0, 0, 0 |
| Invariant violations before and after | none, none |
| Dead-queue growth | 0 |
| Time for the last saga to finish after the burst | 11.9 s |
| Saga steps completed by the second replica | 400 of the run's 800 (200 sagas × 4 steps) |

The second replica did half of the saga work, so the competing-consumer and `SKIP LOCKED` logic share the work correctly and produce no duplicate effects. The first replica's log count (80,060) is its lifetime total since it started and is not comparable.

## What the dropped throughput attempt showed

A first attempt to compare throughput (500 clients, SSE completion, three repetitions with two replicas) was started, then stopped; it is not part of the results. Its first two repetitions, read from the runner's console log, were far worse than the single-replica Test 7 baseline of 43 completed/s: repetition 1 completed 1817 of 2094 accepted (30.3/s), repetition 2 completed 299 of 799 accepted (5.0/s). Neither replica logged any warning or error, and their CPU was low.

The cause is in the code: `orchestrator-service/pkg/statusstream/hub.go` is documented as "in-process … for the current single-replica orchestrator deployment". A saga's completion notification goes to the hub of the replica that handled the final reply. With two replicas, a completion handled by replica 2 never reaches clients waiting on replica 1, so those clients wait until their fallback. **The SSE completion path therefore does not work across replicas.** The database and the sagas themselves were correct throughout; only the push notification is replica-local. I confirmed the hub's comment and wiring in the code, and I did not run an experiment that isolates this as the only cause.

## Limits

1. **Throughput with two replicas was not measured,** so "does a second orchestrator help?" is unanswered.
2. **One burst of 200 checkouts** for the correctness check; no fault was injected while two replicas ran.
3. **Both replicas ran on the same host,** sharing its two vCPUs, so this test says nothing about horizontal scaling across hosts.
4. **The other services still run as single replicas,** and so do the database and the broker.
5. **Run record not filled in** (git commit, image tag, Neon branch).

## Open items for the scaling work

- Make completion notification work across replicas (for example a database-backed or broker-backed notification instead of the in-process hub), or serve status from the database only.
- Repeat the replica throughput comparison in poll mode and, once notification works, in SSE mode, on separate hosts behind a load balancer.
