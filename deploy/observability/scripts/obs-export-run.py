#!/usr/bin/env python3
"""Export the evidence of one test run to test-results/<test_run_id>/.

  obs-export-run.py <test_run_id> [--prom URL] [--loki URL] [--grafana URL] [--render]

Writes prometheus-metrics.json, logs.json, audit.json, notes.md (and, with --render
and the grafana-image-renderer service running, dashboard-*.png) next to run.json,
so the evidence survives the EC2 hosts, the SSH tunnels and the local stack.
"""
import argparse, base64, datetime as dt, json, os, sys, urllib.parse, urllib.request

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))

RUN = "__RUN__"  # replaced with test_run_id="<id>" before querying


def m(name, extra=""):
    """Metric selector restricted to the run."""
    return name + "{" + RUN + ("," + extra if extra else "") + "}"


# What every run is judged on. Instant values at the end of the run plus the max
# over the run, so a transient failure is not hidden by recovery.
CORRECTNESS = {
    "stuck_sagas": f'sum({m("sagas_by_status", "status=\"STUCK\"")})',
    "dead_outbox_rows": f'sum({m("outbox_dead_rows")})',
    "dead_letter_queue_depth": f'sum({m("rabbitmq_detailed_queue_messages", "queue=~\".*\\\\.dead\"")})',
    "unconfirmed_payments": f'sum({m("payment_attempts_unconfirmed")})',
    "unknown_payments": f'sum({m("payment_attempts_by_status", "status=\"UNKNOWN\"")})',
    "refunds_incomplete": f'sum({m("payment_refunds_incomplete")})',
    "stock_invariant_violations": f'sum({m("inventory_stock_invariant_violations")})',
    "dead_payment_jobs": f'sum({m("payment_jobs", "status=\"dead\"")})',
    "webhooks_parked": f'sum({m("payment_webhook_parked")})',
    "sagas_started": f'sum({m("sagas_started_total")})',
    "sagas_completed": f'sum({m("saga_completions_total", "status=\"COMPLETED\"")})',
    "sagas_compensated": f'sum({m("saga_completions_total", "status=\"COMPENSATED\"")})',
    "http_5xx_total": f'sum({m("http_requests_total", "code_class=\"5xx\"")})',
}
SERIES = {  # name -> PromQL over the run window
    "request_rate_by_service": f'sum by (service) (rate({m("http_requests_total")}[30s]))',
    "p95_latency_by_service": f'histogram_quantile(0.95, sum by (service, le) (rate({m("http_request_duration_seconds_bucket")}[1m])))',
    "saga_accepted_rate": f'sum(rate({m("sagas_started_total")}[30s]))',
    "saga_completed_rate": f'sum(rate({m("saga_completions_total", "status=\"COMPLETED\"")}[30s]))',
    "saga_duration_p95": f'histogram_quantile(0.95, sum by (le) (rate({m("saga_duration_seconds_bucket")}[1m])))',
    "outbox_pending": f'sum by (service) ({m("outbox_pending_rows")})',
    "outbox_oldest_age": f'max by (service) ({m("outbox_oldest_pending_age_seconds")})',
    "queue_depth_ready": f'sum by (queue) ({m("rabbitmq_detailed_queue_messages_ready")})',
    "db_pool_in_use": f'sum by (service) ({m("go_sql_in_use_connections")})',
    "db_pool_wait_rate": f'sum by (service) (rate({m("go_sql_wait_count_total")}[1m]))',
    "db_query_p95": f'histogram_quantile(0.95, sum by (service, le) (rate({m("db_query_duration_seconds_bucket")}[1m])))',
    "alerts_firing": 'count by (alertname) (ALERTS{alertstate="firing"})',
}


def get(url, params=None, headers=None):
    if params:
        url += "?" + urllib.parse.urlencode(params)
    req = urllib.request.Request(url, headers=headers or {})
    with urllib.request.urlopen(req, timeout=30) as r:
        return r.read()


