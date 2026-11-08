#!/usr/bin/env python3
"""Fault-injection tests: LOCAL docker compose stack (default) or the AWS staging stack.

Each scenario offers a burst of checkouts, injects one fault while sagas are in
flight, restores the service, waits for the system to settle, then checks the
correctness invariants. Only checkouts the orchestrator accepted (HTTP 202) are
judged: a request refused while the orchestrator was down was never promised.

LOCAL (default): kills/restarts local containers and, for the retry-chain
scenarios, briefly renames one bookkeeping table. Never point it at anything else.

CLOUD (CHAOS_TARGET=cloud), run from your laptop:
  - containers are controlled over SSH on the hosts listed in deploy/observability/hosts.env
    (SSH_USER, SSH_KEY, ORCHESTRATOR_HOST, ORDER_HOST, INVENTORY_HOST, PAYMENT_HOST, BROKER_HOST);
  - the orchestrator is reached through an SSH port forward the script opens itself;
  - the database is queried with psql using DATABASE_TEST_URL from the repository .env;
  - results go to test-results/test3/ (chaos-test.json and chaos-test.md).
  The DB-drop and network-delay faults use sudo iptables / tc on the target host and
  are always undone in a finally block; `chaos.py heal` removes them by hand.

Usage: tools/chaos/chaos.py [scenario ...]    (default: all local scenarios, or "test3" in cloud)
       tools/chaos/chaos.py preflight          (cloud: checks every dependency, injects nothing)
       tools/chaos/chaos.py heal               (cloud: removes any leftover iptables/tc fault)
Standard library only.
"""
import base64
import concurrent.futures as cf
import datetime
import json
import os
import random
import re
import socket
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
CLOUD = os.environ.get("CHAOS_TARGET", "local") == "cloud"
COMPOSE_FILE = os.path.join(ROOT, "compose.local.yaml")
DIRECT = os.environ.get("CHAOS_DIRECT", "") == "1"  # run on the load generator: orchestrator reachable directly (set ORCH_URL)
RUN_ID = os.environ.get("CHAOS_RUN_ID", "")  # observability run id: sent as X-Test-Run-ID so logs, traces and outbox rows carry it
OUT_DIR = os.environ.get("OUT", os.path.join(ROOT, "final", "results", RUN_ID or "test3") if CLOUD else os.path.join(ROOT, "test-results", "local", "chaos"))
LOCAL_PORT = int(os.environ.get("CHAOS_LOCAL_PORT", "18083"))  # cloud: forwarded orchestrator port
ORCH = os.environ.get("ORCH_URL", f"http://127.0.0.1:{LOCAL_PORT}" if CLOUD else "http://127.0.0.1:8083")
PG = ["docker", "exec", "-i", "distri-local-postgres-1", "psql", "-U", "distri", "-d", "distri", "-At", "-F", "|"]
MQ = "distri-local-rabbitmq-1"
TERMINAL = ("COMPLETED", "COMPENSATED")

# Load shape. "Clients" is the number of concurrent request threads; the burst is paced at RATE/s.
CLIENTS = int(os.environ.get("CHAOS_CLIENTS", "30" if CLOUD else "16"))
BURST = int(os.environ.get("CHAOS_BURST", "200" if CLOUD else "120"))
RATE = int(os.environ.get("CHAOS_RATE", "50" if CLOUD else "100"))
KILL_AFTER = float(os.environ.get("CHAOS_KILL_AFTER", "1.5" if CLOUD else "0.8"))
# A payment left ambiguous resolves only after PAYMENT_WINDOW + PAYMENT_UNCONFIRMED_GRACE (5 m + 5 m by default).
SETTLE_TIMEOUT = int(os.environ.get("CHAOS_SETTLE_TIMEOUT", "900" if CLOUD else "150"))
HTTP_TIMEOUT = float(os.environ.get("CHAOS_HTTP_TIMEOUT", "10" if CLOUD else "5"))
DB_FAULT_SECONDS = int(os.environ.get("CHAOS_DB_FAULT_SECONDS", "20"))
NET_FAULT_SECONDS = int(os.environ.get("CHAOS_NET_FAULT_SECONDS", "40"))
NET_DELAY = os.environ.get("CHAOS_NET_DELAY", "200ms 50ms")  # netem: delay, jitter
FAULT_TARGET = os.environ.get("CHAOS_FAULT_TARGET", "payment-service")  # service hit by the DB-drop / delay faults

HOSTS_ENV = os.environ.get("HOSTS_ENV", os.path.join(ROOT, "deploy", "observability", "hosts.env"))
SERVICE_HOST_KEY = {"orchestrator": "ORCHESTRATOR_HOST", "order-service": "ORDER_HOST", "inventory-service": "INVENTORY_HOST",
                    "payment-service": "PAYMENT_HOST", "rabbitmq": "BROKER_HOST"}
DOCKER_FILTER = "docker ps -aq --filter label=com.docker.compose.service={} --filter label=com.docker.compose.oneoff=False | head -n1"
IPT_RULE = "DOCKER-USER -p tcp --dport 5432 -j REJECT --reject-with tcp-reset"  # container egress to Postgres
NETEM_IF = "$(ip -o -4 route show to default | awk '{print $5; exit}')"

