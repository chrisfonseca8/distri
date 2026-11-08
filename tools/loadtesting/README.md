# Load testing

Load testing is separate from correctness testing. Correctness asks whether an
invariant holds; load testing asks where throughput stops scaling and why.

Local load-test code and its report contract live in [`local`](local). Results
will be written as one Markdown file and one JSON file:

```text
test-results/local/load/load-test.md
test-results/local/load/load-test.json
```

## Required client metrics

Every run must include:

| Metric | Definition |
|---|---|
| Offered rate | Workflows scheduled per second, whether or not they complete. |
| Accepted requests/s | Checkout requests returning any 2xx response divided by measurement duration. |
| Completed orders/s | Sagas reaching `COMPLETED` (order `PAID`, stock confirmed) or `COMPENSATED` (order `CANCELLED`, stock released) divided by measurement duration. |
| Checkout latency | min, mean, p50, p95, p99, and max for `POST /v1/checkouts` on the orchestrator. |
| Pay latency | Retired in OR9: the orchestrator takes the payment itself, so there is no pay call and this column is always empty. |
| End-to-end latency | Checkout start until the saga is `COMPLETED` or `COMPENSATED`. |
| Server completion latency | Persisted saga `updated_at - created_at`, excluding client observation delay. |
| Completion observation | Polling interval, or SSE attempts/terminal events/polling fallbacks. |
| Status mix | Separate counts for each HTTP status, business conflicts, 5xx, transport errors, and decode errors. |
| In-flight | Maximum and time-weighted mean active requests/workflows. |

`201` and replayed `200` are accepted. `409` is recorded as a business
conflict, not a server failure. `5xx`, transport failures, and invalid responses
are separate failure classes.

## Required service metrics

Each service must expose a load-test telemetry snapshot containing:

- inbound request count, error count, current/max in-flight, and handler
  latency percentiles;
- Order Service outbound inventory/payment call count, errors, and latency;
- goroutine count, heap/RSS-relevant memory values, GC cycles and pause time;
- `sql.DB.Stats()`: open, in-use, idle, `WaitCount`, and `WaitDuration`.

CPU and memory must also be sampled from the containers. A `pprof` CPU profile
is captured for diagnosis; it is not a replacement for container CPU usage.

## Required database metrics

Sample PostgreSQL throughout the measurement window:

- total and active connections;
- `idle in transaction` connections;
- sessions waiting on locks and the number of ungranted locks;
- `pg_stat_database.deadlocks` before and after;
- database size before and after;
- slow-query totals from `pg_stat_statements`, when the extension is available.

Every optional source has an explicit `available` flag and an explanatory
`unavailable_reason`. Missing telemetry must never be serialized as a zero.

## Run structure

Each experiment has four periods:

1. **Preflight** — verify services, telemetry endpoints, product fixtures,
   database access, and storage headroom.
2. **Warm-up** — establish connections and caches; exclude these samples.
3. **Measurement** — maintain the configured open-loop offered rate and collect
   all metrics at fixed sampling intervals.
4. **Drain** — stop offering new work, wait for accepted orders to become
   terminal, then take final service/database snapshots.

Use a fresh database for each experiment. Change only one capacity variable at
a time and give every run a stable label.

Set `LOAD_COMPLETION_MODE=sse` to observe completion through
`GET /v1/checkouts/{order_id}/events`. The default remains `poll` for historical
comparisons. An SSE run retries a broken stream twice, then records and uses a
polling fallback; the generated report exposes all three counts.

## Little's law check

For the stable measurement window, report:

```text
expected in-flight ≈ completed orders/s × mean end-to-end seconds
```

The observed mean in-flight and calculated value should be close. A widening
gap, rising backlog, or accepted rate materially above completed rate means the
system is accepting work faster than it can finish it.

## Implemented local cases

### C1 smoke

One client performs exactly 20 sequential checkout, pay, and terminal-order
workflows. It must offer, accept, and complete all 20 and pass the final
cross-service database invariants.

```sh
docker compose -f compose.local.yaml down --volumes
GATEWAY_MODE=success GATEWAY_LATENCY=200ms \
  docker compose -f compose.local.yaml up --build --wait
LOCAL_UID=$(id -u) LOCAL_GID=$(id -g) LOCAL_DOCKER_GID=$(stat -c %g /var/run/docker.sock) LOAD_CASE=C1 \
  docker compose -f compose.local.yaml --profile local-loadtest run --rm --no-deps local-loadtest
```

### C2 baseline

Ten closed-loop clients warm up for 10 seconds, then continuously execute the
same workflow for a measured 60 seconds.
The runner performs three independent repetitions, resetting transactional data
and inventory counters before each run. Products are selected round-robin over
all 50 seeded products. The aggregate includes mean, min, max, standard
deviation, and coefficient of variation for rates and p95 latencies.

```sh
LOCAL_UID=$(id -u) LOCAL_GID=$(id -g) LOCAL_DOCKER_GID=$(stat -c %g /var/run/docker.sock) LOAD_CASE=C2 \
  docker compose -f compose.local.yaml --profile local-loadtest run --rm --no-deps local-loadtest
```

The defaults are the defined test cases. `LOAD_ORDERS`, `LOAD_CLIENTS`,
`LOAD_DURATION`, and `LOAD_RUNS` may be overridden for a quick development
check, but an overridden run is not the canonical C1/C2 measurement.

### C3 client sweep

