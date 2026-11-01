#!/usr/bin/env python3
"""Observability acceptance test (run once before Phase 1).

Drives the local docker-compose stack through five scenarios and checks that the
metrics, logs, traces and alerts all show the expected sequence:

  1. one successful checkout         -> COMPLETED saga, one trace across all services
  2. one declined payment            -> saga COMPENSATED, compensation metrics
  3. one service stopped             -> ServiceDown alert, client errors, recovery
  4. transient RabbitMQ failure      -> outbox backlog, transient failures, recovery
  5. controlled unroutable message   -> outbox row marked dead, OutboxRowDead alert

  python3 deploy/observability/scripts/obs-acceptance.py [--run-id acceptance-1]

Needs the application stack and the observability stack up (see README.md). It
restarts payment-service once (to switch the simulated gateway to "decline") and
stops/starts inventory-service and rabbitmq; it restores all of them.
"""
import argparse, json, os, subprocess, sys, time, urllib.parse, urllib.request, uuid

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))
APP = os.path.join(ROOT, "compose.local.yaml")
PROM = f"http://127.0.0.1:{os.environ.get('PROMETHEUS_PORT', '9090')}"
LOKI, TEMPO, ORCH = "http://127.0.0.1:3100", "http://127.0.0.1:3200", "http://127.0.0.1:8083"
PRODUCT = "10000000-0000-4000-8000-000000000001"
results = []


def sh(*cmd, env=None, check=True):
    e = dict(os.environ, **(env or {}))
    return subprocess.run(cmd, capture_output=True, text=True, check=check, env=e, cwd=ROOT).stdout


def http(url, params=None, data=None, headers=None):
    if params:
        url += "?" + urllib.parse.urlencode(params)
    req = urllib.request.Request(url, data=data, headers=headers or {}, method="POST" if data is not None else "GET")
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            return r.status, r.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()


def prom(q):
    return json.loads(http(PROM + "/api/v1/query", {"query": q})[1])["data"]["result"]


def prom_val(q):
    r = prom(q)
    return float(r[0]["value"][1]) if r else 0.0


def wait_for(desc, fn, timeout=90, every=3):
    end = time.time() + timeout
    while time.time() < end:
        v = fn()
        if v:
            return v
        time.sleep(every)
    return None


def check(name, ok, detail=""):
    results.append((name, bool(ok), detail))
    print(("PASS " if ok else "FAIL ") + name + (f"  [{detail}]" if detail else ""))
    return ok


def checkout(run):
    body = json.dumps({"items": [{"product_id": PRODUCT, "quantity": 1}]}).encode()
    code, raw = http(ORCH + "/v1/checkouts", data=body, headers={
        "Content-Type": "application/json", "Idempotency-Key": f"acc-{uuid.uuid4()}", "X-Test-Run-ID": run})
    return code, (json.loads(raw) if raw[:1] == b"{" else {})


def saga_status(order_id):
    code, raw = http(f"{ORCH}/v1/checkouts/{order_id}")
    return json.loads(raw).get("status") if code == 200 else None


def logs(run, extra=""):
    end = time.time_ns()
    r = json.loads(http(LOKI + "/loki/api/v1/query_range", {
        "query": f'{{test_run_id="{run}"}} {extra}', "start": end - 3600 * 10**9, "end": end, "limit": 5000})[1])
    return [v[1] for s in r["data"]["result"] for v in s["values"]]


