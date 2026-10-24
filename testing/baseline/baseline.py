#!/usr/bin/env python3
"""Low-rate baseline sanity run (NOT a capacity benchmark).

50 checkout arrivals spread evenly over 60 s (open loop: arrival i fires at t0 + i*1.2 s
whether or not earlier ones finished), one unit of one product each, products drawn
uniformly (a seeded shuffle of all seeded products, repeated if there are fewer than
arrivals), then a drain period until every saga is terminal or the drain limit passes.

It only reads and writes through the public checkout API; it never edits service tables.
Stdlib only. See docs/baseline.md for the stack configuration and what is recorded.

  python3 testing/baseline/baseline.py --label B0
"""
import argparse, json, os, random, socket, subprocess, threading, time, urllib.request, urllib.error, uuid, statistics, datetime

ORCH = os.environ.get("ORCH_URL", "http://localhost:8083")
PG = ["docker", "exec", "distri-local-postgres-1", "psql", "-U", "distri", "-d", "distri", "-At", "-F", "|", "-c"]
TERMINAL = {"COMPLETED", "COMPENSATED", "STUCK"}
SERVICES = ["order-service", "orchestrator", "inventory-service", "payment-service", "gateway-simulator", "postgres", "rabbitmq"]


def sh(cmd, timeout=30):
    r = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)
    return r.stdout.strip()


def psql(q):
    return sh(PG + [q])


def http(method, url, body=None, headers=None, timeout=10):
    req = urllib.request.Request(url, data=body, method=method, headers=headers or {})
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return r.status, r.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()
    except (TimeoutError, socket.timeout) as e:  # the request may or may not have been processed
        return -1, str(e).encode()
    except urllib.error.URLError as e:
        if isinstance(e.reason, (TimeoutError, socket.timeout)):
            return -1, str(e).encode()
        return 0, str(e).encode()  # could not connect / reset: not sent or not known
    except Exception as e:
        return 0, str(e).encode()


def pct(xs, p):
    if not xs:
        return None
    xs = sorted(xs)
    return xs[min(len(xs) - 1, max(0, int(round(p / 100 * len(xs) + 0.5)) - 1))]


MAX_INFLIGHT_POSTS = 16   # bounded HTTP concurrency for checkout posts
POST_TIMEOUT_S = 10


def build_schedule(arrivals, duration, products, seed):
    """Arrival i is due at i*duration/arrivals seconds; products are a seeded uniform shuffle."""
    rng = random.Random(seed)
    order = []
    while len(order) < arrivals:
        batch = products[:]
        rng.shuffle(batch)
        order += batch
    interval = duration / arrivals
    return [dict(i=i, product=order[i], due=i * interval) for i in range(arrivals)]


def summarize(rows, offer_window):
    """Accounting over this run's rows only. Acceptance and completion are independent."""
    accepted = [r for r in rows if r.get("status") in (200, 201, 202)]
    dispatched = [r for r in rows if "dispatched" in r]
    rejected = [r for r in rows if r.get("status", 0) > 0 and r["status"] not in (200, 201, 202)]
    ambiguous = [r for r in rows if r.get("status") == -1]
    failed = [r for r in rows if r.get("status") == 0]
    done = [r for r in rows if "final" in r]
    lag = [r["dispatched"] - r["scheduled"] for r in dispatched]
    return {
        "scheduled": len(rows), "dispatched": len(dispatched), "accepted": len(accepted),
        "rejected": len(rejected), "ambiguous_timeouts": len(ambiguous), "failed_transport": len(failed),
        "completed": len(done), "final_status": {s: sum(1 for r in done if r["final"] == s) for s in sorted({r["final"] for r in done})},
        "completed_not_accepted": sum(1 for r in done if r not in accepted),
        "unresolved": len([r for r in rows if "order_id" in r and "final" not in r]),
        "dispatch_lag_s": {"mean": statistics.mean(lag) if lag else None, "p95": pct(lag, 95), "max": max(lag, default=None)},
        "actual_arrival_rate_per_s": (len(dispatched) - 1) / (max(r["dispatched"] for r in dispatched) - min(r["dispatched"] for r in dispatched)) if len(dispatched) > 1 else None,
        "accepted_per_s": len(accepted) / offer_window if offer_window else None,
    }


