# Docker setup

## Local environment

The local stack starts PostgreSQL, runs all schema migrations, seeds the
inventory and payment schemas, and then starts the three APIs.

```bash
docker compose -f compose.local.yaml up --build --wait
```

Endpoints:

- Order API: `http://localhost:8080`
- Inventory API: `http://localhost:8081`
- Payment API: `http://localhost:8082`
- PostgreSQL: `localhost:5432` (`distri` / `distri`, database `distri`)

The migration and seed containers are expected to exit successfully. Their
completed state is what allows the API containers to start.

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