INVARIANTS = {
    "inventory counters within bounds": "SELECT count(*) FROM inventory.inventories WHERE on_hand < 0 OR reserved < 0 OR allocated < 0 OR reserved + allocated > on_hand",
    "one reservation per order": "SELECT count(*) FROM (SELECT order_id FROM inventory.reservations GROUP BY order_id HAVING count(*) > 1) x",
    "one payment per order": "SELECT count(*) FROM (SELECT order_id FROM payments.payment_attempts GROUP BY order_id HAVING count(*) > 1) x",
    "one order row per saga": "SELECT count(*) FROM (SELECT id FROM orders.orders GROUP BY id HAVING count(*) > 1) x",
    "paid orders have a succeeded payment": "SELECT count(*) FROM orders.orders o LEFT JOIN payments.payment_attempts p ON p.order_id=o.id AND p.status='SUCCEEDED' WHERE o.status='PAID' AND p.id IS NULL",
    "paid orders have a confirmed reservation": "SELECT count(*) FROM orders.orders o JOIN inventory.reservations r ON r.order_id=o.id WHERE o.status='PAID' AND r.status<>'CONFIRMED'",
    "cancelled orders have a released reservation": "SELECT count(*) FROM orders.orders o JOIN inventory.reservations r ON r.order_id=o.id WHERE o.status='CANCELLED' AND r.status<>'RELEASED'",
    "unknown payments keep the order pending": "SELECT count(*) FROM payments.payment_attempts p JOIN orders.orders o ON o.id=p.order_id WHERE p.status='UNKNOWN' AND o.status<>'PENDING_PAYMENT'",
}


# ---------------------------------------------------------------- plumbing


def sh(*args, check=True, timeout=120):
    result = subprocess.run(args, capture_output=True, text=True, timeout=timeout)
    if check and result.returncode != 0:
        raise RuntimeError(f"{' '.join(args)} -> {result.returncode}: {result.stderr.strip()[:300]}")
    return result


def read_env_file(path):
    """KEY=VALUE lines only; anything else (comments, malformed lines) is skipped. Values are never printed."""
    values = {}
    try:
        with open(path, encoding="utf-8-sig") as handle:
            for line in handle:
                line = line.strip()
                if line and not line.startswith("#") and "=" in line:
                    key, value = line.split("=", 1)
                    if re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", key.strip()):
                        values[key.strip()] = value.strip().strip('"').strip("'")
    except OSError:
        pass
    return values


_cloud_config = {}


def cloud_config():
    if not _cloud_config:
        hosts = read_env_file(HOSTS_ENV)
        dotenv = read_env_file(os.path.join(ROOT, ".env"))
        # The live staging database is the Sydney "distri_audit" database in deploy/cloud/.env.loadgen (a postgres_fdw view over
        # the four service databases). The repository .env DATABASE_TEST_URL is the older us-east-1 branch: last resort only.
        loadgen = read_env_file(os.path.join(ROOT, "deploy", "cloud", ".env.loadgen"))
        url = os.environ.get("CHAOS_DATABASE_URL") or loadgen.get("LOADTEST_DATABASE_URL") or \
            os.environ.get("DATABASE_TEST_URL") or dotenv.get("DATABASE_TEST_URL", "")
        missing = [k for k in ["SSH_USER", "SSH_KEY", *SERVICE_HOST_KEY.values()] if not hosts.get(k)]
        if missing or not url:
            raise RuntimeError(f"cloud mode needs {HOSTS_ENV} (missing: {missing}) and a database URL (CHAOS_DATABASE_URL, deploy/cloud/.env.loadgen LOADTEST_DATABASE_URL or .env DATABASE_TEST_URL) (set: {bool(url)})")
        # libpq rejects the search_path query parameter some app DSNs carry; every query here is schema-qualified anyway.
        parts = urllib.parse.urlsplit(url)
        query = "&".join(p for p in parts.query.split("&") if p and not p.startswith("search_path="))
        _cloud_config.update(hosts=hosts, db_url=urllib.parse.urlunsplit(parts._replace(query=query)))
    return _cloud_config


def remote(service, command, check=True, timeout=120, attempts=4):
    """Run a shell command on the EC2 host that runs `service`. SSH-level failures (exit 255: timeout,
    reset) are retried; the remote command's own exit code is never retried."""
    hosts = cloud_config()["hosts"]
    target = f"{hosts['SSH_USER']}@{hosts[SERVICE_HOST_KEY[service]]}"
    args = ["ssh", "-i", os.path.expanduser(hosts["SSH_KEY"]), "-o", "BatchMode=yes", "-o", "ConnectTimeout=15",
            "-o", "StrictHostKeyChecking=accept-new", "-o", "ServerAliveInterval=15", "-o", "LogLevel=ERROR", target, command]
    result = None
    for attempt in range(attempts):
        result = subprocess.run(args, capture_output=True, text=True, timeout=timeout)
        if result.returncode != 255:
            break
        time.sleep(2 + attempt * 2)
    if check and result.returncode != 0:
        raise RuntimeError(f"ssh {service}: {command[:80]} -> {result.returncode}: {result.stderr.strip()[:300]}")
    return result


