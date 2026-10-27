# distri

A backend-only distributed checkout and payment system in Go, built as a learning project to see how concurrency, architecture and infrastructure change measurable performance and correctness.

## Services

| Directory | Role |
|---|---|
| `order-service` | Owns order data |
| `orchestrator-service` | Owns the checkout saga state machine |
| `inventory-service` | Stock reserve / confirm / release |
| `payment-service` | Payments via Razorpay Test Mode or the simulated gateway |
| `gateway-simulator` | Controllable fake payment gateway |
| `testing` | Go load generator, correctness tests, baseline runner |

Stack: Go, GORM, PostgreSQL, RabbitMQ, and Docker Compose.

## Run locally

The local Compose stack supplies disposable development credentials, runs each
schema migration, seeds inventory, and starts the four application services:

```bash
docker compose -f compose.local.yaml up --build --wait
```

Run the smoke load case from another terminal:

```bash
testing/loadtesting/run-cases.sh c1
```

Stop the stack without removing its local volumes:

```bash
docker compose -f compose.local.yaml down
```

See [`DOCKER.md`](DOCKER.md), [`testing/README.md`](testing/README.md), and
[`deploy/cloud/README.md`](deploy/cloud/README.md) for the full local, test, and
staging workflows. Cloud tests must use a disposable test database because the
load runner resets its fixture tables.

## Test the source

This repository contains six independent Go modules. From the repository root:

```bash
for module in gateway-simulator inventory-service orchestrator-service order-service payment-service testing; do
  (cd "$module" && go test ./...)
done
```

PostgreSQL-backed integration tests that require explicit opt-in may be skipped
by the normal unit-test command. The running-stack correctness suite is separate
and destructive; follow `testing/README.md` before invoking it.

## Configuration

- `.env.example` documents developer-supplied values. Real `.env*` files are ignored.
- Local development uses `compose.local.yaml`.
- `compose.prod.yaml` is a pre-orchestrator historical reference and is not the maintained deployment definition.
- Cloud staging is documented under `deploy/cloud/`; Terraform state, variable files, host inventories, and generated host environment files are ignored.
- Each service has its own `go.mod` and migration command under `cmd/migrate`.

## Notes

Authentication is currently out of scope. Never commit `.env` files, Terraform
state/variable files, cloud host inventories, database URLs, or private keys.
