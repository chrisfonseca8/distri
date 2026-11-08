# tools

Everything used to test, load and break the system. All commands are run **from the repository root** unless noted. Output goes to `test-results/`, which git ignores; the results that matter are summarised in [`../tests/REPORT.md`](../tests/REPORT.md).

```
tools/
├── load-test/          measure throughput and latency
│   ├── run-cases.sh        entry point: load cases c1 ... c14
│   ├── generator/          the load-generator image entrypoint
│   ├── runner/             the Go program that runs the cases and writes the report
│   ├── sweeps/             worker-count sweeps and follow-up load runs
│   └── README.md           cases, metrics and how to read them
├── correctness/        the 22-phase invariant suite (one Go file per phase)
│   └── README.md           what each phase checks, how to run it
├── fault-injection/    kill services, restart the broker, drop the database, delay the network
│   └── chaos.py
├── baseline/           low-rate sanity run of the whole path
├── cloud/              AWS staging runners: drive cloud tests, change settings, add a replica, stop the instances
├── integration/        run every Go test, including those that need Postgres
│   └── run-integration.sh
├── diagnostics/        one-off analysis programs (queue-wait report, idempotency storm, ...)
├── Dockerfile          builds the load-test and correctness binaries into one image
└── go.mod              one Go module for all of the Go programs above
```

## I want to ...

| Goal | Command |
|---|---|
| Run the Go tests, including the database-backed ones | `tools/integration/run-integration.sh` (needs a throwaway `*_test` Postgres; see the script header) |
| Smoke-test the local stack under load | `docker compose -f compose.local.yaml up --build --wait` then `tools/load-test/run-cases.sh c1` |
| Run another load case (baseline, client sweep, rate sweep, contention, ...) | `tools/load-test/run-cases.sh c2` ... `c14`; the case list is in [`load-test/README.md`](load-test/README.md) |
| Run the correctness suite | see [`correctness/README.md`](correctness/README.md) (`docker compose ... --profile correctness run --rm local-correctness`) |
| Inject faults under load | `python3 tools/fault-injection/chaos.py` (local scenarios) or `chaos.py preflight` / `chaos.py heal` in the cloud; scenarios are listed in the script header |
| Sweep payment worker concurrency | `tools/load-test/sweeps/worker_sweep.sh` |
| Run the cloud versions of these | the runners in [`cloud/`](cloud) and [`../deploy/cloud/README.md`](../deploy/cloud/README.md) |

## Notes

- The load-test and correctness programs are built into one image (`tools/Dockerfile`) with the binaries `loadgen`, `local-loadtest` and `local-correctness`; `compose.local.yaml` and `deploy/cloud/compose.loadgen.yaml` pick the one they need.
- The Go tests for these programs live next to their code (for example `correctness/*_test.go`).
- The correctness suite and the cloud load runs are **destructive** (they reset fixtures); run them only against disposable databases.