def annotate(text):
    """Best-effort Grafana annotation so the fault shows up as a marker on every dashboard. Never fails a test."""
    if not RUN_ID:
        return
    try:
        auth = base64.b64encode(f"admin:{os.environ.get('GRAFANA_ADMIN_PASSWORD', 'admin')}".encode()).decode()
        request = urllib.request.Request(os.environ.get("GRAFANA_URL", "http://127.0.0.1:3000") + "/api/annotations",
                                         data=json.dumps({"text": text, "tags": ["test_run", RUN_ID, "fault"]}).encode(), method="POST",
                                         headers={"Content-Type": "application/json", "Authorization": "Basic " + auth})
        urllib.request.urlopen(request, timeout=5).close()
    except Exception:
        pass


def ctl(service, action):
    """kill | start | restart | stop for the container behind `service`, local or remote."""
    flags = {"kill": ["kill"], "start": ["start"], "restart": ["restart", "-t", "0"], "stop": ["stop", "-t", "30"]}[action]
    if not CLOUD:
        sh("docker", *flags, container(service), timeout=70)
        return
    remote(service, f"docker {' '.join(flags)} $({DOCKER_FILTER.format(service)})", timeout=90)


def container_state(service):
    fmt = "{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}"
    if not CLOUD:
        return sh("docker", "inspect", container(service), "--format", fmt, check=False).stdout.strip()
    return remote(service, f"docker inspect $({DOCKER_FILTER.format(service)}) --format '{fmt}'", check=False).stdout.strip()


_psql = []


def psql_command():
    """Local psql if a real client is installed (Ubuntu's pg_wrapper stub is not), otherwise a throwaway container."""
    if not _psql:
        try:
            works = subprocess.run(["psql", "--version"], capture_output=True, text=True).returncode == 0 and \
                subprocess.run(["psql", cloud_config()["db_url"], "-Atc", "select 1"], capture_output=True, text=True).returncode == 0
        except FileNotFoundError:
            works = False
        _psql.extend(["psql"] if works else ["docker", "run", "--rm", "-i", "--network", "host", "postgres:17-alpine", "psql"])
    return _psql


def sql(statement):
    command = psql_command() + [cloud_config()["db_url"], "-At", "-F", "|"] if CLOUD else PG
    result = subprocess.run(command + ["-c", statement], capture_output=True, text=True, timeout=60)
    if result.returncode != 0:
        raise RuntimeError(f"sql failed: {result.stderr.strip()[:300]}")
    return [line.split("|") for line in result.stdout.strip().splitlines() if line]


def scalar(statement):
    rows = sql(statement)
    return rows[0][0] if rows else ""


def container(service):
    return f"distri-local-{service}-1"


def wait_healthy(service, timeout=90):
    deadline = time.time() + timeout
    state = ""
    while time.time() < deadline:
        try:
            state = container_state(service)
        except Exception as error:  # an SSH blip while a host is busy is retried, not fatal
            state = f"unreachable: {error}"
        parts = state.split()  # "running healthy" | "running" (no healthcheck) | "running unhealthy" | "exited"
        if parts[:1] == ["running"] and (len(parts) == 1 or parts[1] == "healthy"):
            return True
        time.sleep(2 if CLOUD else 1)
    raise RuntimeError(f"{service} did not become healthy: {state}")


def post_checkout(product_id, key=None):
    body = json.dumps({"items": [{"product_id": product_id, "quantity": 1}]}).encode()
    request = urllib.request.Request(ORCH + "/v1/checkouts", data=body, method="POST",
                                     headers={"Content-Type": "application/json", "Idempotency-Key": key or "chaos-" + uuid.uuid4().hex,
                                              **({"X-Test-Run-ID": RUN_ID} if RUN_ID else {})})
    try:
        with urllib.request.urlopen(request, timeout=HTTP_TIMEOUT) as response:
            return response.status, json.loads(response.read() or b"{}")
    except urllib.error.HTTPError as error:
        return error.code, {}
    except Exception:
        return 0, {}


def products(count):
    rows = sql("SELECT product_id FROM inventory.inventories WHERE on_hand-reserved-allocated >= 30 ORDER BY on_hand-reserved-allocated DESC LIMIT 20")
    if not rows:
        raise RuntimeError("no product has enough free stock: reseed the local stack")
    return [random.choice(rows)[0] for _ in range(count)]


class Burst:
    """Offers `count` checkouts at `rate`/s from a thread pool and records what was accepted."""

    def __init__(self, count, rate=25):
        self.count, self.rate = count, rate
        self.accepted, self.refused = [], 0
        self.pool = cf.ThreadPoolExecutor(max_workers=CLIENTS)
        self.futures = []

    def start(self):
        picks = products(self.count)
        for index, product in enumerate(picks):
            self.futures.append(self.pool.submit(self._one, product, index / self.rate))
        return self

    def _one(self, product, delay):
        time.sleep(delay)
        return post_checkout(product)

    def finish(self):
        for future in self.futures:
            status, body = future.result()
            if status == 202 and body.get("order_id"):
                self.accepted.append(body["order_id"])
            else:
                self.refused += 1
        self.pool.shutdown()
        return self.accepted


