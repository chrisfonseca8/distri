# Correctness test — Final database audit

**Result:** PASS

## What is tested

After all load and fault tests, a cross-schema audit (correctness phase 22) checks that the database contains no contradictory or duplicated state, and an observability audit checks counters (stuck sagas, dead outbox rows, unknown payments).

## How it is run

The phase-22 audit ran against the staging database after the soak, so it covers the data of the last load run; each load run resets the data first, so earlier runs were audited immediately after they finished (their audits are in the other tests' data folders). The 8 invariant queries used by the fault harness also ran before and after every fault scenario.

## Result

| Check | Result |
|---|---|
| Phase-22 audit, all assertions | **PASS**, 0 violations in each |
| Observability audit | PASS, no violations, no alerts firing at the time |
| Stuck sagas, dead outbox rows, dead-queue messages, unknown or unconfirmed payments, incomplete refunds, stock violations | all 0 |
| Sagas | 12,736, all COMPLETED |
| Orders / payments / reservations | 12,736 PAID / 12,736 SUCCEEDED / 12,736 CONFIRMED |
| Minimum free stock; products with negative stock | 999,745; none |
| Dead queue `distri.commands.dead` | 0 messages |

Invariants checked: stock counters within bounds; one reservation per order; one payment per order; no duplicate product line per order; paid orders have a successful payment; cancelled orders have a failed payment; paid reservations are confirmed; cancelled reservations are released.

## Limits

- The audit reads the data of one run (the soak).
- It checks database invariants and observability counters; it does not check the broker volume, backups or restores.
- The audit's first assertion prints "timed out waiting for the worker backlog" next to PASS. The same text appears in earlier passing audits and looks like a fixed label; the audit code was not read to confirm.

**Raw data:** [`phase22-audit.md`](data/final-audit/phase22-audit.md), [`audit.json`](data/final-audit/audit.json), [`database-state.txt`](data/final-audit/database-state.txt)
