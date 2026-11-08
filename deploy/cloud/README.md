# First staging deployment

This package creates and runs the fixed first-staging topology described in
[`requirements.md`](requirements.md): four separate application-service EC2
hosts, one RabbitMQ EC2 host, one load-generator/simulator EC2 host, and a
disposable Neon branch capped at 2 CU. It is a single-AZ test environment, not
production HA.

Nothing in this directory applies infrastructure automatically. Review the plan
and current AWS pricing before running `terraform apply`.

## 1. Prerequisites

- AWS CLI credentials and Terraform 1.6+
- a private container repository and read-only pull credentials for the hosts
- a disposable Neon branch in the same AWS region, using its direct TLS URL
- an EC2 key pair if using the remote Docker metrics tunnel
- `DATABASE_TEST_URL` in the repository `.env` pointing only to that branch

If one empty Neon project hosts all four service databases, run database setup
with `USE_SINGLE_NEON_ADMIN_URL=yes`; this uses `ORCHESTRATOR_NEON_ADMIN_URL`
for every database while retaining separate databases and application roles.

## 2. Review and provision

```sh
cd deploy/cloud/terraform
cp terraform.tfvars.example terraform.tfvars
# Edit terraform.tfvars: region/AZ, your /32 admin CIDR, key name, budget email.
terraform init
terraform fmt -check
terraform validate
terraform plan -out=staging.tfplan
# Stop here and review the plan before any AWS creation.
terraform apply staging.tfplan
terraform output
```

Set `create_instance_profile = false` when the provisioning principal has EC2
permissions but cannot create IAM roles. In that mode SSM is unavailable and a
working `key_name` plus a narrow `admin_cidr` are required for administration.
The full topology needs six Elastic IPs. Set `create_elastic_ips = false` when
the regional EIP quota is lower; instances will still receive public addresses,
but those addresses can change after a stop/start cycle.

The defaults provision:

| Host | Type | Root gp3 | Additional storage |
|---|---|---:|---:|
| orchestrator | `c7i.large` (2 vCPU, 4 GiB) | 30 GiB | — |
| order | `c7i.large` (2 vCPU, 4 GiB) | 30 GiB | — |
| inventory | `c7i.large` (2 vCPU, 4 GiB) | 30 GiB | — |
| payment | `c7i.large` (2 vCPU, 4 GiB) | 30 GiB | — |
| broker | `m7i.large` (2 vCPU, 8 GiB) | 20 GiB | 50 GiB RabbitMQ gp3 |
| loadgen | `c7i.large` (2 vCPU, 4 GiB) | 40 GiB | — |

All volumes are encrypted gp3 at 3,000 IOPS and 125 MiB/s. The separate broker
volume has `prevent_destroy`; removing the stack requires an explicit retention
decision and Terraform change for that volume.

## 3. Build the immutable images

Six tags are used: order, inventory, payment, orchestrator, gateway simulator,
and load generator. The command is a dry run unless explicitly confirmed.

```sh
deploy/cloud/build-push.sh your-account/distri "$(git rev-parse --short HEAD)"
CONFIRM_PUSH=yes deploy/cloud/build-push.sh your-account/distri "$(git rev-parse --short HEAD)"
```

Never configure real Razorpay credentials for a load test. The payment service
uses the dedicated simulator host through its Razorpay-compatible API.

## 4. Generate host configuration

Use the private addresses shown by `terraform output`:

```sh
deploy/cloud/make-env.sh your-account/distri <image-tag> \
  <orchestrator-ip> <order-ip> <inventory-ip> <payment-ip> <broker-ip> <loadgen-ip>
```

This creates three mode-600, git-ignored files: `.env.cloud`, `.env.broker`, and
`.env.loadgen`. It generates shared broker/simulator secrets and does not print
them. `.env.cloud` is the shared application-host configuration; `.env.broker`
and `.env.loadgen` belong only on their corresponding hosts.

## 5. Copy deployment files

The bootstrap creates `/home/ec2-user/distri` on every host. Log in to the
private container repository on each host, then copy:

```text
each service host: compose.cloud.yaml, deploy/cloud/.env.cloud
broker:  deploy/cloud/compose.broker.yaml, deploy/cloud/.env.broker
loadgen: deploy/cloud/compose.loadgen.yaml, deploy/cloud/compose.simulator.yaml,
         deploy/cloud/.env.loadgen, tools/loadtesting/run-cases.sh, test-results/
```

Keep the same repository-relative paths under `~/distri`; the load result mount
depends on them. SSM Session Manager commands are included in Terraform output.

## 6. Start in dependency order

On the broker host:

```sh
cd ~/distri
docker compose --env-file deploy/cloud/.env.broker -f deploy/cloud/compose.broker.yaml pull
docker compose --env-file deploy/cloud/.env.broker -f deploy/cloud/compose.broker.yaml up -d --wait
```

On the load-generator host, start the simulator before the app:

