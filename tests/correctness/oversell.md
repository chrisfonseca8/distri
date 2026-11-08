# Correctness test — Oversell on the cloud stack

**Result:** PASS

## What is tested

That concurrent buyers can never be sold more units than exist. 500 clients compete for one product that has only 10 units. Exactly 10 must succeed and the other 490 must be refused with HTTP 409; stock must never go below zero.

## How it is run

Case `c5b-hot-product-sellout`: `LOAD_CLIENTS=500`, `LOAD_ORDERS=500`, `LOAD_HOT_STOCK=10`, `LOAD_MAX_CONNS=1000`, run from the load-generator host against the AWS staging stack with the gateway simulator in `success` mode. The unit-level counterpart is `TestConcurrentBuyersCannotOversellLastTenUnits` ([`oversell_integration_test.go`](../../inventory-service/pkg/services/oversell_integration_test.go)), 200 goroutines buying the last 10 units.

## Result

| Check | Result |
|---|---|
| Attempts offered | 500 |
| `HTTP 202` accepted | **10** |
| `HTTP 409` out of stock | **490** |
| Transport errors, 5xx, decode errors | 0 |
| Completed | 10 (10 paid, 0 cancelled, 0 pending at the drain) |
| Invariant "consumes exactly the available stock, rejects the remainder with 409" | **PASS** |
| Lock waiters, deadlocks | 0 |
| Checkout latency p50 / p95 / p99 | 465 / 546 / 552 ms |
| End-to-end latency for the 10 sold (p50 / p95) | 7.4 s / 13.0 s |
| Phase-22 database audit | **PASS** (1 of 1) |

Only 10 checkouts needed the full saga; the other 490 ended at the inventory step. The end-to-end figures of 7 to 13 s come from 500 clients hitting a few services at once, not from the payment path.

## Limits

- One sample: no repeat, so no variance figure.
- Simulated gateway; one product and one load shape. Multi-product and multi-item contention is covered by correctness phases [17](phases/17-concurrent-limited-stock-checkout.md) and [18](phases/18-multi-item-deadlock-resistance.md).
- The other case run by the same command (500 clients on one hot product, throughput) refused 151 requests with 503; see [failure case 11](../failure-cases/11-overload-hot-product.md).

**Raw data:** [`load-test.md`](data/oversell/load-test.md), [`load-test.json`](data/oversell/load-test.json), [`phase22-audit.md`](data/oversell/phase22-audit.md), [`audit.json`](data/oversell/audit.json)
