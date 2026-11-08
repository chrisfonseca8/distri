#!/usr/bin/env python3
"""Compact summary of load-test runs newer than a timestamp.

Usage: summarize_load.py <since-iso> <out.json> [source.json]
The runner's load-test.json accumulates every historical run; this keeps only the
runs started at or after <since-iso>, with the fields the report needs.
"""
import json
import sys

since, out = sys.argv[1], sys.argv[2]
source = sys.argv[3] if len(sys.argv) > 3 else "test-results/load-test/load-test.json"
data = json.load(open(source))
runs = []
for r in data["runs"]:
    if r["started_at"] < since:
        continue
    c, db, cfg = r["client"], r["database"], r["config"]
    runs.append({
        "label": r["label"], "case": cfg["case"], "workload": cfg["workload"], "clients": cfg["clients"],
        "gateway_latency_ms": cfg["gateway_latency"] / 1e6, "measurement_s": cfg["measurement"] / 1e9,
        "offered": c["offered"], "accepted_per_s": c["accepted_per_second"], "completed_per_s": c["completed_orders_per_second"],
        "paid": c["paid"], "cancelled": c["cancelled"], "pending_at_drain": c["pending_at_drain"],
        "e2e_ms": {k: c["end_to_end_latency"][k] for k in ("p50_ms", "p95_ms", "p99_ms", "max_ms")},
        "checkout_ms": {k: c["checkout_latency"][k] for k in ("p50_ms", "p95_ms", "p99_ms")},
        "max_in_flight_workflows": c["in_flight"]["max_workflows"],
        "status_mix": c.get("checkout_status_mix"),
        "invariants_all_passed": all(i["passed"] for i in r["invariants"]),
        "invariants_failed": [i["name"] for i in r["invariants"] if not i["passed"]],
        "deadlocks": db["deadlocks_after"] - db["deadlocks_before"],
        "lock_waiting_sessions_max": db.get("lock_waiting_sessions", {}).get("max") if isinstance(db.get("lock_waiting_sessions"), dict) else db.get("lock_waiting_sessions"),
        "db_bytes_before": db["database_bytes_before"], "db_bytes_after": db["database_bytes_after"],
        "runner_cpu_percent": c.get("runner_cpu_percent"),
    })
json.dump({"since": since, "runs": runs}, open(out, "w"), indent=2)
print(f"{len(runs)} run(s) -> {out}")