C3 repeats the warmed 60-second closed-loop workload at 1, 2, 5, 10, 20,
and 50 clients. Every point gets a fresh transactional fixture and its own
service, container, and PostgreSQL measurement window. The aggregate report
adds a concurrency/throughput/p99 table and identifies the first knee candidate
where the next concurrency step adds less than 10% completed throughput.

```sh
LOCAL_UID=$(id -u) LOCAL_GID=$(id -g) LOCAL_DOCKER_GID=$(stat -c %g /var/run/docker.sock) LOAD_CASE=C3 \
  docker compose -f compose.local.yaml --profile local-loadtest run --rm --no-deps local-loadtest
```

### C4 rate sweep

C4 is open loop. It offers 10, 20, 40, and 80 complete workflows per second
for 30 measured seconds per step after a 10-second warm-up. Scheduling does not
wait for earlier workflows. The runner drains scheduled work, records the
accepted/completed gap, errors, latency, and in-flight growth, and identifies
the first point completing less than 95% of the offered rate or producing
errors as a saturation candidate.

```sh
LOCAL_UID=$(id -u) LOCAL_GID=$(id -g) LOCAL_DOCKER_GID=$(stat -c %g /var/run/docker.sock) LOAD_CASE=C4 \
  docker compose -f compose.local.yaml --profile local-loadtest run --rm --no-deps local-loadtest
```

### C5–C7 contention wave

- C5a runs 50 closed-loop clients for 60 seconds against one product with
  ample stock.
- C5b submits exactly 2,000 attempts with 50 clients against 1,000 units. It
  fails unless exactly 1,000 complete and exactly 1,000 receive HTTP 409.
- C6 runs 50 clients for 60 seconds with an exact 80/20 request distribution:
  five hot products receive 80% and the other 45 receive 20%.
- C7 runs 50 clients for 60 seconds with deterministic pseudo-random two- and
  three-item carts and line ordering over the five-product hot set. Any new
  PostgreSQL deadlock fails the run.

```sh
LOCAL_UID=$(id -u) LOCAL_GID=$(id -g) LOCAL_DOCKER_GID=$(stat -c %g /var/run/docker.sock) LOAD_CASE=C5 \
  docker compose -f compose.local.yaml --profile local-loadtest run --rm --no-deps local-loadtest
LOCAL_UID=$(id -u) LOCAL_GID=$(id -g) LOCAL_DOCKER_GID=$(stat -c %g /var/run/docker.sock) LOAD_CASE=C6 \
  docker compose -f compose.local.yaml --profile local-loadtest run --rm --no-deps local-loadtest
LOCAL_UID=$(id -u) LOCAL_GID=$(id -g) LOCAL_DOCKER_GID=$(stat -c %g /var/run/docker.sock) LOAD_CASE=C7 \
  docker compose -f compose.local.yaml --profile local-loadtest run --rm --no-deps local-loadtest
```

### C9–C14 behaviour, latency and pool cases

| Case | What it does | Needs |
|---|---|---|
| C9 | 20% of workflows send a duplicate checkout with the same key | default stack |
| C10 | retired in OR9 (it sent two simultaneous `/pay` calls; that endpoint no longer exists) | — |
| C11 | every payment is declined; checks the release path | payment service in `decline` mode |
| C12 | 60/30/10 success/decline/timeout; counts orders left pending and the stock they hold | payment service in `random` mode |
| C13 | gateway latency 0 / 200 / 1000 / 2000 ms (stops at 2 s: 3 s hits the payment service's own gateway timeout) | payment service restarted per latency |
| C14 | `MaxOpenConns` = `MaxIdleConns` = 2 / 5 / 10 / 20 in all three services; optional gateway latency | all three services restarted per size |

The runner only checks its own `GATEWAY_MODE` variable, not the mode the running
payment service was started with. `run-cases.sh` therefore restarts the services
with the right settings before every case:

```sh
testing/loadtesting/run-cases.sh c10
testing/loadtesting/run-cases.sh c11
testing/loadtesting/run-cases.sh c12
testing/loadtesting/run-cases.sh c13            # all four latencies, or e.g.: c13 0ms
testing/loadtesting/run-cases.sh c14 200ms      # pool sweep at 200 ms; add sizes, e.g.: c14 0ms 10
testing/loadtesting/run-cases.sh restore        # back to success, 200 ms, pool 10 open / 5 idle
```

Run `restore` afterwards. The results land in `test-results/local/load/load-test.md`
with summary tables for C9–C12, C13 and C14. Everything in that file is written by
the runner. When a case is run again, the previous run is kept in a generated
"Repeat history" table with its mean, spread and coefficient of variation.

## Telemetry collection

The local stack enables a test-only endpoint in each service. Before every
measured window the runner resets it, then records inbound counts, errors,
maximum in-flight handlers, latency percentiles, goroutines, heap allocation,
GC activity, and `sql.DB.Stats()` including `WaitCount` and `WaitDuration`.

The runner samples Docker CPU and working-set memory for Order, Inventory,
Payment, and PostgreSQL through the Docker Unix socket. The socket group ID is
passed with `LOCAL_DOCKER_GID`; telemetry failure is a preflight/run failure,
not a silent zero. PostgreSQL activity, locks, deadlocks, and database size are
sampled separately. `pg_stat_statements` is collected when installed.

The report config also records the logical CPU count and memory visible to the
load-generator container, gateway mode, and configured simulator latency.
