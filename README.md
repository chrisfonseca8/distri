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

Stack: Go, GORM, PostgreSQL (schema per service), RabbitMQ, Kafka, Docker Compose.

## Running

- Copy `.env.example` to `.env` and fill in your database URL.
- Compose files: `compose.local.yaml` (local stack), `compose.simulator.yaml`, `compose.cloud.yaml`, `compose.prod.yaml`. See `DOCKER.md` and `deploy/cloud/README.md`.
- Each service has its own `go.mod`; migrations run via `go run ./cmd/migrate` in the service directory.

## Notes

No auth (out of scope). Secrets live in git-ignored `.env` files and `*.pem` keys; do not commit them.
