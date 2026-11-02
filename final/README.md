# Final cloud tests — to complete before the project is called done

Status (2 Nov 2026): **tests 1–10 run, reports in `final/results/test2` … `test10`.** Test 2 correctness (all phases pass across 3 gateway modes), 3 faults (7 of 7 pass), 4 oversell (exactly 10 of 500 sold), 5 baseline (1.55 completed/s at 10 clients; 5 s of the 7 s is the simulator's pay delay), 6 limit (about 1000 clients, 50 completed/s with SSE completion; polling completion peaks at 300 clients), 7 bottleneck (no variable tried moved the number), 8 replicas (correct with 2 orchestrators; throughput not measured, SSE completion is single-replica), 9 soak (5 minutes only, steady), 10 final audit (clean; instances not yet stopped and Neon data not yet deleted). Steps: [`tests-6-10-steps.md`](tests-6-10-steps.md), [`test3-steps.md`](test3-steps.md).

What exists today (so you know the starting point):

- Local only: 271 Go tests (incl. the database-gated integration tests), 17 mutation checks, 10 fault-injection scenarios, contention/soak/worker-sweep load runs. Report: `test-results/local/full-test-report.md`.
- In AWS: about 45 load runs (26–31 Oct) and 20 phase-22 invariant audits. Of the 22 correctness phases only phase 22 has ever run there. No fault injection has ever run there.

Time estimate (estimates, not measurements): code and tooling 9–12 h, AWS setup 1–3 h, the runs themselves 6.5–8 h, about **8–10 h of billable instance time**, roughly 2.5–3 working days elapsed.

---

## 0. Before you start (prerequisites)

### 0.1 Code fixes that must land first
Otherwise test 3 fails for a reason we already know.

- [x] **Stuck-payment gap.** Policy: keep reconciling through the payment window plus `PAYMENT_UNCONFIRMED_GRACE` (default 5 minutes), then fail unresolved attempts as `UNCONFIRMED`. Never retry an ambiguous charge. A capture observed later is recorded for refund, never accepted for the cancelled checkout.
- [x] **Outbox dead state.** A permanently unroutable row is marked dead after 20 such failures and is no longer claimed. Transient broker failures do not increment the permanent-failure counter. `Store.Requeue` resets the counter and revives a dead row.

### 0.2 Tests to add first (no cloud needed)
- [x] **Oversell test:** 200 goroutines buying the last 10 units; exactly 10 succeed, stock never below zero (inventory-service).
- [x] **Saga "always ends" test:** walks every step and outcome in the transition table; a final state is always reachable, never a dead end.
- [x] **Poison message test:** verifies the dead limit, claim exclusion, requeue, and that a broker outage does not count as poison.

### 0.3 Safety rules (non-negotiable)

- Set an **AWS budget alert** before starting any instance.


---

## 1. The tests, in order

Pass criteria are the definition of done for each step. Record raw JSON for every run and commit it to the repo (not under the ignored `test-results/`).

| # | Test | What to do | Pass means | Est. |
|---|---|---|---|---|
| 1 | **Smoke run** | Start the stack, run 10 checkouts. | All 10 reach `COMPLETED`. Confirms wiring before spending money. | 10 min |
| 2 | **Full correctness suite, all active phases** (success, decline and timeout modes) | Run phases 1–10 and 13–22 in the cloud; phases 11–12 are retired with the removed `/pay` endpoint. | Every applicable phase is green across the three mode reports; only documented outcome-specific skips are allowed. The success run must execute recovery phases 8 and 16, and phase 22 must be clean after every mode: no oversell, duplicate payment or reservation, or paid order without stock. | 45 min |
| 3 | **Fault tests, one at a time, ~30 clients** | Run each fault below under load; run the phase-22 audit after each. | For all: no duplicate charges or reservations, no stuck sagas, dead queue unchanged. | 1–1.25 h |
| 3a | `kill -9` payment-service during gateway calls | | Every saga reaches a final state within the payment window plus grace. | |
| 3b | `kill -9` orchestrator, order-service, inventory-service | one at a time | as above | |
| 3c | Restart RabbitMQ | | as above | |
| 3d | Drop the database connection, or let Neon autosuspend | | as above | |
| 3e | Add network delay between hosts | | as above | |
| 4 | **Oversell on the cloud** | Hot product, 500 buyers for 10 units. | Exactly 10 succeed; stock never goes below zero. | 15 min |
| 5 | **Load baseline** | Low-rate sanity run; save raw JSON in the repo. | Run completes, invariants pass, JSON committed. | 15 min |
| 6 | **Step sweep to find the limit** | Raise concurrency step by step. | You can state where completed/s stops growing. Report **accepted and completed rates separately**, plus p95 and p99. | 30 min |
| 7 | **Bottleneck experiment** | Change **one variable per run**: worker count, then poll intervals, then DB pool. Repeat each run 3 times. | You can state which limit moved the number. | 1.5–2 h |
| 8 | **Replica test** | Run two orchestrator replicas. | All sagas complete, no duplicate effects, throughput improves **or** you can explain why not. | 30–40 min |
| 9 | **Soak** | 30–60 min at about 70 % of the measured limit. | Latency does not drift; DB size growth matches the retention jobs; the outbox backlog returns to zero. | 1–1.25 h |
| 10 | **Final audit and cleanup** | Run the phase-22 audit once more, delete the test data, terminate every instance. | Audit clean, Neon branch data removed, no instance still running. | 20 min |

Known open item to watch in steps 6–7: locally, throughput stopped near **25 completed/s** and raising payment `WORKER_CONCURRENCY` from 8 to 16 did not help. That second limit is undiagnosed. Do not just add workers; check queue depth, consumer concurrency, claim poll intervals and DB pool waits first.

---

## 2. Existing tools to reuse

- `testing/run-integration.sh` — Go tests with every gated integration test, race detector on.
- `testing/chaos/chaos.py` — fault-injection harness (local container names; needs the SSH port).
- `testing/loadtesting/run-cases.sh` — load cases; remote mode via `DISTRI_REMOTE=1`.
- `testing/chaos/summarize_load.py` — compact summary of runs newer than a timestamp.
- `testing/report/build_report.py` — builds the Markdown report from the artefacts on disk.

## 3. Not covered even after all of the above

These need separate work and are **not** closed by this list:

- Real Razorpay Test Mode success path (blocked: no UI to complete a payment) and real webhooks over public HTTPS.
- Every service is a single replica; the database and broker are single points of failure. Say so in any production claim.
- Backup and restore of Neon, loss of the broker volume.
- An operator runbook (reviving `STUCK` sagas, refunds after a hard crash, rolling restarts with `SIGTERM`) and alerting on `STUCK` sagas, dead-letter growth and outbox backlog.
- The webhook batch / concurrency sweep recommended by the 2026-10-25 choke report.

## 4. Done when

All of steps 1–10 pass, the raw JSON and the final report are committed, the AWS budget alert never fired, and the instance list is empty.