def saga_counts(ids):
    if not ids:
        return {}
    quoted = ",".join(f"'{i}'" for i in ids)
    return {status: int(n) for status, n in sql(f"SELECT status, count(*) FROM orchestrator.sagas WHERE id IN ({quoted}) GROUP BY 1")}


def settle(ids, timeout=None):
    """Wait until every saga is terminal, or give up after `timeout` (default: SETTLE_TIMEOUT)."""
    timeout = timeout or SETTLE_TIMEOUT
    started = time.time()
    last = None
    while time.time() - started < timeout:
        counts = saga_counts(ids)
        if sum(counts.get(s, 0) for s in TERMINAL) == len(ids):
            return counts, round(time.time() - started, 1)
        last = counts
        time.sleep(2)
    return saga_counts(ids), round(time.time() - started, 1)


def ambiguity_explained(ids):
    """Sagas that did not finish must be in the documented ambiguous state, never lost."""
    if not ids:
        return 0, 0
    quoted = ",".join(f"'{i}'" for i in ids)
    open_count = int(scalar(f"SELECT count(*) FROM orchestrator.sagas WHERE id IN ({quoted}) AND status NOT IN ('COMPLETED','COMPENSATED')"))
    explained = int(scalar(f"""SELECT count(*) FROM orchestrator.sagas s WHERE s.id IN ({quoted}) AND s.status NOT IN ('COMPLETED','COMPENSATED')
        AND EXISTS (SELECT 1 FROM payments.payment_attempts p WHERE p.order_id=s.id AND p.status='UNKNOWN')"""))
    return open_count, explained


def invariant_violations():
    return {name: int(scalar(query)) for name, query in INVARIANTS.items()}


def dead_queue():
    if CLOUD:
        out = remote("rabbitmq", "docker exec $(docker ps -q --filter label=com.docker.compose.service=rabbitmq | head -n1) "
                     "rabbitmqctl list_queues name messages --silent", check=False).stdout
    else:
        out = sh("docker", "exec", MQ, "rabbitmqctl", "list_queues", "name", "messages", "--silent", check=False).stdout
    queues = {}
    for line in out.splitlines():
        parts = line.split()
        if len(parts) == 2 and parts[1].isdigit():
            queues[parts[0]] = int(parts[1])
    return queues


def duplicate_effects(ids):
    if not ids:
        return {}
    quoted = ",".join(f"'{i}'" for i in ids)
    return {
        "payments_beyond_one_per_order": int(scalar(f"SELECT coalesce(sum(n-1),0) FROM (SELECT count(*) n FROM payments.payment_attempts WHERE order_id IN ({quoted}) GROUP BY order_id) x")),
        "reservations_beyond_one_per_order": int(scalar(f"SELECT coalesce(sum(n-1),0) FROM (SELECT count(*) n FROM inventory.reservations WHERE order_id IN ({quoted}) GROUP BY order_id) x")),
        "orders_missing_for_accepted_sagas": int(scalar(f"SELECT count(*) FROM orchestrator.sagas s WHERE s.id IN ({quoted}) AND s.status='COMPLETED' AND NOT EXISTS (SELECT 1 FROM orders.orders o WHERE o.id=s.id)")),
    }


# ---------------------------------------------------------------- scenarios


def in_flight():
    return int(scalar("SELECT count(*) FROM orchestrator.sagas WHERE created_at > now() - interval '90 seconds' AND status NOT IN ('COMPLETED','COMPENSATED','STUCK')"))


def inject_network_fault(service, how):
    """Start a network fault on the host of `service`. Returns the shell command that undoes it."""
    if how == "db-drop":  # every container on the host gets a TCP reset when it talks to Postgres (port 5432)
        remote(service, f"sudo -n iptables -I {IPT_RULE}")
        return f"sudo -n iptables -D {IPT_RULE}"
    if how == "net-delay":  # all egress from the host is delayed (netem); SSH to it slows down too
        remote(service, f"sudo -n tc qdisc replace dev {NETEM_IF} root netem delay {NET_DELAY}")
        return f"sudo -n tc qdisc del dev {NETEM_IF} root"
    raise ValueError(how)


def heal():
    """Remove any leftover DB-drop / delay fault from every service host."""
    for service in ("orchestrator", "order-service", "inventory-service", "payment-service"):
        for command in (f"sudo -n iptables -D {IPT_RULE}", f"sudo -n tc qdisc del dev {NETEM_IF} root"):
            result = remote(service, command, check=False)
            print(f"{service}: {command.split()[2]} -> {'removed' if result.returncode == 0 else 'nothing to remove'}")


