# tests

Every kind of test in this project: what it covers, what it found, and the raw result files. Each folder has its own README (what is tested, what was done, verdict, limits) and one file per test. The folders mirror [`tools/`](../tools/README.md), which holds the programs that produced these results.

```
tests/
├── REPORT.md            one-page summary: verdicts, headline metrics, changes and impact, known limits
├── unit/                Go unit and integration tests, one file per module, one section per *_test.go
├── correctness/         the 22-phase suite, the oversell test, the final database audit
│   ├── phases/              one file per phase
│   └── data/                raw results
├── load-test/           baseline, client sweeps, open-loop rate sweep, bottleneck experiment, soak
│   └── data/                raw results
├── fault-injection/     service kills, broker restart, database drop, network delay, replicas, gateway decline and timeout, overload
│   └── data/                raw results
└── penetration/         security attacks: not performed
```

| Folder | Tests | Verdict | Produced by |
|---|---|---|---|
| [`unit/`](unit/README.md) | 292 Go test functions in 78 files | all pass against a Postgres container (470 passes with subtests, 0 skipped) | `go test`, [`tools/integration`](../tools/integration/run-integration.sh) |
| [`correctness/`](correctness/README.md) | 20 active phases × 3 gateway modes, oversell, final audit | PASS, with caveats | [`tools/correctness`](../tools/correctness) |
| [`load-test/`](load-test/README.md) | 6 load tests | PASS; limits documented | [`tools/load-test`](../tools/load-test) |
| [`fault-injection/`](fault-injection/README.md) | 11 failure cases | PASS (7 of 7 fault scenarios) | [`tools/fault-injection`](../tools/fault-injection/chaos.py) |
| [`penetration/`](penetration/README.md) | none | **not performed** | – |

## Reading guide

- Start with [`REPORT.md`](REPORT.md) for the headline numbers.
- To see **how** a number was produced, open the matching test file: it states the load, the settings, the result and the limits, and links to the raw data in the folder's `data/`.
- To re-run a test, use the table in [`tools/README.md`](../tools/README.md).

## Conventions

- Every cloud result used the **simulated payment gateway** (configured with a 5 s pay delay) on AWS with single replicas, on 1 November 2026.
- *Accepted/s* (HTTP 202) and *completed/s* (reached a final saga state) are always reported separately. Peak and sustained figures are labelled.
- A claim of improvement needs a before and after run; where none exists the result is labelled *measured* or *qualitative*.
- The `*_test.go` files stay next to the code they test, in each service.