```sh
cd ~/distri
docker compose --env-file deploy/cloud/.env.loadgen -f deploy/cloud/compose.simulator.yaml pull
docker compose --env-file deploy/cloud/.env.loadgen -f deploy/cloud/compose.simulator.yaml up -d --wait
```

On each service host, pull the same immutable image set. Run only that host's
migration and service. For example, use the corresponding pair below:

```sh
cd ~/distri
export SERVICE_COMPOSE='docker compose --env-file deploy/cloud/.env.cloud -f compose.cloud.yaml'
$SERVICE_COMPOSE pull

# Orchestrator host
$SERVICE_COMPOSE --profile migrate run --rm orchestrator-migrate
$SERVICE_COMPOSE up -d --wait orchestrator

# Order host
$SERVICE_COMPOSE --profile migrate run --rm order-migrate
$SERVICE_COMPOSE up -d --wait order-service

# Inventory host
$SERVICE_COMPOSE --profile migrate run --rm inventory-migrate
$SERVICE_COMPOSE --profile migrate run --rm inventory-seed
$SERVICE_COMPOSE up -d --wait inventory-service

# Payment host
$SERVICE_COMPOSE --profile migrate run --rm payment-migrate
$SERVICE_COMPOSE up -d --wait payment-service
```

## 7. Enable application-container sampling

The runner samples all four application containers through four SSH-forwarded
Unix sockets. On the load-generator host, create the directory and one tunnel
to each service host:

```sh
mkdir -p /home/ec2-user/sockets
rm -f /home/ec2-user/sockets/*.sock
ssh -f -o ExitOnForwardFailure=yes -o StreamLocalBindUnlink=yes -N -L /home/ec2-user/sockets/orchestrator.sock:/var/run/docker.sock ec2-user@<orchestrator-ip>
ssh -f -o ExitOnForwardFailure=yes -o StreamLocalBindUnlink=yes -N -L /home/ec2-user/sockets/order.sock:/var/run/docker.sock ec2-user@<order-ip>
ssh -f -o ExitOnForwardFailure=yes -o StreamLocalBindUnlink=yes -N -L /home/ec2-user/sockets/inventory.sock:/var/run/docker.sock ec2-user@<inventory-ip>
ssh -f -o ExitOnForwardFailure=yes -o StreamLocalBindUnlink=yes -N -L /home/ec2-user/sockets/payment.sock:/var/run/docker.sock ec2-user@<payment-ip>
```

Run it under a supervised systemd unit for long tests. Do not expose the Docker
TCP API: access to this socket is effectively root access to the app host.

## 8. Preflight and first test

Warm Neon with throwaway requests, then verify all hosts and configuration:

```sh
cd ~/distri
docker compose --env-file deploy/cloud/.env.loadgen -f deploy/cloud/compose.loadgen.yaml --profile loadtest config --quiet
curl -fsS http://<order-private-ip>:8080/health
curl -fsS http://<orchestrator-private-ip>:8083/readyz
curl -fsS http://127.0.0.1:9090/healthz
test -S /home/ec2-user/sockets/order.sock
test -S /home/ec2-user/sockets/inventory.sock
test -S /home/ec2-user/sockets/payment.sock
test -S /home/ec2-user/sockets/orchestrator.sock
```

Run C1 from the separate load-generator host:

```sh
docker compose --env-file deploy/cloud/.env.loadgen \
  -f deploy/cloud/compose.loadgen.yaml --profile loadtest run --rm loadtest
```

The runner has an unscoped database URL because it resets fixtures and performs
the audit. Its hostname allow-list must match the Neon host exactly. Run phase 22
after every correctness or load run:

```sh
PHASES=22 docker compose --env-file deploy/cloud/.env.loadgen \
  -f deploy/cloud/compose.loadgen.yaml --profile correctness run --rm correctness
```

Capture app, broker, loadgen, and Neon metrics with each result.

For the full correctness suite, keep payment-service on the project's
Razorpay-compatible gateway simulator; never configure real payment credentials
or a real gateway endpoint. Select deterministic success, decline, and timeout
behaviour by recreating `gateway-simulator` on the load-generator host with the
matching `SIM_*` scenario settings before each correctness run. The correctness
container mounts the forwarded Order and Payment Docker sockets so recovery
phases 8 and 16 can stop and restart their participant instead of reporting
`SKIP`.

Each mode requires a fresh disposable database. Set
`CORRECTNESS_RESET_DATA=true` only for this staging branch; the runner refuses the
reset unless the database hostname exactly matches `LOAD_TEST_ALLOWED_DB_HOST`.

## 9. Stop and cost control

Stop containers immediately after a session and stop or terminate EC2 instances.
Elastic IPs and EBS volumes can still incur cost while instances are stopped.
The optional Terraform budget sends forecasted 80% and actual 100% notifications;
it does not stop resources automatically.

Before `terraform destroy`, decide whether to snapshot/retain RabbitMQ data. The
50 GiB data volume is protected by `prevent_destroy` on purpose.