def fault_under_load(service, how, burst=None, kill_after=None, allow_ambiguous=False):
    """Kill/restart `service` (or cut its network) while a burst is in flight."""
    burst = burst or BURST
    kill_after = KILL_AFTER if kill_after is None else kill_after
    before = invariant_violations()
    queue_before = dead_queue().get("distri.commands.dead", 0)
    run = Burst(burst, rate=RATE).start()
    time.sleep(kill_after)
    sagas_in_flight = in_flight()
    fault_started = time.time()
    fault_started_at = datetime.datetime.now(datetime.timezone.utc).isoformat()
    annotate(f"FAULT {how} -> {service} injected ({len(run.futures)} checkouts offered, {sagas_in_flight} sagas in flight)")
    if how == "kill":
        ctl(service, "kill")
    elif how == "restart":
        ctl(service, "restart")
    elif how == "stop":
        ctl(service, "stop")
    if how in ("db-drop", "net-delay"):
        undo = inject_network_fault(service, how)
        try:
            time.sleep(DB_FAULT_SECONDS if how == "db-drop" else NET_FAULT_SECONDS)
        finally:
            remote(service, undo, check=False)
    else:
        time.sleep(3)
    if how in ("kill", "stop"):
        ctl(service, "start")
    fault_seconds = round(time.time() - fault_started, 1)
    fault_ended_at = datetime.datetime.now(datetime.timezone.utc).isoformat()
    annotate(f"FAULT {how} -> {service} restored after {fault_seconds}s")
    accepted = run.finish()
    wait_healthy(service)
    counts, drain = settle(accepted)
    open_count, explained = ambiguity_explained(accepted)
    after = invariant_violations()
    duplicates = duplicate_effects(accepted)
    notes = {}
    if how == "stop":
        notes["note"] = "graceful stop; container restarted afterwards"
    dead_growth = dead_queue().get("distri.commands.dead", 0) - queue_before
    ok = (
        len(accepted) > 0
        and sagas_in_flight > 0  # otherwise the fault hit an idle system and proved nothing
        and all(v == 0 for v in duplicates.values())
        and all(after[name] <= before[name] for name in after)
        and (open_count == 0 or (allow_ambiguous and open_count == explained))
        and (dead_growth <= 0 or not CLOUD)  # Test 3 criterion: the dead queue does not grow
    )
    return {
        "accepted": len(accepted), "refused_while_down": run.refused, "sagas_in_flight_at_fault": sagas_in_flight, "saga_status": counts,
        "unfinished": open_count, "unfinished_with_unknown_payment": explained,
        "fault_started_at": fault_started_at, "fault_ended_at": fault_ended_at,
        "fault_seconds": fault_seconds, "drain_seconds": drain, "duplicate_effects": duplicates,
        "invariant_violations_before": before, "invariant_violations_after": after,
        "dead_queue_growth": dead_growth,
        "pass": ok, **notes,
    }


def retry_chain(service, table, queue_prefix):
    """Make one consumer fail transiently (its bookkeeping table disappears), watch the
    message climb the retry tiers, restore the table, and require the saga to finish."""
    schema_table = f"{table}"
    sql(f"ALTER TABLE {schema_table} RENAME TO processed_messages_chaos")
    renamed_at = time.time()
    restored = False
    peaks = {}
    try:
        status, body = post_checkout(products(1)[0])
        if status != 202:
            raise RuntimeError(f"checkout refused: {status}")
        order_id = body["order_id"]
        while time.time() - renamed_at < 9:
            for name, depth in dead_queue().items():
                if name.startswith(queue_prefix + ".retry"):
                    peaks[name] = max(peaks.get(name, 0), depth)
            time.sleep(0.5)
    finally:
        sql(f"ALTER TABLE {schema_table.rsplit('.',1)[0]}.processed_messages_chaos RENAME TO processed_messages")
        restored = True
    while time.time() - renamed_at < 50:  # let the 30 s tier be seen
        for name, depth in dead_queue().items():
            if name.startswith(queue_prefix + ".retry"):
                peaks[name] = max(peaks.get(name, 0), depth)
        time.sleep(1)
    counts, drain = settle([order_id], timeout=120)
    tiers_seen = sorted(name.rsplit(".", 1)[1] for name, peak in peaks.items() if peak > 0)
    ok = restored and counts.get("COMPLETED", 0) == 1 and len(tiers_seen) >= 1
    return {"order_id": order_id, "retry_tiers_that_held_the_message": tiers_seen, "peak_depth_per_retry_queue": {k: v for k, v in peaks.items() if v},
            "saga_status": counts, "seconds_to_finish_after_restore": drain, "pass": ok}


