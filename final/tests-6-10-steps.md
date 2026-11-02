# Tests 6–10 — exact steps

Tests 1–5 are done (results in `final/results/test2` … `test5`). Everything below is prepared but **has not been run**. Per `final/README.md`, change **one variable per run** and keep the raw JSON.

Scripts used (all in `scripts/`):
- **Laptop:** `cloud-test.sh` (`new`, `push`, `ssh`, `fetch`), `cloud-cleanup.sh`.
- **Load generator:** `loadgen-load.sh` (load presets + audit), `cloud-set-env.sh` (change a service setting), `cloud-replica.sh` (second orchestrator), and `testing/chaos/chaos.py` (test 8 correctness scenario).

## Before every session

Laptop terminal 1 (leave running):
```sh
cd ~/Desktop/code/projects/distri
docker compose -f deploy/observability/compose.observability.yaml up -d
deploy/observability/scripts/obs-tunnels.sh start
```
Laptop terminal 2, once per session:
```sh
cd ~/Desktop/code/projects/distri
scripts/cloud-test.sh ip            # must print an IP address; if it prints an error, start the instance
scripts/cloud-test.sh push          # copies the scripts to the load generator
```
If `ssh`/`scp` times out, your laptop's public IP is not allowed. Add it (a duplicate-rule error just means it is already allowed; run `curl -s https://checkip.amazonaws.com` a few times, since your outgoing address changes):
```sh
MYIP=$(curl -s https://checkip.amazonaws.com)/32
for G in distri-staging-loadgen distri-staging-services distri-staging-broker; do
  ID=$(aws ec2 describe-security-groups --region ap-southeast-2 --filters Name=group-name,Values=$G --query 'SecurityGroups[0].GroupId' --output text)
  aws ec2 authorize-security-group-ingress --region ap-southeast-2 --group-id $ID --protocol tcp --port 22 --cidr $MYIP
done
```
Fill in a copy of `final/run-record-template.md` for every run (git commit, image tag, Neon branch, settings). Run **nothing else** against the stack while a test runs.

Each test below has the same shape: **(1)** `new` on the laptop, **(2)** `ssh` in and run the preset on the load generator, **(3)** `fetch` on the laptop.

---

## Test 6 — step sweep to find the limit (about 30 min)

Pass: you can state where completed/s stops growing, reporting **accepted and completed separately**, plus p95 and p99.

Laptop:
```sh
scripts/cloud-test.sh new test6
scripts/cloud-test.sh ssh
```
Load generator (type the run id printed by `new`; it starts with `cloud-test6-`):
```sh
cd ~/distri
RUN=<RUN>
scripts/loadgen-load.sh test6-clients $RUN          # C3 closed loop: 1,2,5,10,20,50,100,200 clients
```
Then, as a second view with an open-loop offered rate (a fresh run id each time: `new test6` again, `ssh`, `RUN=<new RUN>`):
```sh
scripts/loadgen-load.sh test6-rate $RUN             # C4 open loop: 1,2,4,8,16,32 requests per second
```
Laptop: `scripts/cloud-test.sh fetch` (uses the last run id) and read `final/results/<RUN>/load-test.md`.

What to read: the table of one row per step. The limit is where completed/s flattens or the end-to-end latency climbs without throughput following. Baseline for reference: 1.55 completed/s at 10 clients, with almost no CPU in use, so expect it to scale with clients for a while. If throughput stops growing, the next things to read are the CPU and DB-wait columns for the service that saturates.

Tune the sweep if needed: `STEPS=10,20,40,80,160,320 scripts/loadgen-load.sh test6-clients $RUN` or `RATES=...` (integers).

## Test 7 — bottleneck experiment (1.5–2 h)

Pass: you can state which limit moved the number. Choose the operating point from test 6: a client count on the rising part of the curve, near but below the knee. Use the **same** `CLIENTS` for every run below.

For each experiment the sequence is: `new test7-<name>` (laptop) → `ssh` → change **one** setting → run the preset (3 repeats are built in) → reset the setting → `fetch`.

Load generator, baseline first (no change):
```sh
CLIENTS=<N> LABEL=cloud-test7-baseline scripts/loadgen-load.sh test7 $RUN
```
Then one variable at a time, in this order (README: workers, then poll intervals, then DB pool). Each block is one experiment with its own run id. Always `reset` afterwards.

| # | Variable | Change | Command to change |
|---|---|---|---|
| 1 | payment workers | `WORKER_CONCURRENCY` 4 → 8 | `scripts/cloud-set-env.sh set payment-service WORKER_CONCURRENCY=8` |
| 2 | payment consumers | `PAYMENT_CONSUMER_MAX_CONCURRENCY` 4 → 8 | `scripts/cloud-set-env.sh set payment-service PAYMENT_CONSUMER_MAX_CONCURRENCY=8` |
| 3 | inventory consumers | `INVENTORY_CONSUMER_MAX_CONCURRENCY` 4 → 8 | `scripts/cloud-set-env.sh set inventory-service INVENTORY_CONSUMER_MAX_CONCURRENCY=8` |
| 4 | orchestrator outbox poll | `ORCHESTRATOR_OUTBOX_POLL_INTERVAL` 100ms → 20ms | `scripts/cloud-set-env.sh set orchestrator ORCHESTRATOR_OUTBOX_POLL_INTERVAL=20ms` |
| 5 | order outbox poll | `ORDER_OUTBOX_POLL_INTERVAL` 100ms → 20ms | `scripts/cloud-set-env.sh set order-service ORDER_OUTBOX_POLL_INTERVAL=20ms` |
| 6 | orchestrator DB pool | `DB_MAX_OPEN_CONNS` 10 → 20 | `scripts/cloud-set-env.sh set orchestrator DB_MAX_OPEN_CONNS=20` |