def dead_count():
    for line in sh(["docker", "exec", "distri-local-rabbitmq-1", "rabbitmqctl", "list_queues", "name", "messages"]).splitlines():
        if line.startswith("distri.commands.dead"):
            return int(line.split()[-1])
    return None


def queue_depths():
    out = {}
    for line in sh(["docker", "exec", "distri-local-rabbitmq-1", "rabbitmqctl", "list_queues", "name", "messages_ready", "messages_unacknowledged"]).splitlines():
        p = line.split()
        if len(p) == 3 and p[0].startswith("distri.") and p[1].isdigit():
            out[p[0]] = int(p[1]) + int(p[2])
    return out


def backlogs():
    q = ("select 'outbox_unpublished', (select count(*) from orders.outbox where published_at is null)"
         "+(select count(*) from inventory.outbox where published_at is null)"
         "+(select count(*) from payments.outbox where published_at is null)"
         "+(select count(*) from orchestrator.outbox where published_at is null)"
         " union all select 'jobs_pending', (select count(*) from payments.jobs where status not in ('succeeded','dead'))"
         "+(select count(*) from orders.jobs where status not in ('succeeded','dead'))"
         " union all select 'webhook_inbox_unprocessed', count(*) from payments.webhook_events where status <> 'PROCESSED'"
         " union all select 'sagas_running', count(*) from orchestrator.sagas where status in ('RUNNING','COMPENSATING')"
         " union all select 'compensation_open', count(*) from payments.compensation_records where status not in ('PROCESSED','REVIEW')")
    return {k: int(v) for k, v in (l.split("|") for l in psql(q).splitlines() if l)}


def stats_sample():
    out = {}
    raw = sh(["docker", "stats", "--no-stream", "--format", "{{.Name}}|{{.CPUPerc}}|{{.MemUsage}}"], timeout=60)
    for line in raw.splitlines():
        n, cpu, mem = line.split("|")
        for s in SERVICES:
            if n == f"distri-local-{s}-1":
                out[s] = {"cpu_pct": float(cpu.strip("%")), "mem": mem.split("/")[0].strip()}
    return out


def snapshot_config():
    keep = ("GATEWAY_", "PAYMENT_", "COMPENSATION_", "WEBHOOK_", "DB_MAX", "RAZORPAY_BASE_URL", "WORKER", "SIM_", "SAGA_", "OUTBOX_", "RABBITMQ_PREFETCH")
    cfg = {}
    for svc in ("payment-service", "gateway-simulator", "orchestrator", "order-service", "inventory-service"):
        env = sh(["docker", "exec", f"distri-local-{svc}-1", "env"]).splitlines()
        cfg[svc] = sorted(e for e in env if e.startswith(keep) and "SECRET" not in e and "KEY" not in e and "PASS" not in e)
    return cfg