def two_replicas(burst=80):
    name = "distri-orch2"
    sh("docker", "rm", "-f", name, check=False)
    # Clone the running orchestrator (same image, env, network) instead of `docker compose run`,
    # which would also parse the repository's root .env.
    info = json.loads(sh("docker", "inspect", container("orchestrator")).stdout)[0]
    network = next(iter(info["NetworkSettings"]["Networks"]))
    env_args = [arg for pair in info["Config"]["Env"] for arg in ("-e", pair)]
    sh("docker", "run", "-d", "--name", name, "--network", network, *env_args, info["Config"]["Image"], timeout=120)
    try:
        time.sleep(8)
        before = invariant_violations()
        accepted = Burst(burst, rate=60).start().finish()
        counts, drain = settle(accepted)
        logs = {}
        for label, cname in (("orchestrator-1", container("orchestrator")), ("orchestrator-2", name)):
            text = sh("docker", "logs", cname, check=False).stdout + sh("docker", "logs", cname, check=False).stderr
            logs[label] = len(re.findall(r'"msg":"(?:resent saga command|outbox relay started)"', text))
        after = invariant_violations()
        duplicates = duplicate_effects(accepted)
        ok = (counts.get("COMPLETED", 0) + counts.get("COMPENSATED", 0) == len(accepted) and all(v == 0 for v in duplicates.values())
              and all(after[n] <= before[n] for n in after))
        return {"accepted": len(accepted), "saga_status": counts, "drain_seconds": drain, "duplicate_effects": duplicates,
                "relay_or_sweeper_log_events_per_replica": logs, "invariant_violations_after": after, "pass": ok}
    finally:
        sh("docker", "rm", "-f", name, check=False)


def two_replicas_cloud(burst=None):
    """Test 8: a second orchestrator replica (same image, env, database and broker) on the orchestrator host.
    Passes only if every accepted saga finishes, nothing is duplicated, and the second replica did real work."""
    burst = burst or BURST
    name = "distri-orch2"
    remote("orchestrator", f"docker rm -f {name} >/dev/null 2>&1; cd ~/distri && docker compose --env-file deploy/cloud/.env.orchestrator "
                           f"-f compose.cloud.yaml run -d --no-deps --name {name} orchestrator", timeout=240)
    try:
        time.sleep(20)  # let the replica connect to the database and declare its consumers
        before = invariant_violations()
        run = Burst(burst, rate=RATE).start()
        accepted = run.finish()
        counts, drain = settle(accepted)
        open_count, explained = ambiguity_explained(accepted)
        duplicates = duplicate_effects(accepted)
        after = invariant_violations()
        events = {}
        for label, selector in (("replica_1", f"$({DOCKER_FILTER.format('orchestrator')})"), ("replica_2", name)):
            out = remote("orchestrator", f"docker logs {selector} 2>&1 | grep -c saga_step_completed || true", check=False).stdout.strip()
            events[label] = int(out) if out.isdigit() else -1
        ok = (len(accepted) > 0 and open_count == 0 and all(v == 0 for v in duplicates.values())
              and all(after[n] <= before[n] for n in after) and events["replica_2"] > 0)
        return {"accepted": len(accepted), "refused_while_down": run.refused, "saga_status": counts, "unfinished": open_count,
                "unfinished_with_unknown_payment": explained, "drain_seconds": drain, "duplicate_effects": duplicates,
                "saga_step_completed_events_per_replica": events, "invariant_violations_before": before,
                "invariant_violations_after": after, "dead_queue_growth": 0, "pass": ok}
    finally:
        remote("orchestrator", f"docker rm -f {name}", check=False)


SCENARIOS = {
    "orchestrator-kill9-mid-burst": lambda: fault_under_load("orchestrator", "kill"),
    "order-service-kill9-mid-message": lambda: fault_under_load("order-service", "kill"),
    "inventory-kill9-mid-message": lambda: fault_under_load("inventory-service", "kill"),
    "payment-kill9-mid-flight": lambda: fault_under_load("payment-service", "kill", allow_ambiguous=True),
    "payment-sigterm-graceful": lambda: fault_under_load("payment-service", "stop", allow_ambiguous=True),
    "broker-restart-mid-burst": lambda: fault_under_load("rabbitmq", "restart", burst=None if CLOUD else 40),
    "db-connection-drop": lambda: fault_under_load(FAULT_TARGET, "db-drop", allow_ambiguous=FAULT_TARGET == "payment-service"),
    "network-delay": lambda: fault_under_load(FAULT_TARGET, "net-delay", allow_ambiguous=FAULT_TARGET == "payment-service"),
    "retry-chain-payment": lambda: retry_chain("payments.processed_messages", "payments.processed_messages", "distri.payment.commands"),
    "retry-chain-inventory": lambda: retry_chain("inventory.processed_messages", "inventory.processed_messages", "distri.inventory.commands"),
    "retry-chain-orchestrator-replies": lambda: retry_chain("orchestrator.processed_messages", "orchestrator.processed_messages", "distri.orchestrator.replies"),
    "two-live-orchestrator-replicas": lambda: two_replicas_cloud() if CLOUD else two_replicas(),
}
CLOUD_ONLY = {"db-connection-drop", "network-delay"}
LOCAL_ONLY = {"retry-chain-payment", "retry-chain-inventory", "retry-chain-orchestrator-replies"}
# Test 3 order (see tests/REPORT.md): 3a, 3b (three services), 3c, 3d, 3e.
TEST3 = ["payment-kill9-mid-flight", "orchestrator-kill9-mid-burst", "order-service-kill9-mid-message",
         "inventory-kill9-mid-message", "broker-restart-mid-burst", "db-connection-drop", "network-delay"]


# ---------------------------------------------------------------- cloud helpers


