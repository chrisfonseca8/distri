# Test 10 — final audit and cleanup (final result)

**Date:** 1 Nov 2026, 21:56–22:00 UTC · **Environment:** `staging` (ap-southeast-2) · **Run:** `cloud-test10-20261101T215559Z` (full evidence in `../cloud-test10-20261101T215559Z/`).

**Data in this folder:** [`phase22-audit.md`](phase22-audit.md), [`audit.json`](audit.json), [`database-state.txt`](database-state.txt), [`run.json`](run.json).

## Verdict

**Audit: PASS. Cleanup: NOT done, by decision.** The final database audit is clean. The cleanup half of the test (delete the test data, stop every instance) was not carried out, because the owner asked only for the soak and the audit. All six staging instances are still running, so they are still costing money until they are stopped.

## Final audit

The phase-22 audit ran against the staging database after all the load tests and fault tests, on the data of the last load run (the Test 9 soak).

| Check | Result |
|---|---|
| Phase-22 audit, all 11 assertions | **PASS**, 0 violations in each |
| Observability audit | PASS, no violations, no alerts firing |
| Stuck sagas, dead outbox rows, dead-queue messages, unknown or unconfirmed payments, incomplete refunds, stock violations | all 0 |
| The 8 harness invariant queries | all 0 |
| Sagas | 12,736, all COMPLETED; none unfinished |
| Orders / payments / reservations | 12,736 PAID / 12,736 SUCCEEDED / 12,736 CONFIRMED |
| Minimum free stock; products with negative stock | 999,745; none |
| Dead queue `distri.commands.dead` | 0 messages |

The audit's first assertion prints "timed out waiting for the worker backlog" next to a PASS. This text is the same in earlier passing audits and appears to be a fixed label, not a real timeout. I did not read the audit code to confirm it.

## State left behind by the tests

- All settings changed in Test 7 were restored to their original values and verified (`DB_MAX_OPEN_CONNS=10`, `WORKER_CONCURRENCY=4`, outbox poll 100 ms, consumer limits). One copy of each original env file remains on the orchestrator, inventory and payment hosts as `.env.<service>.test7-orig`, which is harmless.
- The second orchestrator replica from Test 8 was removed; one orchestrator runs.
- No network fault (iptables, `tc`) is active.

## Cleanup still to do

1. **Delete the test data:** the disposable Neon branch (in the Neon console, or `neonctl branches delete <name>`). This is irreversible, so it was not done.
2. **Stop the instances** to stop the AWS charges: `scripts/cloud-cleanup.sh` lists the six instances (dry run), and `CONFIRM=yes scripts/cloud-cleanup.sh` stops them. Stopping keeps the disks; terminating them and the broker volume is a separate decision (`terraform plan -destroy`).
3. **Remove the two SSH-rule additions** to the security groups for your laptop's addresses when finished.

## Limits

1. The audit reads the data of one run (the soak). Each load run resets the data first, so earlier tests' data is no longer in the database; their audits were run after each test.
2. The audit checks the database invariants and the observability counters. It does not check the broker volume, backups or restores.
3. Run record not filled in (git commit, image tag, Neon branch).
