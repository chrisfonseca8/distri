# Tests

Every kind of test in this project, what it covers, and what it found. Each folder starts with a README that says what is being tested and what has been done; every individual test has its own file.

| Folder | What it tests | Verdict |
|---|---|---|
| [`unit/`](unit/README.md) | Go unit tests and database-backed integration tests, one section per `*_test.go` file | see folder |
| [`correctness/`](correctness/README.md) | The 22-phase suite against the live stack, the oversell test, the final database audit | PASS, with caveats |
| [`load/`](load/README.md) | Baseline, client sweeps, open-loop rate sweep, bottleneck experiment, soak | PASS; limits documented |
| [`failure-cases/`](failure-cases/README.md) | Service kills, broker restart, database drop, network delay, replicas, gateway decline and timeout, overload | PASS (7 of 7 fault scenarios) |
| [`penetration/`](penetration/README.md) | Security attacks | **not performed** |

[`REPORT.md`](REPORT.md) is the one-page summary with the headline metrics and the changes-and-impact table.

All cloud runs used the **simulated payment gateway** and single replicas on AWS (1 November 2026). The `*_test.go` files themselves stay next to the code they test, in each service; the `unit/` folder documents them.
