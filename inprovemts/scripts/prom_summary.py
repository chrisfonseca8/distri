#!/usr/bin/env python3
"""Summarise one run's timeline from the local Prometheus (scraping the staging hosts over SSH tunnels).
usage: prom_summary.py <data-dir> [warmup_seconds=0 (the report's started_at already excludes warm-up)] [measure_seconds=180]
Reads <data-dir>/load-test.json for the run's started_at/finished_at; writes <data-dir>/prom-summary.md.
Measurement window = started_at+warmup .. +measure. 'whole' = started_at .. finished_at (includes warm-up and drain).
"""
import json, sys, urllib.parse, urllib.request, datetime as dt
d = sys.argv[1]; warm = int(sys.argv[2]) if len(sys.argv) > 2 else 0; meas = int(sys.argv[3]) if len(sys.argv) > 3 else 180
r = json.load(open(f"{d}/load-test.json"))["runs"][0]
ts = lambda s: dt.datetime.fromisoformat(s[:26].rstrip("Z") + "+00:00").timestamp()
t0, t1 = ts(r["started_at"]), ts(r["finished_at"]); m0, m1 = t0 + warm, t0 + warm + meas
def api(path, **p):
    return json.load(urllib.request.urlopen("http://localhost:9090/api/v1/" + path + "?" + urllib.parse.urlencode(p)))["data"]["result"]
def rng(q, a, b, step=5):  # list of (labels, [values])
    return [(x["metric"], [float(v) for _, v in x["values"] if v != "NaN"]) for x in api("query_range", query=q, start=a, end=b, step=step)]
def inst(q, at): return [(x["metric"], float(x["value"][1])) for x in api("query", query=q, time=at) if x["value"][1] != "NaN"]
out = [f"# Prometheus timeline summary — {r['label']}\n",
       f"whole run {r['started_at']} .. {r['finished_at']}; measurement window = +{warm}s .. +{warm+meas}s of the run.\n"]
def table(title, header, rows):
    out.append(f"\n## {title}\n\n| " + " | ".join(header) + " |\n|" + "---|" * len(header))
    for row in rows: out.append("| " + " | ".join(str(c) for c in row) + " |")
f = lambda x: "-" if x is None else (f"{x:.2f}" if abs(x) < 100 else f"{x:.0f}")
def series_stats(q, label, a=m0, b=m1, whole=(t0, t1)):
    rows = []
    for m, v in rng(q, a, b):
        w = [x for _, vs in rng(q, *whole) for x in vs] if False else None
        rows.append((m.get(label, "-"), f(max(v) if v else None), f(sum(v) / len(v) if v else None)))
    return rows
def whole_max(q, label):
    res = {}
    for m, v in rng(q, t0, t1 + 120):
        res[m.get(label, "-")] = max(v) if v else None
    return res
# queues (all non-retry queues that were ever non-zero)
rows = []
for kind in ("ready", "unacked"):
    mx = whole_max(f"rabbitmq_detailed_queue_messages_{kind}", "queue")
    mw = {m.get("queue"): (sum(v) / len(v) if v else 0) for m, v in rng(f"rabbitmq_detailed_queue_messages_{kind}", m0, m1)}
    for q, v in sorted(mx.items()):
        if v: rows.append((q, kind, f(v), f(mw.get(q))))
table("RabbitMQ queues (max over whole run + 2 min, mean over measurement window)", ["queue", "kind", "max", "mean in window"], rows or [("(all zero)", "", "", "")])
table("Consumers connected (autoscale), max", ["queue", "max"], [(q, f(v)) for q, v in sorted(whole_max("rabbitmq_consumer_connected", "queue").items())])
rate = lambda q, lab: {m.get(lab, "-"): v for m, v in inst(q, m1)}
cons = rate(f'sum by (queue)(rate(rabbitmq_consume_total{{result="acked"}}[{meas}s]))', "queue")
p95c = rate(f'histogram_quantile(.95, sum by (le,queue)(increase(rabbitmq_consume_duration_seconds_bucket[{meas}s])))', "queue")
table("RabbitMQ consumers (measurement window)", ["queue", "acked/s", "handler p95 s"], [(q, f(cons[q]), f(p95c.get(q))) for q in sorted(cons) if cons[q] > 0])
dl = inst(f"sum by (queue,reason)(increase(rabbitmq_dead_letters_total[{int(t1+120-t0)}s]))", t1 + 120)
table("Dead letters during run", ["queue", "reason", "count"], [(m.get("queue"), m.get("reason"), f(v)) for m, v in dl if v] or [("none", "", "0")])
rt = inst(f"sum by (queue)(increase(rabbitmq_retries_total[{int(t1+120-t0)}s]))", t1 + 120)
table("Retry-queue republishes during run", ["queue", "count"], [(m.get("queue"), f(v)) for m, v in rt if v] or [("none", "0")])
# backlogs
rows = []
for name, q, lab in [("outbox pending rows", "outbox_pending_rows", "service"), ("outbox oldest pending age s", "outbox_oldest_pending_age_seconds", "service"),
                     ("payment webhook backlog", "payment_webhook_backlog", "service"), ("payment webhook oldest pending age s", "payment_webhook_oldest_pending_age_seconds", "service"),
                     ("payment jobs by status (queued)", 'payment_jobs{status="queued"}', "service"), ("payment jobs (running)", 'payment_jobs{status="running"}', "service"),
                     ("payment jobs oldest queued age s", "payment_jobs_oldest_queued_age_seconds", "service"), ("saga oldest active age s", "saga_oldest_active_age_seconds", "service"),
                     ("sagas RUNNING", 'sagas_by_status{status="RUNNING"}', "service"), ("outbox dead rows", "outbox_dead_rows", "service")]:
    for k, v in whole_max(q, lab).items(): rows.append((name, k, f(v)))