def open_orchestrator_tunnel():
    """Forward 127.0.0.1:LOCAL_PORT to the orchestrator's port 8083 on its EC2 host."""
    if DIRECT:
        return None
    hosts = cloud_config()["hosts"]
    target = f"{hosts['SSH_USER']}@{hosts['ORCHESTRATOR_HOST']}"
    proc = subprocess.Popen(["ssh", "-N", "-T", "-i", os.path.expanduser(hosts["SSH_KEY"]), "-o", "BatchMode=yes",
                             "-o", "StrictHostKeyChecking=accept-new", "-o", "ExitOnForwardFailure=yes", "-o", "ServerAliveInterval=15", "-o", "ConnectTimeout=15", "-o", "LogLevel=ERROR",
                             "-L", f"127.0.0.1:{LOCAL_PORT}:127.0.0.1:8083", target])
    deadline = time.time() + 45
    while time.time() < deadline:
        if proc.poll() is not None:
            raise RuntimeError(f"orchestrator SSH tunnel exited ({proc.returncode}); is port {LOCAL_PORT} already in use?")
        try:
            socket.create_connection(("127.0.0.1", LOCAL_PORT), timeout=1).close()
            return proc
        except OSError:
            time.sleep(0.5)
    proc.terminate()
    raise RuntimeError("orchestrator SSH tunnel did not come up in 45 s")


def git_state():
    commit = sh("git", "-C", ROOT, "rev-parse", "--short", "HEAD", check=False).stdout.strip()
    dirty = bool(sh("git", "-C", ROOT, "status", "--porcelain", "--untracked-files=no", check=False).stdout.strip())
    return f"{commit}{' (uncommitted changes)' if dirty else ''}"


def preflight():
    """Check every dependency of a cloud run. Injects nothing."""
    failures = 0

    def report(ok, name, detail=""):
        nonlocal failures
        failures += 0 if ok else 1
        print(f"{'PASS' if ok else 'FAIL'}  {name}{('  [' + str(detail) + ']') if detail else ''}", flush=True)

    for service in SERVICE_HOST_KEY:
        try:
            state = container_state(service)
            report(state in ("running healthy", "running"), f"{service} container", state)
        except Exception as error:
            report(False, f"{service} container (ssh)", str(error)[:150])
    try:
        report(scalar("SELECT 1") == "1", "database reachable (DATABASE_TEST_URL)")
        for name, query in INVARIANTS.items():
            int(scalar(query))
        report(True, "invariant queries run on the audit schemas", f"{len(INVARIANTS)} queries")
        rows = sql("SELECT product_id FROM inventory.inventories WHERE on_hand-reserved-allocated >= 30")
        free = scalar("SELECT coalesce(sum(on_hand-reserved-allocated),0) FROM inventory.inventories")
        report(len(rows) >= 5, "products with >= 30 free units", f"{len(rows)} products, {free} free units in total")
        report(int(scalar("SELECT count(*) FROM orchestrator.sagas WHERE status NOT IN ('COMPLETED','COMPENSATED')")) == 0,
               "no unfinished sagas before the run")
    except Exception as error:
        report(False, "database checks", str(error)[:200])
    tunnel = None
    try:
        tunnel = open_orchestrator_tunnel()
        with urllib.request.urlopen(ORCH + "/readyz", timeout=HTTP_TIMEOUT) as response:
            report(response.status == 200, "orchestrator /readyz through the SSH tunnel", response.status)
    except Exception as error:
        report(False, "orchestrator through the SSH tunnel", str(error)[:150])
    finally:
        if tunnel:
            tunnel.terminate()
    try:
        queues = dead_queue()
        report(bool(queues), "RabbitMQ queues readable", f"dead queue depth {queues.get('distri.commands.dead', '?')}")
        report(queues.get("distri.commands.dead", 1) == 0, "dead queue empty before the run (purge it if not)")
    except Exception as error:
        report(False, "RabbitMQ queues", str(error)[:150])
    result = remote(FAULT_TARGET, "sudo -n iptables -S DOCKER-USER >/dev/null && echo ok", check=False)
    report("ok" in result.stdout, f"iptables DOCKER-USER usable with sudo on {FAULT_TARGET}", result.stderr.strip()[:100])
    result = remote(FAULT_TARGET, "command -v tc >/dev/null && sudo -n tc qdisc show >/dev/null && echo ok", check=False)
    report("ok" in result.stdout, f"tc (netem) usable with sudo on {FAULT_TARGET}", result.stderr.strip()[:100])
    print(f"\n{'preflight OK' if not failures else str(failures) + ' check(s) failed'}")
    return failures == 0


