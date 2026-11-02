# Test 5 — load baseline on the cloud stack (final result)

**Date:** 1 Nov 2026, 19:26–19:39 UTC · **Environment:** `staging` (ap-southeast-2; four service hosts, one broker host, Neon database; gateway simulator in `success` mode with 200 ms latency) · **Run id:** `cloud-test5-20261101T192617Z`

**Data in this folder:** [`load-test.md`](load-test.md) / [`load-test.json`](load-test.json) (the runner's report for the three repetitions), [`phase22-audit.md`](phase22-audit.md) (database audit after the run), [`audit.json`](audit.json) (observability audit), [`run.json`](run.json).
**Full evidence** (Prometheus export, logs): `../cloud-test5-20261101T192617Z/`.

## Verdict

**PASS.** The canonical C2 baseline ran three independent repetitions. Every offered workflow completed, no invariant failed, and the database audit afterwards was clean. The run is a record of how the deployment performs at low load, to compare later tests against. It is **not** a target.

**The headline is that each checkout takes about 7 seconds even when the stack is idle, and almost all of that is a configured delay.** Ten clients get **1.55 completed checkouts per second**, with a **median end-to-end time of about 7 seconds and a p95 of 11 seconds**. Nothing is saturated (see below). The payment step averages 6.2 s of the 6.75 s saga, and the gateway simulator in staging is configured with `SIM_PAY_DELAY: 5s` (`deploy/cloud/compose.simulator.yaml`), a deliberate delay before the simulated customer pays. So this baseline measures a latency chosen in the test setup, not the speed of the application.

## What was done

C2 is the standard baseline: 10 closed-loop clients, a 10 s warm-up, then 60 s of measurement, repeated 3 times. Between repetitions the runner resets transactional data and inventory, and purchases are spread round-robin over all 50 products. No setting was overridden, so this is the canonical case. It was run from the load-generator host against the staging services.

## Results

| | Run 1 | Run 2 | Run 3 | Mean | CV |
|---|---:|---:|---:|---:|---:|
| Offered = accepted = completed | 94 | 97 | 88 | 93 | – |
| Accepted and completed per second | 1.57 | 1.62 | 1.47 | **1.55** | 4.0% |
| Checkout p95 (the accept call) | 54 ms | – | 85 ms | 66 ms | 20.7% |
| End-to-end p95 | 11.1 s | 10.2 s | 12.6 s | **11.3 s** | 8.5% |
| Pending at the drain | 0 | 0 | 0 | – | – |

Run 1 in detail: checkout p50 / p95 / p99 = 38 / 54 / 84 ms, and end-to-end p50 / p95 / p99 = 6.9 / 11.1 / 15.7 s. All 94 checkouts returned `HTTP 202` with no 5xx errors, transport errors or cancelled orders. The accepted and completed rates are equal because nothing was left unfinished. Little's law holds (observed in-flight workflows 10.9 against 10.9 expected), so the measurement is internally consistent.

Invariants: stock bounds, one reservation and payment per order, paid orders have a successful payment and a confirmed reservation, cancelled orders have failed payments: all pass in every repetition. The phase-22 audit after the run passes (1 of 1).

## Where the time goes (not where it does not)

| Service (run 1) | CPU mean / max | DB pool waits | Requests |
|---|---|---:|---:|
| orchestrator | 7% / 13% | 65 (1.1 s total) | 6264 |
| order | 2.5% / 7.8% | 0 | 0 |
| inventory | 1.4% / 6.8% | 0 | 95 |
| payment | 3.5% / 9.4% | 0 | 282 |

- **No service is busy.** Every service sat below 14% CPU, and the database pools barely waited. Postgres showed no lock waits and no deadlocks.
- **The accept step is fast.** The checkout call answers in about 66 ms (p95). It includes a database write, which makes a distant database an unlikely explanation. My earlier guess that a Neon database in `us-east-1` caused the delay was wrong for this symptom.
- **The time is in the payment step.** Mean step durations over the three repetitions (Prometheus, 342 sagas): create order 0.16 s, confirm stock 0.33 s, mark order paid 0.14 s, **take payment 6.16 s**, saga total 6.75 s. Inside the payment step the gateway API calls took about 1 ms and webhook processing about 14 ms. The simulator's configured pay delay (`SIM_PAY_DELAY`, default 5 s) accounts for most of it. About 1.2 s of the payment step is not explained by what I measured.
- **Throughput here is clients divided by latency** (10 / 6.9 s is about 1.5 per second), because every workflow spends most of its time in that fixed simulated delay. More clients should raise throughput almost linearly until some service or database saturates. That is what test 6 will find out.

## Observability (`audit.json`)

- **Verdict PASS, no violations.** At the end: 0 stuck sagas, 0 dead outbox rows, 0 unknown or unconfirmed payments, 0 stock violations.
- **Alerts fired:** `DatabasePoolSaturated` and `ServiceDown`. Neither is explained by this run, and I did not look into either. `DatabasePoolSaturated` has fired in every cloud run so far.
- The 5xx and saga totals in the audit are cumulative since the services started, so they include earlier tests. This run's own count was 0 5xx.

## Limits

1. **Simulated gateway** (`success`, 200 ms), not Razorpay.
2. **Low load only:** 10 clients. This says nothing about behaviour near the limit.
3. **Three short repetitions** (88–97 workflows each), so the spread figures rest on few samples.
4. **The 7 s median is mostly a simulator setting** (5 s pay delay), so the baseline latency says little about the real payment path. About 1.2 s of the payment step is unexplained.
5. **The run record is not filled in:** git commit, image tag, Neon branch and the deployed worker and poll settings.

## Open items

- Explain the remaining ~1.2 s in the payment step (webhook delivery, reconciliation polling, or job pickup).
- Confirm which Neon region each service's database is in.
- Explain the two alerts that fired.