table("Backlogs / ages (max over whole run + 2 min)", ["metric", "service", "max"], rows)
rows = []
for m, v in rng('sagas_by_current_step{status="RUNNING"}', t0, t1 + 120): rows.append((m.get("current_step", "-"), f(max(v) if v else None)))
table("Running sagas by current step (max)", ["current_step", "max"], rows)
# CPU
cpu = []
for svc in ("orchestrator", "payment", "inventory", "order"):
    q = f'sum(rate(container_cpu_usage_seconds_total{{service="{svc}-containers",name=~".*app-.*"}}[30s]))*100'
    v = [x for _, vs in rng(q, m0, m1) for x in vs]
    hq = f'100-avg(rate(node_cpu_seconds_total{{service="{svc}-host",mode="idle"}}[30s]))*100'
    h = [x for _, vs in rng(hq, m0, m1) for x in vs]
    over = (sum(1 for x in v if x > 70) * 5) if v else 0
    cpu.append((svc, f(sum(v) / len(v)) if v else "-", f(max(v)) if v else "-", f"{over}s", f(sum(h) / len(h)) if h else "-", f(max(h)) if h else "-"))
hq = '100-avg(rate(node_cpu_seconds_total{service="broker-host",mode="idle"}[30s]))*100'; h = [x for _, vs in rng(hq, m0, m1) for x in vs]
cpu.append(("broker (host)", "-", "-", "-", f(sum(h) / len(h)) if h else "-", f(max(h)) if h else "-"))
table("CPU in measurement window (container % of one core; host % of 2 vCPU; 'time >70%' = seconds the container exceeded 70% of one core)", ["service", "container mean", "container max", "time >70% of a core", "host mean", "host max"], cpu)
# latency histograms
def hist(title, metric, by, extra="", mn=0.0):
    q = f"histogram_quantile(.95, sum by (le,{by})(increase({metric}_bucket{extra}[{meas}s])))"
    c = f"sum by ({by})(increase({metric}_count{extra}[{meas}s]))"
    p = {tuple(m.get(b) for b in by.split(",")): v for m, v in inst(q, m1)}
    n = {tuple(m.get(b) for b in by.split(",")): v for m, v in inst(c, m1)}
    table(title, by.split(",") + ["n", "p95 s"], [(*k, f(n.get(k)), f(v)) for k, v in sorted(p.items()) if n.get(k, 0) > 0])
hist("Saga step duration p95 (orchestrator metric)", "saga_step_duration_seconds", "step")
hist("Saga end-to-end duration p95", "saga_duration_seconds", "status")
hist("HTTP handler p95 by service/route (inbound)", "http_request_duration_seconds", "service,route")
hist("HTTP client call p95 (outbound from service to peer)", "http_client_request_duration_seconds", "service,peer")
hist("DB statement p95 by service", "db_query_duration_seconds", "service")
hist("Outbox: row created -> confirmed publish p95", "outbox_publish_latency_seconds", "service")
hist("Payment gateway call p95", "payment_gateway_call_duration_seconds", "service")
hist("Payment webhook processing p95", "payment_webhook_processing_duration_seconds", "service")
tx = inst(f"sum by (service,name,result)(increase(db_transactions_total[{meas}s]))", m1)
table("DB transactions in window", ["service", "name", "result", "count"], [(m.get("service"), m.get("name"), m.get("result"), f(v)) for m, v in sorted(tx, key=lambda x: -x[1])[:14]])
open(f"{d}/prom-summary.md", "w").write("\n".join(out) + "\n"); print("\n".join(out))