def write_markdown(report):
    lines = ["# Test 3 — fault tests on the cloud stack", "",
             f"Run window: {report['started']} → {report['finished']} (UTC)  ", f"Git: {report['environment']['git']}", "",
             "| Scenario | Result | Accepted | Sagas in flight at fault | Refused while down | Saga status | Unfinished | Duplicates | Dead-queue growth | Drain (s) |",
             "|---|:---:|---:|---:|---:|---|---:|---|---:|---:|"]
    for name, r in report["results"].items():
        dup = r.get("duplicate_effects", {})
        lines.append(f"| {name} | {'PASS' if r['pass'] else '**FAIL**'} | {r.get('accepted', '-')} | {r.get('sagas_in_flight_at_fault', '-')} | "
                     f"{r.get('refused_while_down', '-')} | {json.dumps(r.get('saga_status', {}))} | {r.get('unfinished', '-')} "
                     f"({r.get('unfinished_with_unknown_payment', '-')} explained) | {'none' if dup and not any(dup.values()) else json.dumps(dup)} | "
                     f"{r.get('dead_queue_growth', '-')} | {r.get('drain_seconds', '-')} |")
        if r.get("harness_error"):
            lines.append(f"|  | harness error: {r['harness_error'][:200]} |||||||||")
    lines += ["", "## Settings", "", "```json", json.dumps(report["environment"], indent=2), "```", "",
              "A scenario passes only if: at least one checkout was accepted; sagas were in flight when the fault landed; no duplicate payment or "
              "reservation; no invariant got worse; every saga is COMPLETED/COMPENSATED (or explained by an UNKNOWN payment where allowed); "
              "the dead queue did not grow.", ""]
    with open(os.path.join(OUT_DIR, "chaos-test.md"), "w") as handle:
        handle.write("\n".join(lines))


def main():
    args = sys.argv[1:]
    if args == ["heal"]:
        if not CLOUD:
            sys.exit("heal only applies to CHAOS_TARGET=cloud")
        heal()
        return
    if args == ["preflight"]:
        if not CLOUD:
            sys.exit("preflight only applies to CHAOS_TARGET=cloud")
        sys.exit(0 if preflight() else 1)
    default = TEST3 if CLOUD else [n for n in SCENARIOS if n not in CLOUD_ONLY]
    wanted = default if not args else [n for a in args for n in (TEST3 if a == "test3" else [a])]
    unknown = [name for name in wanted if name not in SCENARIOS]
    if unknown:
        sys.exit(f"unknown scenario(s): {unknown}; known: {list(SCENARIOS)} plus the group 'test3'")
    wrong = [n for n in wanted if (CLOUD and n in LOCAL_ONLY) or (not CLOUD and n in CLOUD_ONLY)]
    if wrong:
        sys.exit(f"{wrong} cannot run with CHAOS_TARGET={'cloud' if CLOUD else 'local'}")
    os.makedirs(OUT_DIR, exist_ok=True)
    results = {}
    try:  # a partial rerun updates its own scenarios and keeps the others
        with open(os.path.join(OUT_DIR, "chaos-test.json")) as handle:
            results = json.load(handle).get("results", {})
    except (OSError, ValueError):
        pass
    environment = {"target": "cloud" if CLOUD else "local", "git": git_state(), "clients": CLIENTS, "burst": BURST, "rate_per_s": RATE,
                   "kill_after_s": KILL_AFTER, "settle_timeout_s": SETTLE_TIMEOUT, "fault_target": FAULT_TARGET,
                   "db_fault_seconds": DB_FAULT_SECONDS, "net_fault_seconds": NET_FAULT_SECONDS, "net_delay": NET_DELAY}
    tunnel = open_orchestrator_tunnel() if CLOUD else None
    started = datetime.datetime.now(datetime.timezone.utc)
    try:
        for name in wanted:
            print(f"== {name}", flush=True)
            begun = time.time()
            window_start = datetime.datetime.now(datetime.timezone.utc).isoformat()
            try:
                results[name] = SCENARIOS[name]()
            except Exception as error:  # a harness failure is a failed scenario, reported as such
                results[name] = {"pass": False, "harness_error": str(error)}
            results[name]["started_at"] = window_start
            results[name]["ended_at"] = datetime.datetime.now(datetime.timezone.utc).isoformat()
            results[name]["seconds"] = round(time.time() - begun, 1)
            print(f"   {'PASS' if results[name]['pass'] else 'FAIL'}  {json.dumps({k: v for k, v in results[name].items() if k in ('accepted','sagas_in_flight_at_fault','saga_status','unfinished','unfinished_with_unknown_payment','dead_queue_growth','harness_error')})}", flush=True)
            for service in ("orchestrator", "order-service", "inventory-service", "payment-service", "rabbitmq"):
                try:
                    wait_healthy(service)
                except Exception as error:
                    print(f"   WARNING {error}", flush=True)
    finally:
        if tunnel:
            tunnel.terminate()
        if CLOUD:  # never leave a network fault behind, even after a crash or Ctrl-C
            try:
                heal()
            except Exception as error:
                print(f"WARNING heal failed, run `chaos.py heal`: {error}", flush=True)
    report = {"started": started.isoformat(), "finished": datetime.datetime.now(datetime.timezone.utc).isoformat(),
              "environment": environment, "results": results}
    with open(os.path.join(OUT_DIR, "chaos-test.json"), "w") as handle:
        json.dump(report, handle, indent=2)
    write_markdown(report)
    passed = sum(1 for name in wanted if results[name]["pass"])
    print(f"{passed}/{len(wanted)} scenarios passed -> {OUT_DIR}")
    sys.exit(0 if passed == len(wanted) else 1)


if __name__ == "__main__":
    main()
