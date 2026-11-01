# Run record — copy this file once per run

Fill it in before the run starts and finish it right after. A result without this record does not count.

| Field | Value |
|---|---|
| Step number and name (from `final/README.md`) | |
| Git commit (`git rev-parse HEAD`) and whether the tree was clean | |
| Image tag / build deployed | |
| Instance types and region (services, load runner, broker) | |
| Neon branch name and compute size | |
| `DATABASE_TEST_URL` host (never the password) | |
| Config that differs from defaults (env vars: worker counts, poll intervals, pool sizes, gateway mode and latency, payment window and grace) | |
| Load shape (closed-loop clients or open-loop rate, warm-up, measurement, drain) | |
| Fault injected, and when (seconds into the run) | |
| Start time (UTC) | |
| End time (UTC) | |
| Sagas in flight when the fault landed | |
| Accepted / completed per second (**separately**) | |
| p50 / p95 / p99 end-to-end latency | |
| Final saga states (COMPLETED / COMPENSATED / other) | |
| Phase-22 audit result (each invariant) | |
| Dead-letter queue before / after | |
| Raw JSON path in the repo | |
| Result: PASS / FAIL, and the reason if FAIL | |
| Anything unexpected | |
