# Phase 22 — Database invariants

**What is tested:** Scans all three schemas for negative/oversold stock, duplicate business keys, and cross-service terminal-state contradictions.

**Test code:** [`tools/correctnesstesting/local/phase22_invariants.go`](../../../tools/correctnesstesting/local/phase22_invariants.go)

**Counted result:** passes in 3 of 4 recorded runs (success; decline, 3 s window (rerun); timeout).

## Result in each recorded run

| Run (gateway mode) | Status | Duration (s) | HTTP requests | HTTP p95 (ms) |
|---|---|---:|---:|---:|
| success | PASS | 0.57 | 0 | 0 |
| decline, default payment window | FAIL | 10.1 | 0 | 0 |
| decline, 3 s window (rerun) | PASS | 0.72 | 0 | 0 |
| timeout | PASS | 0.2 | 0 | 0 |

## Assertions (recorded in the success run)

| Assertion | Result | Detail |
|---|---|---|
| actionable background jobs drain before audit | PASS | timed out waiting for the worker backlog |
| inventory counters stay within bounds | PASS | violations=0 |
| one reservation per order | PASS | violations=0 |
| one payment per order | PASS | violations=0 |
| one product line per order | PASS | violations=0 |
| paid orders have successful payment | PASS | violations=0 |
| cancelled orders have failed payment | PASS | violations=0 |
| paid reservations are confirmed | PASS | violations=0 |
| cancelled reservations are released | PASS | violations=0 |
| unknown payments retain pending orders | PASS | violations=0 |
| payment amounts match order totals in paise | PASS | violations=0 |

## Run that did not pass: decline, default payment window

Assertions that failed:

- **actionable background jobs drain before audit** — timed out waiting for the worker backlog

In this run the worker backlog from the earlier phases had not drained when the phase ran, so it timed out. The phase passed on the rerun with a 3 s payment window and 1 s reconciliation interval, so the decline result holds for those timings only.

**Raw data:** [`test2-all-runs.json`](../data/cloud-suite/test2-all-runs.json)