One experiment, end to end (example: row 1). Laptop first:
```sh
scripts/cloud-test.sh new test7-payworkers
scripts/cloud-test.sh ssh
```
Load generator:
```sh
cd ~/distri
RUN=<RUN>
scripts/cloud-set-env.sh set payment-service WORKER_CONCURRENCY=8
CLIENTS=<N> LABEL=cloud-test7-payment-workers-8 scripts/loadgen-load.sh test7 $RUN
scripts/cloud-set-env.sh reset payment-service
exit
```
Laptop: `scripts/cloud-test.sh fetch`.

`scripts/cloud-set-env.sh show <service>` lists the tuning settings currently overridden on that host (never secrets). The consumer prefetch (10) and the starting consumer concurrency (1) are hard-coded in `compose.cloud.yaml`, so changing them needs a compose edit, not this script.

Read the result as: baseline mean ± spread of completed/s over 3 runs versus the changed run. A change counts only if it is clearly larger than the spread between repeats (baseline CV was 4% for test 5). Locally, raising payment `WORKER_CONCURRENCY` from 8 to 16 did not help, so do not assume more workers will.

## Test 8 — replica test (30–40 min)

Pass: all sagas complete, no duplicate effects, throughput improves **or** you can explain why not.

**8a. Correctness with two replicas.** Load generator, in a fresh run (`new test8`, `ssh`):
```sh
cd ~/distri
RUN=<RUN>
CHAOS_TARGET=cloud CHAOS_DIRECT=1 HOSTS_ENV=$PWD/deploy/cloud/hosts.loadgen.env CHAOS_RUN_ID=$RUN ORCH_URL=http://10.40.1.10:8083 \
  python3 testing/chaos/chaos.py two-live-orchestrator-replicas
```
(`deploy/cloud/hosts.loadgen.env` is created by `scripts/loadgen-test3.sh` on its first run, which has already happened. It starts the second replica, sends a burst, requires every accepted saga to finish with no duplicates and the second replica to have completed saga steps itself, and removes the replica.)

**8b. Throughput with one replica versus two.** Two separate runs with the same `CLIENTS` as test 7 and otherwise identical settings.

One replica. Laptop: `scripts/cloud-test.sh new test8-1replica`, then `ssh`. Load generator:
```sh
cd ~/distri
RUN=<RUN>
CLIENTS=<N> LABEL=cloud-test8-1-replica scripts/loadgen-load.sh test7 $RUN
exit
```
Laptop: `scripts/cloud-test.sh fetch`.

Two replicas. Laptop: `scripts/cloud-test.sh new test8-2replicas`, then `ssh`. Load generator:
```sh
cd ~/distri
RUN=<RUN>
scripts/cloud-replica.sh start
CLIENTS=<N> LABEL=cloud-test8-2-replicas scripts/loadgen-load.sh test7 $RUN
scripts/cloud-replica.sh stop
exit
```
The load always goes to the first replica's port 8083; the second replica takes a share of the queue consumers and outbox/sweeper work. If throughput does not improve, the explanation to check is that the limit is elsewhere (another service or a database), not the orchestrator.

Laptop: `scripts/cloud-test.sh fetch` after 8a as well. Check `scripts/cloud-replica.sh status` afterwards to make sure no replica is left running.

## Test 9 — soak (1–1.25 h)

Pass: latency does not drift, database size growth matches what the retention jobs allow, the outbox backlog returns to zero.

Pick `CLIENTS` at about 70% of the limit found in test 6. Laptop: `scripts/cloud-test.sh new test9`, then `ssh`. Load generator:
```sh
cd ~/distri
RUN=<RUN>
CLIENTS=<N> DURATION=45m nohup scripts/loadgen-load.sh test9 $RUN > soak.log 2>&1 &
tail -f soak.log
```
Ctrl-C stops only the `tail`. Watch Grafana (dashboards "01 System overview", "03 Saga flow", "05 RabbitMQ and outbox", "06 Postgres and infra"), set to the run id. Check at the start, middle and end that p95 latency is flat and the outbox backlog returns to zero. The runner's report gives database bytes before and after.

Laptop: when `soak.log` shows `== done`, run `scripts/cloud-test.sh fetch`.

## Test 10 — final audit and cleanup (20 min)

Pass: audit clean, test data removed, no instance still running.

1. Final audit. Laptop: `scripts/cloud-test.sh new test10`, then `ssh`. Load generator:
   ```sh
   cd ~/distri
   scripts/loadgen-load.sh audit <RUN>
   ```
   Laptop: `scripts/cloud-test.sh fetch`.
2. Remove the test data: delete the disposable Neon branch in the Neon console (or `neonctl branches delete <name>`). This script does not do it, because deleting a branch cannot be undone.
3. Check what would be stopped, then stop everything:
   ```sh
   scripts/cloud-cleanup.sh                  # dry run: lists the six instances and their state
   CONFIRM=yes scripts/cloud-cleanup.sh      # stops them and checks none is still running
   ```
   This **stops** the instances (the disks remain and cost a little). Terminating them and the broker volume is a separate decision (`terraform plan -destroy`; the broker data volume is protected).
4. Stop the local observability stack if you want: `docker compose -f deploy/observability/compose.observability.yaml down`.

## After each test

Ask for the README: "write the report for test N". Reports follow `final/results/test5/README.md`: verdict, what was done, results table, observability, limits, open items. Fill in the run record before you ask.
