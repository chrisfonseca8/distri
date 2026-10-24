# First baseline: low-rate sanity run (B0)

**This is a sanity run, not a capacity benchmark.** 50 checkouts over 60 s is 0.83/s; it shows that the
whole path (orchestrator → order → inventory → payment → simulator → webhook/poll → compensation
bookkeeping) completes cleanly and gives a first latency/backlog reading. It says nothing about the
throughput limit, and the gateway is the **simulator**, so none of it is real Razorpay or real
payment-gateway latency. Local Postgres in Docker, one replica per service.

Run 2026-10-24; results in `test-results/baseline/B0.json` and in `memory.md`.

## Workload

| Item | Value |
|---|---|
| Arrivals | 50, open loop, fixed spacing 1.2 s (arrival *i* fires at t0 + 1.2·*i* regardless of earlier ones) |
| Cart | 1 line, quantity 1 |
| Products | uniform: seeded (42) shuffle of all 50 seeded products, each used exactly once; the per-product counts are written to the report |
| Stock | ~1,000,000 units per product, so stock is never the limit |
| Faults / refund scenarios | none; compensation worker and reconcilers are running and should have nothing to do |
| Drain | up to 180 s after the last arrival, polling every saga until `COMPLETED` / `COMPENSATED` / `STUCK` |

## Stack configuration (pinned; recorded into the report from the running containers)

Gateway: simulator scenario mix `capture=100`, seed 42, API latency `normal` (median ~60 ms, assumed), pay delay 2 s,
capture delay 500 ms, webhooks **on** with the `standard` retry profile (5 s, 25 s, 2 m, …, assumed), 4 webhook
workers, 300 ms median webhook delay. Payment service: `PAYMENT_WINDOW=5m`, reconcile interval 15 s, min poll 10 s,
audit 1 m / 1 h, compensation interval 2 s, webhook inbox interval 1 s, DB pool 10 open / 5 idle per service.
(Polling stays on as the normal recovery path; with webhooks working it should mostly find work already done.)

```sh
# 0. pinned stack: unsets any short fault-test timings, recreates the simulator and payment-service,
#    prints the effective configuration (secrets filtered) and fails on any mismatch
loadgen/baseline/start_stack.sh

# 1. the run (also writes test-results/baseline/B0.json); do not restart the simulator until it finishes
python3 loadgen/baseline/baseline.py --label B0
```

Checks of the runner itself (no traffic): `cd loadgen/baseline && python3 -m unittest -v` and
`python3 loadgen/baseline/baseline.py --dry-run`.

Do not run the Go correctness suite, `payment-service` integration tests against `distri`, or the
`stage5` harness between steps 0 and 2.

## What the report records

- **Accepted** checkouts/s (2xx over the offer window) and **completed** checkouts/s (sagas terminal) — separately, over the
  offer window and over the total including drain; the gap is the backlog.
- Checkout POST latency (p50/p95/max) and end-to-end latency, checkout start → saga terminal (mean/p50/p95/p99/max).
- HTTP status mix; final saga status mix; unresolved sagas after the drain (ids listed).
- Backlogs before, after and the maximum seen while sampling every 5 s: unpublished outbox rows (all four services),
  pending jobs, unprocessed webhook inbox rows, running sagas, open compensation records; RabbitMQ messages
  ready+unacked across `distri.*` queues.
- Dead-queue count before/after and the delta (expected 0).
- Container CPU % per service (peak, sampled) and memory; local database size before/after (whole database — **not** a
  per-checkout figure; Neon is not involved in this run).
- The effective service/simulator configuration, copied from the containers (secrets filtered out).

## Reading the result

Pass: 50/50 accepted, 50/50 `COMPLETED`, 0 unresolved, dead-queue delta 0, outbox/jobs/inbox back to their starting values
after the drain, no new `compensation_records`. Anything else is a finding to explain before a rate is raised.
Report sanity-run numbers with their window; do not extrapolate them to a limit.