def counts():
    """Historical-background counters, read before and after; the run reports deltas."""
    q = ("select 'dead_jobs', (select count(*) from payments.jobs where status='dead')+(select count(*) from orders.jobs where status='dead')"
         " union all select 'compensation_records', count(*) from payments.compensation_records"
         " union all select 'webhook_events_total', count(*) from payments.webhook_events")
    return {k: int(v) for k, v in (l.split("|") for l in psql(q).splitlines() if l)}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--label", default="B0")
    ap.add_argument("--arrivals", type=int, default=50)
    ap.add_argument("--duration", type=float, default=60.0)
    ap.add_argument("--drain", type=float, default=180.0)
    ap.add_argument("--seed", type=int, default=42)
    ap.add_argument("--sample-every", type=float, default=5.0)
    ap.add_argument("--out", default="test-results/baseline")
    ap.add_argument("--dry-run", action="store_true", help="print the schedule and exit without sending traffic")
    a = ap.parse_args()

    run_id = f"{a.label}-{uuid.uuid4().hex[:8]}"
    products = psql("select product_id from inventory.inventories order by product_id").splitlines()
    sched = build_schedule(a.arrivals, a.duration, products, a.seed)
    if a.dry_run:
        print(json.dumps({"run_id": run_id, "arrivals": len(sched), "last_due_s": sched[-1]["due"], "products_used": len({r["product"] for r in sched})}))
        return
    stock = {p: int(v) for p, v in (l.split("|") for l in psql("select product_id, on_hand-reserved from inventory.inventories").splitlines())}
    need = {}
    for r in sched:
        need[r["product"]] = need.get(r["product"], 0) + 1
    short = [p for p, n in need.items() if stock.get(p, 0) < n]
    if short:
        raise SystemExit(f"preflight: not enough stock for {short}")

    pre = {"dead": dead_count(), "db_bytes": int(psql("select pg_database_size('distri')")), "backlogs": backlogs(),
           "counts": counts(), "queues": queue_depths(), "config": snapshot_config(), "started_utc": datetime.datetime.utcnow().isoformat() + "Z"}
    rows = [dict(r, key=f"{run_id}-{r['i']}") for r in sched]
    samples, stop = [], threading.Event()

    def sampler():
        t0 = time.time()
        while not stop.is_set():
            try:
                samples.append({"t": round(time.time() - t0, 1), "backlogs": backlogs(), "queues_total": sum(queue_depths().values()), "containers": stats_sample()})
            except Exception as e:  # sampling must never break the run
                samples.append({"t": round(time.time() - t0, 1), "error": str(e)})
            stop.wait(a.sample_every)

    threading.Thread(target=sampler, daemon=True).start()

    gate = threading.BoundedSemaphore(MAX_INFLIGHT_POSTS)
    start = time.time()
    for r in rows:
        r["scheduled"] = start + r["due"]

    def fire(r):
        body = json.dumps({"items": [{"product_id": r["product"], "quantity": 1}]}).encode()
        with gate:
            r["dispatched"] = time.time()
            status, raw = http("POST", ORCH + "/v1/checkouts", body, {"Idempotency-Key": r["key"], "Content-Type": "application/json"}, timeout=POST_TIMEOUT_S)
            r["post_s"] = time.time() - r["dispatched"]
        r["status"] = status
        if status in (200, 201, 202):
            r["order_id"] = json.loads(raw)["order_id"]

    # completion is tracked by a poller that runs from the first arrival, independent of HTTP acceptance
    drain_deadline = [None]
    poll_done = threading.Event()

    def poller():
        while not poll_done.is_set():
            open_rows = [r for r in rows if "order_id" in r and "final" not in r]
            for r in open_rows:
                status, raw = http("GET", f"{ORCH}/v1/checkouts/{r['order_id']}", timeout=5)
                if status == 200:
                    st = json.loads(raw)["status"]
                    if st in TERMINAL:
                        r["observed"], r["final"] = time.time(), st
            if drain_deadline[0] and time.time() > drain_deadline[0]:
                return
            poll_done.wait(0.5)

    threading.Thread(target=poller, daemon=True).start()
    threads = []
    for r in rows:
        delay = r["scheduled"] - time.time()
        if delay > 0:
            time.sleep(delay)
        th = threading.Thread(target=fire, args=(r,))
        th.start()
        threads.append(th)
    for th in threads:
        th.join()
    offer_end = time.time()
    drain_deadline[0] = offer_end + a.drain
    while time.time() < drain_deadline[0] and any("order_id" in r and "final" not in r for r in rows):
        time.sleep(0.5)
    poll_done.set()
    end = time.time()
    stop.set()

    # ambiguous POST timeouts: ask the orchestrator's own idempotency table whether the request landed (read-only)
    for r in rows:
        if r.get("status") == -1:
            oid = psql(f"select coalesce(order_id::text,'') from orchestrator.checkout_attempts where idempotency_key='{r['key']}'")
            r["ambiguous_resolved_order_id"] = oid or None

    ids = [r["order_id"] for r in rows if "order_id" in r]
    in_list = ",".join(f"'{i}'" for i in ids) or "''"
    run_db = {
        "sagas_by_status": dict(l.split("|") for l in psql(f"select status, count(*) from orchestrator.sagas where id in ({in_list}) group by 1").splitlines() if l),
        "payment_attempts_by_status": dict(l.split("|") for l in psql(f"select status, count(*) from payments.payment_attempts where order_id in ({in_list}) group by 1").splitlines() if l),
        "payment_attempts_gateway_labels": dict(l.split("|") for l in psql(f"select gateway, count(*) from payments.payment_attempts where order_id in ({in_list}) group by 1").splitlines() if l),
        "compensation_records_for_this_run": int(psql(f"select count(*) from payments.compensation_records where order_id in ({in_list})") or 0),
        "duplicate_attempts_per_order": int(psql(f"select count(*) from (select order_id from payments.payment_attempts where order_id in ({in_list}) group by 1 having count(*)>1) x") or 0),
    }
    post = {"dead": dead_count(), "db_bytes": int(psql("select pg_database_size('distri')")), "backlogs": backlogs(), "counts": counts(), "queues": queue_depths()}

    window = max(offer_end - start, 1e-9)
    summ = summarize(rows, window)
    done = [r for r in rows if "final" in r]
    e2e = [r["observed"] - r["dispatched"] for r in done]
    lat = [r["post_s"] for r in rows if r.get("status") in (200, 201, 202)]
    last_done = max((r["observed"] for r in done), default=start)
    report = {
        "label": a.label, "run_id": run_id, "kind": "low-rate baseline sanity run, not a capacity benchmark; gateway is the simulator",
        "small_sample_note": f"n={a.arrivals}; percentiles are order statistics of a tiny sample",
        "spread_s": a.duration, "scheduled_interval_s": a.duration / a.arrivals, "seed": a.seed,
        "post_timeout_s": POST_TIMEOUT_S, "max_inflight_posts": MAX_INFLIGHT_POSTS,
        "product_distribution": {p: sum(1 for r in rows if r["product"] == p) for p in sorted({r["product"] for r in rows})},
        "http_status": {str(s): sum(1 for r in rows if r.get("status") == s) for s in sorted({r.get("status") for r in rows if "status" in r})},
        **summ,
        "intervals_s": {"offer_window": window, "total_incl_drain": end - start, "first_arrival_to_last_completion": last_done - start},
        "completed_per_s_over_offer_window": len(done) / window,
        "completed_per_s_first_arrival_to_last_completion": len(done) / max(last_done - start, 1e-9),
        "unresolved_orders": [r["order_id"] for r in rows if "order_id" in r and "final" not in r],
        "ambiguous_resolved": {r["key"]: r.get("ambiguous_resolved_order_id") for r in rows if r.get("status") == -1},
        "checkout_post_latency_s": {"p50": pct(lat, 50), "p95": pct(lat, 95), "p99": pct(lat, 99), "max": max(lat, default=None)},
        "end_to_end_s": {"basis": "dispatch to observed terminal saga (poll every ~0.5 s)", "mean": statistics.mean(e2e) if e2e else None,
                         "p50": pct(e2e, 50), "p95": pct(e2e, 95), "p99": pct(e2e, 99), "max": max(e2e, default=None)},
        "this_run_in_database": run_db,
        "dead_queue": {"before": pre["dead"], "after": post["dead"], "delta": (post["dead"] or 0) - (pre["dead"] or 0)},
        "dead_jobs": {"before": pre["counts"]["dead_jobs"], "after": post["counts"]["dead_jobs"], "delta": post["counts"]["dead_jobs"] - pre["counts"]["dead_jobs"]},
        "counts_before_after": {"before": pre["counts"], "after": post["counts"]},
        "database_size_bytes": {"before": pre["db_bytes"], "after": post["db_bytes"], "delta": post["db_bytes"] - pre["db_bytes"],
                                "note": "local Docker Postgres, whole database; Neon storage is unavailable (not used in this run)"},
        "backlogs": {"before": pre["backlogs"], "after": post["backlogs"]},
        "queues_before_after": {"before": pre["queues"], "after": post["queues"]},
        "max_backlogs_sampled": {k: max((s.get("backlogs", {}).get(k, 0) for s in samples), default=0) for k in pre["backlogs"]},
        "max_queue_total_sampled": max((s.get("queues_total", 0) for s in samples), default=0),
        "peak_container_cpu_pct": {svc: max((s.get("containers", {}).get(svc, {}).get("cpu_pct", 0) for s in samples), default=0) for svc in SERVICES},
        "last_container_mem": next((s["containers"] for s in reversed(samples) if s.get("containers")), {}),
        "config": pre["config"], "started_utc": pre["started_utc"], "samples": samples,
        "rows": [{k: v for k, v in r.items() if k != "key"} | {"key": r["key"]} for r in rows],
    }
    os.makedirs(a.out, exist_ok=True)
    path = os.path.join(a.out, f"{a.label}.json")
    json.dump(report, open(path, "w"), indent=1)
    slim = {k: v for k, v in report.items() if k not in ("samples", "config", "product_distribution", "rows")}
    print(json.dumps(slim, indent=1))
    print("full report:", path)


if __name__ == "__main__":
    main()
