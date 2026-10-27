# First staging deployment requirements

This is the fixed starting topology for the first cloud test. It is deliberately
single-AZ and not highly available. It is a measurement environment, not a
production topology.

## Compute

| Host | Default EC2 type | Allocation | Workload |
|---|---|---|---|
| `distri-staging-orchestrator` | `c7i.large` | 2 vCPU, 4 GiB | Orchestrator only |
| `distri-staging-order` | `c7i.large` | 2 vCPU, 4 GiB | Order service only |
| `distri-staging-inventory` | `c7i.large` | 2 vCPU, 4 GiB | Inventory service only |
| `distri-staging-payment` | `c7i.large` | 2 vCPU, 4 GiB | Payment service and its workers only |
| `distri-staging-broker` | `m7i.large` | 2 vCPU, 8 GiB | RabbitMQ only |
| `distri-staging-loadgen` | `c7i.large` | 2 vCPU, 4 GiB | Load generator and test coordinator |

The load generator is intentionally off the application-service hosts. All six
EC2 instances use x86_64 Amazon Linux 2023 and Docker Compose. Detailed EC2 monitoring
is enabled. The Terraform variables allow every instance type to be changed.

## Storage

| Host | Volume | Configuration |
|---|---|---|
| Each application service | Root | 30 GiB encrypted gp3, 3,000 IOPS, 125 MiB/s |
| Broker | Root | 20 GiB encrypted gp3, 3,000 IOPS, 125 MiB/s |
| Broker | RabbitMQ data | 50 GiB encrypted gp3, 3,000 IOPS, 125 MiB/s |
| Load generator | Root/results | 40 GiB encrypted gp3, 3,000 IOPS, 125 MiB/s |

RabbitMQ data is mounted at `/data/rabbitmq` from its dedicated EBS volume. The
volume is retained when the broker instance is terminated unless the operator
explicitly deletes it. Set a snapshot policy before retaining staging data for
more than a test session.

## Database

- Use a disposable Neon branch in the same region as EC2 (`us-east-1` by default).
- Autoscaling maximum: 2 CU. Warm it before measurements.
- Application services use direct, TLS-required connection strings.
- Initial pool budget: 10 open and 10 idle connections per service, 40 total.
- The runner gets an unscoped URL to the same disposable database solely for
  fixture reset and audit. `LOAD_TEST_ALLOWED_DB_HOST` must match exactly.
- Record branch size and compute settings before and after every run.

## Network

Terraform creates one VPC and one public subnet in one availability zone. Public
addresses provide outbound access to Docker Hub and Neon without a NAT Gateway;
security groups still allow application traffic only by source security group.
This saves staging cost but is not the recommended production network layout.

| Destination | Port | Allowed source |
|---|---:|---|
| Service APIs | 8080-8083 | Load-generator SG and service SG |
| Service-host SSH | 22 | Load-generator SG and optional administrator CIDR |
| RabbitMQ AMQP | 5672 | Application SG |
| RabbitMQ management | 15672 | Load-generator SG and optional administrator CIDR |
| Simulator | 9090 | Application SG |
| Host SSH | 22 | Optional administrator CIDR |

All instances allow outbound traffic. Prefer Systems Manager Session Manager for
shell access. Supplying an EC2 key pair is optional but required for the SSH
Docker-socket tunnel used by the current remote container sampler.

## Fixed application settings for the first comparison

- `WEBHOOK_PROCESS_BATCH=100`
- `WEBHOOK_PROCESS_INTERVAL=1s`
- `WORKER_CONCURRENCY=4`
- `ORDER_OUTBOX_CONCURRENCY=4`
- `ORCHESTRATOR_OUTBOX_CONCURRENCY=4`
- order and orchestrator outbox poll interval: `100ms`
- RabbitMQ consumer pools: minimum 1; maximum 4 for participant services and
  maximum 30 for the orchestrator reply pool
- DB pools: 10 open / 10 idle per service

Change one setting per later experiment and record the exact environment.

## Before applying

1. Create the disposable Neon branch and set its maximum to 2 CU.
2. Create a private Docker Hub repository and a read-only pull token.
3. Create an EC2 key pair if using the remote Docker-socket SSH tunnel.
4. Copy `terraform.tfvars.example` to `terraform.tfvars` and fill the administrator
   CIDR, key name, budget email, and region/AZ if different.
5. Run `terraform plan`; review instance count, public IPv4 addresses, EBS volumes,
   and the optional AWS Budget before applying.
6. Confirm current prices in the AWS calculator. No price is encoded as a promise.