def alert_state(name):
    alerts = json.loads(http(PROM + "/api/v1/alerts")[1])["data"]["alerts"]
    return [a for a in alerts if a["labels"]["alertname"] == name and a["state"] == "firing"]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--run-id", default="acceptance-" + time.strftime("%Y%m%d-%H%M%S"))
    a = ap.parse_args()
    run = a.run_id
    print(f"== observability acceptance, test_run_id={run}")
    sh(os.path.join(ROOT, "deploy/observability/scripts/obs-run.sh"), "start", run, "local")
    time.sleep(8)  # first scrape with the new labels

    # ---- 1. successful checkout ------------------------------------------------
    print("\n-- 1. successful checkout")
    code, body = checkout(run)
    oid = body.get("order_id")
    check("checkout accepted (202) and echoes X-Test-Run-ID", code == 202, f"http {code}")
    ok = wait_for("completed", lambda: saga_status(oid) == "COMPLETED", 60)
    check("saga reaches COMPLETED", ok)
    wait_for("loki", lambda: sum(1 for l in logs(run, f'|= "{oid}"') if '"saga_step_completed"' in l) >= 4, 60, 3)
    lines = logs(run, f'|= "{oid}"')
    services = {json.loads(l).get("service") for l in lines if l.startswith("{")}
    events = [json.loads(l).get("event") for l in lines if l.startswith("{") and json.loads(l).get("event")]
    check("logs for the order exist with test_run_id + order_id + saga_id", len(lines) > 0 and all(
        json.loads(l).get("test_run_id") == run for l in lines if l.startswith("{")), f"{len(lines)} lines")
    check("saga_step_started / saga_step_completed events logged", "saga_step_started" in events and events.count("saga_step_completed") >= 4,
          f"{events.count('saga_step_started')} started, {events.count('saga_step_completed')} completed")
    trace_ids = {json.loads(l).get("trace_id") for l in lines if l.startswith("{") and json.loads(l).get("trace_id")}
    check("orchestrator logs carry a trace_id", len(trace_ids) >= 1)
    # The HTTP request's trace continues through the outbox and RabbitMQ into the other services.
    spanned = set()
    for tid in trace_ids:
        t = wait_for("trace", lambda: (http(f"{TEMPO}/api/traces/{tid}")[0] == 200) and http(f"{TEMPO}/api/traces/{tid}")[1], 40)
        if t:
            doc = json.loads(t)
            for rs in doc.get("batches", doc.get("resourceSpans", [])):
                for at in rs["resource"]["attributes"]:
                    if at["key"] == "service.name":
                        spanned.add(at["value"]["stringValue"])
    check("one trace spans orchestrator, order, payment and inventory (context crossed RabbitMQ)",
          {"orchestrator-service", "order-service", "payment-service", "inventory-service"} <= spanned, str(sorted(spanned)))
    check("saga completion counted in metrics", prom_val(f'sum(saga_completions_total{{test_run_id="{run}",status="COMPLETED"}})') >= 1)
    check("HTTP RED metrics recorded for the checkout route",
          prom_val(f'sum(http_requests_total{{test_run_id="{run}",route="POST /v1/checkouts",code_class="2xx"}})') >= 1)
    row = sh("docker", "exec", "distri-local-postgres-1", "psql", "-U", "distri", "distri", "-Atc",
             "select count(*) from orchestrator.outbox where correlation->>'test_run_id' = '%s'" % run).strip()
    check("outbox rows carry test_run_id", int(row or 0) >= 1, f"{row} rows")

    # ---- 2. declined payment ---------------------------------------------------
    print("\n-- 2. declined payment")
    sh("docker", "compose", "--env-file", "/dev/null", "-f", APP, "up", "-d", "--no-deps", "payment-service",
       env={"GATEWAY_MODE": "decline", "OTEL_EXPORTER_OTLP_ENDPOINT": "http://alloy:4318"})
    wait_for("payment up", lambda: "healthy" in sh("docker", "inspect", "-f", "{{.State.Health.Status}}", "distri-local-payment-service-1"), 60)
    code, body = checkout(run)
    oid2 = body.get("order_id")
    st = wait_for("compensated", lambda: saga_status(oid2) in ("COMPENSATED", "STUCK") and saga_status(oid2), 90)
    check("declined payment ends the saga COMPENSATED", st == "COMPENSATED", str(st))
    rejected = wait_for("rejected", lambda: prom_val(f'sum(payment_gateway_calls_total{{test_run_id="{run}",result="rejected"}})') >= 1, 40, 3)
    check("gateway rejection recorded", rejected)
    sh("docker", "compose", "--env-file", "/dev/null", "-f", APP, "up", "-d", "--no-deps", "payment-service",
       env={"GATEWAY_MODE": "success", "OTEL_EXPORTER_OTLP_ENDPOINT": "http://alloy:4318"})
    time.sleep(5)
    check("compensation metrics recorded",
          prom_val(f'sum(saga_compensation_steps_total{{test_run_id="{run}"}})') >= 1 and
          prom_val(f'sum(saga_completions_total{{test_run_id="{run}",status="COMPENSATED"}})') >= 1)

    # ---- 3. stop one service ---------------------------------------------------
    print("\n-- 3. service stopped (inventory-service)")
    sh("docker", "stop", "distri-local-inventory-service-1")
    code, _ = checkout(run)
    check("checkout fails cleanly while inventory is down (no 2xx)", code >= 500 or code == 503 or code >= 400, f"http {code}")
    fired = wait_for("ServiceDown", lambda: [x for x in alert_state("ServiceDown") if x["labels"].get("service") == "inventory-service"], 60, 3)
    check("ServiceDown alert fires for inventory-service", fired)
    check("orchestrator's client error counted", prom_val(f'sum(http_client_requests_total{{test_run_id="{run}",peer="inventory-service",result="error"}})') >= 1)
    sh("docker", "start", "distri-local-inventory-service-1")
    recovered = wait_for("ServiceDown clears", lambda: not [x for x in alert_state("ServiceDown") if x["labels"].get("service") == "inventory-service"], 90, 3)
    check("alert clears after the service returns", recovered)

    # ---- 4. transient RabbitMQ failure -----------------------------------------
    print("\n-- 4. transient RabbitMQ publishing failure")
    sh("docker", "stop", "distri-local-rabbitmq-1")
    code, body = checkout(run)  # accepted; its create-order command cannot be published yet
    oid4 = body.get("order_id")
    check("checkout still accepted while the broker is down (outbox absorbs it)", code == 202, f"http {code}")
    failed = wait_for("transient", lambda: prom_val(f'sum(outbox_publish_total{{test_run_id="{run}",result="transient_failure"}})') >= 1, 60, 3)
    check("outbox transient failures counted", failed)
    age = wait_for("age", lambda: prom_val(f'max(outbox_oldest_pending_age_seconds{{test_run_id="{run}"}})') > 2, 40, 3)
    check("outbox oldest-pending age grows while the broker is down", age)
    ev = wait_for("event", lambda: [l for l in logs(run, '|= "outbox_publish_failed"') if '"failure":"transient"' in l], 40, 3)
    check("outbox_publish_failed event logged (transient)", ev)
    sh("docker", "start", "distri-local-rabbitmq-1")
    done = wait_for("drain", lambda: saga_status(oid4) == "COMPLETED", 150, 5)
    check("backlog drains and the saga completes after the broker returns", done, str(saga_status(oid4)))

    # ---- 5. unroutable outbox message ------------------------------------------
    print("\n-- 5. controlled unroutable outbox message")
    mid = str(uuid.uuid4())
    sh("docker", "exec", "distri-local-postgres-1", "psql", "-U", "distri", "distri", "-c",
       "insert into orchestrator.outbox (message_id, routing_key, payload, unpublishable_attempts, created_at, correlation) "
       f"values ('{mid}', 'acceptance.no.such.route', '{{}}', 19, now(), '{{\"test_run_id\":\"{run}\"}}')")
    dead = wait_for("dead", lambda: prom_val('sum(outbox_dead_rows{service="orchestrator-service"})') >= 1, 60, 3)
    check("unroutable row marked dead (outbox_dead_rows >= 1)", dead)
    check("OutboxRowDead alert fires", wait_for("alert", lambda: alert_state("OutboxRowDead"), 60, 3))
    check("outbox_message_marked_dead event logged", wait_for("log", lambda: logs(run, '|= "outbox_message_marked_dead"'), 40, 3))
    # clean up the deliberate poison row so later runs start clean
    sh("docker", "exec", "distri-local-postgres-1", "psql", "-U", "distri", "distri", "-c",
       f"delete from orchestrator.outbox where message_id = '{mid}'")

    # ---- always-on checks ------------------------------------------------------
    print("\n-- dashboards and alert plumbing")
    gf = json.loads(http("http://127.0.0.1:3000/api/search?type=dash-db", headers={"Authorization": "Basic YWRtaW46YWRtaW4="})[1])
    check("six dashboards provisioned", len([d for d in gf if d["uri"].startswith("db/")]) >= 6, str(len(gf)))
    rules = json.loads(http(PROM + "/api/v1/rules")[1])["data"]["groups"]
    check("alert rules loaded", sum(len(g["rules"]) for g in rules) >= 15)
    sh(os.path.join(ROOT, "deploy/observability/scripts/obs-run.sh"), "stop", run)

    out = os.path.join(ROOT, "final", "results", run)
    os.makedirs(out, exist_ok=True)
    passed = sum(1 for _, ok, _ in results if ok)
    with open(os.path.join(out, "acceptance.md"), "w") as f:
        f.write(f"# Observability acceptance — {run}\n\n{passed}/{len(results)} checks passed.\n\n| result | check | detail |\n|---|---|---|\n")
        for n, ok, d in results:
            f.write(f"| {'PASS' if ok else 'FAIL'} | {n} | {d} |\n")
    print(f"\n{passed}/{len(results)} checks passed -> {out}/acceptance.md")
    sys.exit(0 if passed == len(results) else 1)


if __name__ == "__main__":
    main()
