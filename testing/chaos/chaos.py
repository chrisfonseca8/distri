#!/usr/bin/env python3
"""Fault-injection tests against the LOCAL docker compose stack (compose.local.yaml).

Each scenario offers a burst of checkouts, injects one fault while sagas are in
flight, restores the service, waits for the system to settle, then checks the
correctness invariants. Only checkouts the orchestrator accepted (HTTP 202) are
judged: a request refused while the orchestrator was down was never promised.

Destructive to the LOCAL stack only: it kills/restarts containers and, for the
retry-chain scenarios, briefly renames one bookkeeping table. Never point it at
anything else. Standard library only.

Usage: testing/chaos/chaos.py [scenario ...]    (default: all)
"""
import concurrent.futures as cf
import datetime
import json
import os
import random
import re
import subprocess
import sys
import time
import urllib.error
import urllib.request
import uuid

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
COMPOSE_FILE = os.path.join(ROOT, "compose.local.yaml")
OUT_DIR = os.environ.get("OUT", os.path.join(ROOT, "test-results", "local", "chaos"))
ORCH = os.environ.get("ORCH_URL", "http://127.0.0.1:8083")
PG = ["docker", "exec", "-i", "distri-local-postgres-1", "psql", "-U", "distri", "-d", "distri", "-At", "-F", "|"]
MQ = "distri-local-rabbitmq-1"
TERMINAL = ("COMPLETED", "COMPENSATED")

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


def sql(statement):
    result = subprocess.run(PG + ["-c", statement], capture_output=True, text=True, timeout=60)
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
    while time.time() < deadline:
        state = sh("docker", "inspect", container(service), "--format", "{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}", check=False).stdout.strip()
        if state.startswith("running") and (state.endswith("healthy") or state == "running "):
            return True
        time.sleep(1)
    raise RuntimeError(f"{service} did not become healthy: {state}")


def post_checkout(product_id, key=None):
    body = json.dumps({"items": [{"product_id": product_id, "quantity": 1}]}).encode()
    request = urllib.request.Request(ORCH + "/v1/checkouts", data=body, method="POST",
                                     headers={"Content-Type": "application/json", "Idempotency-Key": key or "chaos-" + uuid.uuid4().hex})
    try:
        with urllib.request.urlopen(request, timeout=5) as response:
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
        self.pool = cf.ThreadPoolExecutor(max_workers=16)
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


def settle(ids, timeout=150):
    """Wait until every saga is terminal or the set stops changing for 25 s after `timeout`."""
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


def fault_under_load(service, how, burst=120, kill_after=0.8, allow_ambiguous=False):
    """Kill/restart `service` while a burst is in flight."""
    before = invariant_violations()
    queue_before = dead_queue().get("distri.commands.dead", 0)
    run = Burst(burst, rate=100).start()
    time.sleep(kill_after)
    sagas_in_flight = in_flight()
    if how == "kill":
        sh("docker", "kill", container(service))
    elif how == "restart":
        sh("docker", "restart", "-t", "0", container(service), timeout=60)
    elif how == "stop":
        stopped_at = time.time()
        result = sh("docker", "stop", "-t", "30", container(service), timeout=60)
    time.sleep(3)
    if how in ("kill", "stop"):
        sh("docker", "start", container(service))
    accepted = run.finish()
    wait_healthy(service)
    counts, drain = settle(accepted)
    open_count, explained = ambiguity_explained(accepted)
    after = invariant_violations()
    duplicates = duplicate_effects(accepted)
    notes = {}
    if how == "stop":
        exit_code = sh("docker", "inspect", container(service), "--format", "{{.State.ExitCode}}", check=False).stdout.strip()
        notes["note"] = "graceful stop; container restarted afterwards"
    ok = (
        len(accepted) > 0
        and sagas_in_flight > 0  # otherwise the fault hit an idle system and proved nothing
        and all(v == 0 for v in duplicates.values())
        and all(after[name] <= before[name] for name in after)
        and (open_count == 0 or (allow_ambiguous and open_count == explained))
    )
    return {
        "accepted": len(accepted), "refused_while_down": run.refused, "sagas_in_flight_at_fault": sagas_in_flight, "saga_status": counts,
        "unfinished": open_count, "unfinished_with_unknown_payment": explained,
        "drain_seconds": drain, "duplicate_effects": duplicates,
        "invariant_violations_before": before, "invariant_violations_after": after,
        "dead_queue_growth": dead_queue().get("distri.commands.dead", 0) - queue_before,
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


SCENARIOS = {
    "orchestrator-kill9-mid-burst": lambda: fault_under_load("orchestrator", "kill"),
    "order-service-kill9-mid-message": lambda: fault_under_load("order-service", "kill"),
    "inventory-kill9-mid-message": lambda: fault_under_load("inventory-service", "kill"),
    "payment-kill9-mid-flight": lambda: fault_under_load("payment-service", "kill", allow_ambiguous=True),
    "payment-sigterm-graceful": lambda: fault_under_load("payment-service", "stop", allow_ambiguous=True),
    "broker-restart-mid-burst": lambda: fault_under_load("rabbitmq", "restart", burst=40),
    "retry-chain-payment": lambda: retry_chain("payments.processed_messages", "payments.processed_messages", "distri.payment.commands"),
    "retry-chain-inventory": lambda: retry_chain("inventory.processed_messages", "inventory.processed_messages", "distri.inventory.commands"),
    "retry-chain-orchestrator-replies": lambda: retry_chain("orchestrator.processed_messages", "orchestrator.processed_messages", "distri.orchestrator.replies"),
    "two-live-orchestrator-replicas": two_replicas,
}


def main():
    wanted = sys.argv[1:] or list(SCENARIOS)
    unknown = [name for name in wanted if name not in SCENARIOS]
    if unknown:
        sys.exit(f"unknown scenario(s): {unknown}; known: {list(SCENARIOS)}")
    os.makedirs(OUT_DIR, exist_ok=True)
    results = {}
    try:  # a partial rerun updates its own scenarios and keeps the others
        with open(os.path.join(OUT_DIR, "chaos-test.json")) as handle:
            results = json.load(handle).get("results", {})
    except (OSError, ValueError):
        pass
    started = datetime.datetime.now(datetime.timezone.utc)
    for name in wanted:
        print(f"== {name}", flush=True)
        begun = time.time()
        try:
            results[name] = SCENARIOS[name]()
        except Exception as error:  # a harness failure is a failed scenario, reported as such
            results[name] = {"pass": False, "harness_error": str(error)}
        results[name]["seconds"] = round(time.time() - begun, 1)
        print(f"   {'PASS' if results[name]['pass'] else 'FAIL'}  {json.dumps({k: v for k, v in results[name].items() if k in ('accepted','sagas_in_flight_at_fault','saga_status','unfinished','unfinished_with_unknown_payment','harness_error')})}", flush=True)
        for service in ("orchestrator", "order-service", "inventory-service", "payment-service", "rabbitmq"):
            try:
                wait_healthy(service)
            except Exception as error:
                print(f"   WARNING {error}", flush=True)
    report = {"started": started.isoformat(), "finished": datetime.datetime.now(datetime.timezone.utc).isoformat(), "results": results}
    with open(os.path.join(OUT_DIR, "chaos-test.json"), "w") as handle:
        json.dump(report, handle, indent=2)
    passed = sum(1 for name in wanted if results[name]["pass"])
    print(f"{passed}/{len(wanted)} scenarios passed")
    sys.exit(0 if passed == len(wanted) else 1)


if __name__ == "__main__":
    main()
