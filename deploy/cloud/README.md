# Cloud load-test deployment (Docker Hub → one EC2 host, Neon test database)

Everything runs on one EC2 host: the three services and the load-test runner,
against the **test** Neon database only. The runner truncates tables and resets
inventory, so it refuses to start unless `LOAD_TEST_ALLOWED_DB_HOST` equals the
database host it was given.

## 1. Docker Hub (once)

1. Create a **private** repository, e.g. `ungoodcoder/distri`. One repository holds all
   four images as tags (`order-service-<tag>`, `inventory-service-<tag>`,
   `payment-service-<tag>`, `loadgen-<tag>`); the free plan allows one private
   repository.
2. Create a **read-only access token** for the EC2 host. Do not share it in chat.

## 2. On your machine

```sh
deploy/cloud/make-env.sh ungoodcoder/distri 2026-10-19      # writes deploy/cloud/.env.cloud (mode 600, git-ignored)
CONFIRM_PUSH=yes deploy/cloud/build-push.sh ungoodcoder/distri 2026-10-19
```

Without `CONFIRM_PUSH=yes` the push script only prints what it would push.

## 3. Security group (from the ports discussion)

Inbound 22/tcp from your IP only. Port 8080 only if you call the order API from
outside; the runner uses the private Docker network. Never open 8081 or 8082.
Outbound: the default (Neon on 5432, Docker Hub on 443).

## 4. EC2 host (x86_64, non-burstable, Docker Engine + Compose plugin)

```sh
docker login -u ungoodcoder                 # paste the read-only token
mkdir -p ~/distri/deploy/cloud ~/distri/testing/loadtesting ~/distri/test-results
```

From your machine, copy only what is needed (no source code):

```sh
scp -i <key>.pem compose.cloud.yaml                       ec2-user@<ip>:~/distri/
scp -i <key>.pem deploy/cloud/.env.cloud                  ec2-user@<ip>:~/distri/deploy/cloud/
scp -i <key>.pem testing/loadtesting/run-cases.sh         ec2-user@<ip>:~/distri/testing/loadtesting/
```

## 5. First run, on the host (in `~/distri`)

```sh
export DISTRI_ENV="--env-file deploy/cloud/.env.cloud"
docker compose -f compose.cloud.yaml $DISTRI_ENV pull
docker compose -f compose.cloud.yaml $DISTRI_ENV --profile migrate run --rm inventory-migrate
docker compose -f compose.cloud.yaml $DISTRI_ENV --profile migrate run --rm payment-migrate
docker compose -f compose.cloud.yaml $DISTRI_ENV --profile migrate run --rm order-migrate
docker compose -f compose.cloud.yaml $DISTRI_ENV --profile migrate run --rm inventory-seed
docker compose -f compose.cloud.yaml $DISTRI_ENV up -d --wait
```

Warm the Neon compute (it scales to zero), then run the cases with the same script
used locally:

```sh
export DISTRI_COMPOSE_FILE=compose.cloud.yaml DISTRI_ENV_FILE=deploy/cloud/.env.cloud
export LOADTEST_SERVICE=loadtest LOADTEST_PROFILE=loadtest
testing/loadtesting/run-cases.sh c10        # also c11, c12, c13 [latency...], c14 [latency] [size...]
```

Results are written to `~/distri/test-results/load-test/load-test.md` and `.json`;
copy them back with `scp`. Record the Neon database size before and after (the
report does), and stop or terminate the instance when finished.
