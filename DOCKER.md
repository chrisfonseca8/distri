# Docker setup

## Local environment

The local stack starts PostgreSQL, runs all schema migrations, seeds the
inventory schema, and then starts the three APIs. Payment data is never seeded.

```bash
docker compose -f compose.local.yaml up --build --wait
```

Endpoints:

- Order API: `http://localhost:8080`
- Inventory API: `http://localhost:8081`
- Payment API: `http://localhost:8082`
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

Every correctness or load-test run starts with a fresh PostgreSQL volume:

```bash
docker compose -f compose.local.yaml down --volumes
docker compose -f compose.local.yaml up --build --wait
docker compose -f compose.local.yaml --profile loadtest build loadgen
docker compose -f compose.local.yaml --profile loadtest run --rm --no-deps loadgen
```

After collecting the result, remove the test database:

```bash
docker compose -f compose.local.yaml down --volumes
```

The loadgen binary is currently only a containerized stub. The next step is to
implement its checkout workload, completion polling, latency histograms, error
counts, and open-loop/closed-loop modes. `ORDER_URL` is already wired so that
the workload code will not need Docker-specific addressing.

## Local correctness suite

The phase-based black-box correctness suite lives in
`loadgen/correctnesstesting/local`. Its full test list and report format are
documented in `loadgen/README.md`. Run it against a freshly created local
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
`test-results/correctness-test/correctness-test.md` and
`test-results/correctness-test/correctness-test.json`. Each phase reports its purpose, assertions,
request/error counts, duration, and HTTP latency min/mean/p50/p95/p99/max.
Outcome-specific phases can be selected by rebuilding the fresh stack with
`GATEWAY_MODE=success`, `decline`, or `timeout`.

## Production environment

Production Compose does not run PostgreSQL and does not seed data. Supply
immutable service images and externally managed PostgreSQL URLs:

```bash
export ORDER_SERVICE_IMAGE=registry.example.com/distri/order-service:1.0.0
export INVENTORY_SERVICE_IMAGE=registry.example.com/distri/inventory-service:1.0.0
export PAYMENT_SERVICE_IMAGE=registry.example.com/distri/payment-service:1.0.0

export ORDER_DATABASE_URL='postgres://.../distri?sslmode=require&search_path=orders'
export INVENTORY_DATABASE_URL='postgres://.../distri?sslmode=require&search_path=inventory'
export PAYMENT_DATABASE_URL='postgres://.../distri?sslmode=require&search_path=payments'
```

Run migrations as a separate deployment step:

```bash
docker compose -f compose.prod.yaml --profile migrate run --rm inventory-migrate
docker compose -f compose.prod.yaml --profile migrate run --rm payment-migrate
docker compose -f compose.prod.yaml --profile migrate run --rm order-migrate
```

Then start the APIs:

```bash
docker compose -f compose.prod.yaml up -d --wait
```

Only the Order API is published to the host. Inventory and Payment stay on the
private Compose network and are reached by their service names.

The current payment implementation uses the simulator in every environment.
Set `GATEWAY_MODE` and `GATEWAY_LATENCY` explicitly for controlled deployments;
do not interpret simulator load-test results as real gateway capacity.