def jget(*a, **k):
    return json.loads(get(*a, **k))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("run")
    ap.add_argument("--prom", default=f"http://127.0.0.1:{os.environ.get('PROMETHEUS_PORT', '9090')}")
    ap.add_argument("--loki", default="http://127.0.0.1:3100")
    ap.add_argument("--grafana", default="http://127.0.0.1:3000")
    ap.add_argument("--render", action="store_true")
    ap.add_argument("--step", default="15")
    a = ap.parse_args()

    out = os.path.join(ROOT, "test-results", a.run)
    os.makedirs(out, exist_ok=True)
    meta_path = os.path.join(out, "run.json")
    meta = json.load(open(meta_path)) if os.path.exists(meta_path) else {"test_run_id": a.run}
    now = dt.datetime.now(dt.timezone.utc)
    start = dt.datetime.fromisoformat(meta["started_at"].replace("Z", "+00:00")) if meta.get("started_at") else now - dt.timedelta(hours=1)
    end = dt.datetime.fromisoformat(meta["ended_at"].replace("Z", "+00:00")) if meta.get("ended_at") else now
    end_p = end + dt.timedelta(seconds=30)  # include the last scrapes
    sel = f'test_run_id="{a.run}"'

    inject = lambda q: q.replace(RUN, sel)

    # 1. prometheus-metrics.json
    metrics = {"window": {"start": start.isoformat(), "end": end.isoformat()}, "series": {}, "correctness": {}}
    for name, q in SERIES.items():
        try:
            r = jget(a.prom + "/api/v1/query_range", {"query": inject(q), "start": start.timestamp(), "end": end_p.timestamp(), "step": a.step})
            metrics["series"][name] = {"query": inject(q), "result": r["data"]["result"]}
        except Exception as e:  # keep exporting
            metrics["series"][name] = {"error": str(e)}
    for name, q in CORRECTNESS.items():
        try:
            last = jget(a.prom + "/api/v1/query", {"query": inject(q), "time": end_p.timestamp()})["data"]["result"]
            span = max(int((end_p - start).total_seconds()), 60)
            peak = jget(a.prom + "/api/v1/query", {"query": f"max_over_time(({inject(q)})[{span}s:15s])", "time": end_p.timestamp()})["data"]["result"]
            val = lambda r: float(r[0]["value"][1]) if r else 0.0
            metrics["correctness"][name] = {"at_end": val(last), "peak_during_run": val(peak)}
        except Exception as e:
            metrics["correctness"][name] = {"error": str(e)}
    json.dump(metrics, open(os.path.join(out, "prometheus-metrics.json"), "w"), indent=1)

    # 2. logs.json (every log line of the run; order/saga ids stay inside the lines)
    logs, cursor, total = [], int(start.timestamp() * 1e9), 0
    while total < 200000:
        r = jget(a.loki + "/loki/api/v1/query_range", {"query": f'{{test_run_id="{a.run}"}}', "start": cursor,
                 "end": int(end_p.timestamp() * 1e9), "limit": 5000, "direction": "forward"})
        batch = [(int(v[0]), s["stream"], v[1]) for s in r["data"]["result"] for v in s["values"]]
        if not batch:
            break
        batch.sort(key=lambda x: x[0])
        logs.extend({"ts_ns": t, "labels": l, "line": line} for t, l, line in batch)
        total += len(batch)
        if len(batch) < 5000:
            break
        cursor = batch[-1][0] + 1
    json.dump(logs, open(os.path.join(out, "logs.json"), "w"), indent=1)

    # 3. audit.json: alerts that fired during the run + the correctness verdict
    fired = jget(a.prom + "/api/v1/query_range", {"query": 'ALERTS{alertstate="firing"}',
                 "start": start.timestamp(), "end": end_p.timestamp(), "step": a.step})["data"]["result"]
    names = sorted({r["metric"]["alertname"] for r in fired})
    events = {}
    for e in ("saga_stuck", "outbox_publish_failed", "outbox_message_marked_dead", "payment_unconfirmed",
              "late_capture_refund_required", "stock_invariant_failed"):
        events[e] = sum(1 for l in logs if f'"event":"{e}"' in l["line"])
    c = metrics["correctness"]
    violations = [k for k in ("stuck_sagas", "dead_outbox_rows", "unconfirmed_payments", "refunds_incomplete", "stock_invariant_violations", "dead_payment_jobs")
                  if isinstance(c.get(k), dict) and c[k].get("peak_during_run", 0) > 0]
    audit = {"test_run_id": a.run, "alerts_fired": names, "event_counts_in_logs": events,
             "correctness": c, "violations": violations, "verdict": "PASS" if not violations else "REVIEW"}
    json.dump(audit, open(os.path.join(out, "audit.json"), "w"), indent=1)

    # 4. dashboard PNGs (optional: needs the grafana-image-renderer service)
    shots = []
    if a.render:
        tok = base64.b64encode(f"admin:{os.environ.get('GRAFANA_ADMIN_PASSWORD', 'admin')}".encode()).decode()
        for uid, fname in (("01-system-overview", "dashboard-overview.png"), ("02-service-red", "dashboard-services.png"),
                           ("03-saga-flow", "dashboard-saga.png"), ("04-payment-compensation", "dashboard-payments.png"),
                           ("05-rabbitmq-outbox", "dashboard-rabbitmq-outbox.png"), ("06-postgres-infra", "dashboard-postgres-infra.png")):
            try:
                png = get(f"{a.grafana}/render/d/{uid}", {"var-test_run_id": a.run, "from": int(start.timestamp() * 1000),
                          "to": int(end_p.timestamp() * 1000), "width": 1600, "height": 1800, "kiosk": ""}, {"Authorization": "Basic " + tok})
                open(os.path.join(out, fname), "wb").write(png); shots.append(fname)
            except Exception as e:
                print(f"render {uid} skipped: {e}", file=sys.stderr)

    notes = os.path.join(out, "notes.md")
    if not os.path.exists(notes):
        open(notes, "w").write(f"# Run {a.run}\n\nVerdict (auto): **{audit['verdict']}**  violations: {violations or 'none'}\n\n"
                               f"Alerts that fired: {', '.join(names) or 'none'}\n\n## What was run\n\n## Observations\n\n## Limitations\n")
    print(json.dumps({"run": a.run, "verdict": audit["verdict"], "violations": violations, "alerts": names,
                      "log_lines": len(logs), "screenshots": shots, "dir": out}, indent=1))


if __name__ == "__main__":
    main()
