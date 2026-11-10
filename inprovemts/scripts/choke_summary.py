#!/usr/bin/env python3
"""Where is the next choke? One run, all services, from the local Prometheus.
usage: choke_summary.py <data-dir> [measure_seconds=180]    (writes <data-dir>/choke-summary.md)
Measurement window = report started_at .. +measure; 'whole' = started_at .. finished_at.
"""
import json, sys, urllib.parse, urllib.request, datetime as dt
d = sys.argv[1]; meas = int(sys.argv[2]) if len(sys.argv) > 2 else 180
r = json.load(open(f"{d}/load-test.json"))["runs"][0]; c = r["client"]
ts = lambda s: dt.datetime.fromisoformat(s[:26].rstrip("Z") + "+00:00").timestamp()
t0, t1 = ts(r["started_at"]), ts(r["finished_at"]); m1 = t0 + meas
def api(path, **p): return json.load(urllib.request.urlopen("http://localhost:9090/api/v1/" + path + "?" + urllib.parse.urlencode(p)))["data"]["result"]
def rng(q, a, b, step=5): return [(x["metric"], [(float(t), float(v)) for t, v in x["values"] if v != "NaN"]) for x in api("query_range", query=q, start=a, end=b, step=step)]
def inst(q, at): return [(x["metric"], float(x["value"][1])) for x in api("query", query=q, time=at) if x["value"][1] != "NaN"]
f = lambda x: "-" if x is None else (f"{x:.2f}" if abs(x) < 100 else f"{x:.0f}")
out = [f"# Next-choke summary — {r['label']}\n", f"measurement {r['started_at']} .. +{meas}s; whole run to {r['finished_at']}\n",
       f"client: offered {c['offered']} accepted {c['accepted']} completed {c['completed']} pending {c['pending_at_drain']}; server saga p50/p95/max = "
       f"{c['server_completion_latency']['p50_ms']/1000:.1f}/{c['server_completion_latency']['p95_ms']/1000:.1f}/{c['server_completion_latency']['max_ms']/1000:.1f} s\n"]
def table(title, header, rows):
    out.append(f"\n## {title}\n\n| " + " | ".join(header) + " |\n|" + "---|" * len(header))
    out.extend("| " + " | ".join(str(x) for x in row) + " |" for row in rows)
SERVICES = ["orchestrator-service", "payment-service", "inventory-service", "order-service"]
# 1. every service's DB pool
rows = []
for svc in SERVICES:
    series = rng(f'go_sql_in_use_connections{{service="{svc}"}}', t0, m1)
    vals = [v for _, s in series for _, v in s]
    capv = [v for _, s in rng(f'go_sql_max_open_connections{{service="{svc}"}}', t0, m1) for _, v in s]
    wc = inst(f'increase(go_sql_wait_count_total{{service="{svc}"}}[{meas}s])', m1); wd = inst(f'increase(go_sql_wait_duration_seconds_total{{service="{svc}"}}[{meas}s])', m1)
    n = wc[0][1] if wc else 0; w = wd[0][1] if wd else 0
    full = sum(1 for v, cp in zip(vals, capv or [0] * len(vals)) if cp and v >= cp - 1) / max(len(vals), 1)
    rows.append((svc, f"{min(capv):.0f}->{max(capv):.0f}" if capv else "-", f(max(vals) if vals else None), f(sum(vals) / len(vals) if vals else None), f"{full*100:.0f}%", f(n), f(w), f(w / n * 1000 if n else 0) + " ms"))
table("Database pools (measurement window)", ["service", "cap (min->max)", "in-use max", "in-use mean", "samples within 1 of cap", "blocked acquisitions", "total wait s", "avg wait / blocked"], rows)
# 2. scalers
rows = []
for name, q, lab in [("webhook workers active", "payment_webhook_workers_active", "service"), ("ready backlog", "payment_webhook_ready_backlog", "service"),
                     ("oldest ready age s", "payment_webhook_oldest_ready_age_seconds", "service"), ("worker busy ratio", "payment_webhook_workers_busy_ratio", "service"),
                     ("orchestrator pool cap", "orchestrator_db_pool_cap", "service"), ("pool acquire wait s (avg per blocked)", "orchestrator_db_pool_acquire_wait_seconds", "service"),
                     ("pool scale-up held (guard)", "orchestrator_db_pool_scale_up_held", "service"), ("db probe latency s", "orchestrator_db_probe_latency_seconds", "service"),
                     ("db probe baseline s", "orchestrator_db_probe_baseline_seconds", "service"), ("ungranted locks", "orchestrator_db_ungranted_locks", "service"),
                     ("live connections to orchestrator_db", "orchestrator_db_live_connections", "service")]:
    for m, s in rng(q, t0, t1 + 60):
        v = [x for _, x in s]
        if v: rows.append((name, f(min(v)), f(max(v)), f(v[-1])))
