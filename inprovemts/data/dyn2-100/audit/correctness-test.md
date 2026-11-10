# Correctness test results

Updated: 2026-11-09 19:32:10Z

## Run — cloud-staging

- Run ID: `20261108T194151.485344067Z`
- Started: 2026-11-08 19:41:51Z
- Finished: 2026-11-08 19:41:51Z
- Gateway mode: `success`
- Summary: **1 passed, 0 failed, 0 skipped**

| Phase | Test | Result | Requests | Errors | p50 | p95 | p99 |
|---:|---|:---:|---:|---:|---:|---:|---:|
| 22 | Database invariants | PASS | 0 | 0 | 0.00 ms | 0.00 ms | 0.00 ms |

### Phase 22 — Database invariants: PASS

Scans all three schemas for negative/oversold stock, duplicate business keys, and cross-service terminal-state contradictions.

Duration 321.59 ms. HTTP latency: min 0.00, mean 0.00, p50 0.00, p95 0.00, p99 0.00, max 0.00 ms (0 requests, 0 transport/decode errors).

Assertions:

- **PASS** — actionable background jobs drain before audit: timed out waiting for the worker backlog
- **PASS** — inventory counters stay within bounds: violations=0
- **PASS** — one reservation per order: violations=0
- **PASS** — one payment per order: violations=0
- **PASS** — one product line per order: violations=0
- **PASS** — paid orders have successful payment: violations=0
- **PASS** — cancelled orders have failed payment: violations=0
- **PASS** — paid reservations are confirmed: violations=0
- **PASS** — cancelled reservations are released: violations=0
- **PASS** — unknown payments retain pending orders: violations=0
- **PASS** — payment amounts match order totals in paise: violations=0

---

## Run — inprovemts-dyn2-100

- Run ID: `20261109T193159.890937643Z`
- Started: 2026-11-09 19:31:59Z
- Finished: 2026-11-09 19:32:10Z
- Gateway mode: `success`
- Summary: **1 passed, 0 failed, 0 skipped**

| Phase | Test | Result | Requests | Errors | p50 | p95 | p99 |
|---:|---|:---:|---:|---:|---:|---:|---:|
| 22 | Database invariants | PASS | 0 | 0 | 0.00 ms | 0.00 ms | 0.00 ms |

### Phase 22 — Database invariants: PASS

Scans all three schemas for negative/oversold stock, duplicate business keys, and cross-service terminal-state contradictions.

Duration 10779.26 ms. HTTP latency: min 0.00, mean 0.00, p50 0.00, p95 0.00, p99 0.00, max 0.00 ms (0 requests, 0 transport/decode errors).

Assertions:

- **PASS** — actionable background jobs drain before audit: timed out waiting for the worker backlog
- **PASS** — inventory counters stay within bounds: violations=0
- **PASS** — one reservation per order: violations=0
- **PASS** — one payment per order: violations=0
- **PASS** — one product line per order: violations=0
- **PASS** — paid orders have successful payment: violations=0
- **PASS** — cancelled orders have failed payment: violations=0
- **PASS** — paid reservations are confirmed: violations=0
- **PASS** — cancelled reservations are released: violations=0
- **PASS** — unknown payments retain pending orders: violations=0
- **PASS** — payment amounts match order totals in paise: violations=0

---

