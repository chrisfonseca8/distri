# Failure case 08 — Two orchestrator replicas

**Result:** PASS for correctness. Throughput with two replicas was **not measured**.

## What is tested

Two orchestrator processes (same image, environment, database and broker) run side by side and compete for the same RabbitMQ queues, outbox rows and sweeper work. Work distribution relies on `FOR UPDATE SKIP LOCKED` claims and idempotent consumers, so the second replica must share the work without causing duplicate effects.

## How it is run

A second orchestrator container was started on the orchestrator host (no published port) and 200 checkouts were sent at 50 per second from 30 clients to the first replica. After the burst the harness waited for every saga to finish and checked the invariants; the second replica was then removed.

## Result

| Measure | Value |
|---|---:|
| Checkouts accepted | 200 |
| Final saga status | 200 COMPLETED |
| Unfinished sagas | 0 |
| Payments beyond one per order | 0 |
| Reservations beyond one per order | 0 |
| Accepted sagas without an order | 0 |
| Dead-queue growth | 0 |
| Time for the last saga to finish after the burst (s) | 11.9 |
| Saga steps completed by the second replica | 400 of 800 (200 sagas × 4 steps) |

The first replica's event count in the JSON (80,060) is its lifetime total since it started and is not comparable.

## Finding: SSE completion is replica-local

A first attempt to compare throughput (500 clients, SSE completion, two replicas) was stopped and is not part of the results. Its first two repetitions completed 30.3/s and 5.0/s, far below the single-replica baseline of about 43/s, with no warnings or errors in either replica. The cause is in the code: `orchestrator-service/pkg/statusstream/hub.go` is an in-process hub for a single replica, so a completion handled by one replica never reaches clients waiting on the other until their fallback. The database and the sagas were correct throughout; only the push notification is replica-local. This was confirmed from the code and wiring, not isolated by an experiment.

## Limits

- Throughput with two replicas was not measured.
- One burst of 200 checkouts; no fault injected while two replicas ran.
- Both replicas shared one host (two vCPUs), so this says nothing about scaling across hosts.
- All other services, the broker and the database remained single instances.

**Raw data:** [`chaos-test.json`](data/two-replicas/chaos-test.json), [`chaos-test.md`](data/two-replicas/chaos-test.md), [`audit.json`](data/two-replicas/audit.json)