table("Autoscaler gauges (whole run + 60 s)", ["metric", "min", "max", "last"], rows)
ev = inst(f'sum by (direction)(increase(payment_webhook_scale_events_total[{int(t1+60-t0)}s]))', t1 + 60) + inst(f'sum by (direction)(increase(orchestrator_db_pool_scale_events_total[{int(t1+60-t0)}s]))', t1 + 60)
table("Scale events in the run", ["series", "direction", "count"], [(m.get("__name__", "") or "events", m.get("direction"), f(v)) for m, v in ev] or [("none", "", "0")])
# 3. cpu
rows = []
for svc in ["orchestrator", "payment", "inventory", "order"]:
    cc = [v for _, s in rng(f'sum(rate(container_cpu_usage_seconds_total{{service="{svc}-containers",name=~".*app-.*"}}[30s]))*100', t0, m1) for _, v in s]
    hh = [v for _, s in rng(f'100-avg(rate(node_cpu_seconds_total{{service="{svc}-host",mode="idle"}}[30s]))*100', t0, m1) for _, v in s]
    rows.append((svc, f(sum(cc) / len(cc)) if cc else "-", f(max(cc)) if cc else "-", f(sum(hh) / len(hh)) if hh else "-", f(max(hh)) if hh else "-", f"{sum(1 for v in hh if v > 70) * 5} s"))
hh = [v for _, s in rng('100-avg(rate(node_cpu_seconds_total{service="broker-host",mode="idle"}[30s]))*100', t0, m1) for _, v in s]
rows.append(("broker host", "-", "-", f(sum(hh) / len(hh)) if hh else "-", f(max(hh)) if hh else "-", f"{sum(1 for v in hh if v > 70) * 5} s"))
table("CPU (container = % of one core; host = % of 2 vCPU)", ["service", "container mean", "container max", "host mean", "host max", "host time >70%"], rows)
# 4. webhook flow, queues, outbox, saga
arr = inst(f'sum(rate(http_requests_total{{service="payment-service",route="POST /v1/webhooks/razorpay"}}[{meas}s]))', m1); proc = inst(f'sum(rate(payment_webhook_events_processed_total[{meas}s]))', m1)
out.append(f"\n## Webhook flow\n\narrivals/s {f(arr[0][1] if arr else None)}; processed/s {f(proc[0][1] if proc else None)}")
rows = []
for kind in ("ready", "unacked"):
    for m, s in rng(f"rabbitmq_detailed_queue_messages_{kind}", t0, t1 + 60):
        v = [x for _, x in s]
        if v and max(v) >= 5: rows.append((m.get("queue"), kind, f(max(v))))
table("RabbitMQ queues (max >= 5)", ["queue", "kind", "max"], rows or [("(none)", "", "")])
rows = []
for name, q in [("outbox pending (orchestrator)", 'outbox_pending_rows{service="orchestrator-service"}'), ("outbox oldest age s (orchestrator)", 'outbox_oldest_pending_age_seconds{service="orchestrator-service"}'),
                ("outbox pending (payment)", 'outbox_pending_rows{service="payment-service"}'), ("outbox oldest age s (payment)", 'outbox_oldest_pending_age_seconds{service="payment-service"}'),
                ("sagas RUNNING", 'sum(sagas_by_status{status="RUNNING"})'), ("saga oldest active age s", "max(saga_oldest_active_age_seconds)"),
                ("reply consumers connected", 'rabbitmq_consumer_connected{queue="distri.orchestrator.replies"}')]:
    for m, s in rng(q, t0, t1 + 60):
        v = [x for _, x in s]
        if v: rows.append((name, f(max(v))))
table("Backlogs (max, whole run + 60 s)", ["metric", "max"], rows)
rows = []
for name, q in [("orchestrator GET polling /s", 'sum(rate(http_requests_total{service="orchestrator-service",route="GET /v1/checkouts/{orderID}"}[%ds]))' % meas),
                ("orchestrator POST /v1/checkouts p95 s", 'histogram_quantile(.95,sum by (le)(increase(http_request_duration_seconds_bucket{service="orchestrator-service",route="POST /v1/checkouts"}[%ds])))' % meas),
                ("orchestrator DB statements/s", 'sum(rate(db_query_duration_seconds_count{service="orchestrator-service"}[%ds]))' % meas),
                ("payment DB statements/s", 'sum(rate(db_query_duration_seconds_count{service="payment-service"}[%ds]))' % meas),
                ("inventory handler p95 s", 'histogram_quantile(.95,sum by (le)(increase(rabbitmq_consume_duration_seconds_bucket{queue="distri.inventory.commands"}[%ds])))' % meas),
                ("order handler p95 s", 'histogram_quantile(.95,sum by (le)(increase(rabbitmq_consume_duration_seconds_bucket{queue="distri.order.commands"}[%ds])))' % meas),
                ("payment-commands handler p95 s", 'histogram_quantile(.95,sum by (le)(increase(rabbitmq_consume_duration_seconds_bucket{queue="distri.payment.commands"}[%ds])))' % meas),
                ("orchestrator reply handler p95 s", 'histogram_quantile(.95,sum by (le)(increase(rabbitmq_consume_duration_seconds_bucket{queue="distri.orchestrator.replies"}[%ds])))' % meas),
                ("DB statement p95 s, orchestrator", 'histogram_quantile(.95,sum by (le)(increase(db_query_duration_seconds_bucket{service="orchestrator-service"}[%ds])))' % meas),
                ("DB statement p95 s, payment", 'histogram_quantile(.95,sum by (le)(increase(db_query_duration_seconds_bucket{service="payment-service"}[%ds])))' % meas)]:
    v = inst(q, m1); rows.append((name, f(v[0][1]) if v else "-"))
table("Rates and handler times (measurement window)", ["metric", "value"], rows)
open(f"{d}/choke-summary.md", "w").write("\n".join(out) + "\n"); print("\n".join(out))
