# Docker setup

## Local environment

The local stack starts PostgreSQL and RabbitMQ, runs all schema migrations,
seeds inventory, and then starts the order, inventory, payment, and orchestrator
services. Payment data is never seeded.

```bash
docker compose -f compose.local.yaml up --build --wait
```

Endpoints:

- Order API: `http://localhost:8080`
- Inventory API: `http://localhost:8081`
- Payment API: `http://localhost:8082`
- Orchestrator API: `http://localhost:8083`
- PostgreSQL: `localhost:5432` (`distri` / `distri`, database `distri`)

The migration containers and the inventory seed container are expected to exit
successfully. Their completed state is what allows the API containers to start.

Stop the stack while retaining local data:

```bash
docker compose -f compose.local.yaml down
```

Reset all local database data, then recreate and reseed it:

```bash
docker compose -f compose.local.yaml down --volumes
docker compose -f compose.local.yaml up --build --wait
```

The simulator can be changed for a run without editing Compose:

```bash
GATEWAY_MODE=decline GATEWAY_LATENCY=500ms \
  docker compose -f compose.local.yaml up --build --wait
```

Supported modes are `success`, `decline`, `timeout`, and `random`.

## Local load-test environment

The load generator is an opt-in Compose profile and reaches the Order Service
through the private Compose network at `http://order-service:8080`. It is not
started during normal local development.

The load generator is an opt-in Compose profile. The runner resets its own
fixture tables before every measured run, so use only this disposable local
database:

```bash
docker compose -f compose.local.yaml up --build --wait
tools/loadtesting/run-cases.sh c1
```

After collecting the result, remove the test database:

```bash
docker compose -f compose.local.yaml down --volumes
```

The runner writes Markdown and JSON reports under `test-results/`. See
`tools/loadtesting/README.md` for the case catalogue and metric definitions.

## Local correctness suite

The phase-based black-box correctness suite lives in
`tools/correctnesstesting/local`. Its full test list and report format are
documented in `tools/README.md`. Run it against a freshly created local
database:

```bash
docker compose -f compose.local.yaml down --volumes
docker compose -f compose.local.yaml up --build --wait
docker compose -f compose.local.yaml --profile correctness build local-correctness
LOCAL_UID=$(id -u) LOCAL_GID=$(id -g) \
  docker compose -f compose.local.yaml --profile correctness \
  run --rm --no-deps local-correctness
docker compose -f compose.local.yaml down --volumes
```

Markdown and JSON results are written to
`test-results/local/correctness/correctness-test.md` and
`test-results/local/correctness/correctness-test.json`. Each phase reports its purpose, assertions,
request/error counts, duration, and HTTP latency min/mean/p50/p95/p99/max.
Outcome-specific phases can be selected by rebuilding the fresh stack with
`GATEWAY_MODE=success`, `decline`, or `timeout`.

## Deployment Compose files

The multi-host staging definition is `compose.cloud.yaml` together with the files
under `deploy/cloud/`. Follow `deploy/cloud/README.md`, supply immutable images,
and use only externally managed test databases for load testing.

For example, cloud service images are built with:

```bash
deploy/cloud/build-push.sh your-account/distri "$(git rev-parse --short HEAD)"
```

The command is a dry run unless `CONFIRM_PUSH=yes` is explicitly supplied.
Never interpret gateway-simulator measurements as real payment-provider
capacity.
