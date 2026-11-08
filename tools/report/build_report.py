#!/usr/bin/env python3
"""Builds test-results/local/full-test-report.md from the artefacts on disk.

Reads, never invents:
  test-results/local/integration/<module>/*.log   go test -v output (testing/run-integration.sh)
  test-results/local/mutation-checks.json         mutation checks (recorded by hand as they were run)
  test-results/local/chaos/chaos-test.json        fault-injection scenarios (testing/chaos/chaos.py)
  test-results/local/load-followups/*.json        load follow-up runs (testing/chaos/load_followups.py)
  test-results/local/correctness-three-modes/     the 22-phase suite (report read, not re-run here)
  testing/report/findings.md                      reviewed findings and open items (hand-written)
Standard library only.
"""
import glob
import json
import os
import re
import datetime

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
RES = os.path.join(ROOT, "test-results", "local")
RESULT = re.compile(r"^--- (PASS|FAIL|SKIP): (\S+)")


def read_json(path):
    try:
        with open(path) as handle:
            return json.load(handle)
    except (OSError, ValueError):
        return None


def integration_section():
    rows, totals = [], {"PASS": 0, "FAIL": 0, "SKIP": 0}
    for log in sorted(glob.glob(os.path.join(RES, "integration", "*", "*.log"))):
        if log.endswith(".build.log"):
            continue
        module = os.path.basename(os.path.dirname(log))
        package = os.path.basename(log)[:-4]
        counts = {"PASS": 0, "FAIL": 0, "SKIP": 0}
        with open(log, errors="replace") as handle:
            for line in handle:
                match = RESULT.match(line)  # top-level tests only (no leading space)
                if match:
                    counts[match.group(1)] += 1
        for key in totals:
            totals[key] += counts[key]
        rows.append(f"| {module} | {package} | {counts['PASS']} | {counts['FAIL']} | {counts['SKIP']} |")
    header = "| Module | Package | Pass | Fail | Skip |\n|---|---|---:|---:|---:|\n"
    return totals, header + "\n".join(rows)


def mutation_section():
    data = read_json(os.path.join(RES, "mutation-checks.json"))
    if not data:
        return "_no mutation data_"
    caught = sum(1 for m in data["mutations"] if m["result"] == "caught")
    survived = len(data["mutations"]) - caught
    lines = [f"{len(data['mutations'])} valid mutations: **{caught} caught, {survived} survived**. {data['method']}", "",
             "| ID | Area | Mutation | Result | Caught by / why |", "|---|---|---|---|---|"]
    for m in data["mutations"]:
        detail = "; ".join(m.get("caught_by", [])) if m["result"] == "caught" else m.get("explanation", "")
        if m["result"] == "caught" and m.get("explanation"):
            detail += f" — {m['explanation']}"
        lines.append(f"| {m['id']} | {m['area']} | {m['mutation']} | {m['result']} | {detail} |")
    return "\n".join(lines)


def chaos_section():
    data = read_json(os.path.join(RES, "chaos", "chaos-test.json"))
    if not data:
        return "_not run_"
    lines = ["| Scenario | Result | Accepted | In flight at fault | Final saga status | Unfinished (explained) | Duplicate effects | Seconds |", "|---|---|---:|---:|---|---|---|---:|"]
    for name, r in data["results"].items():
        if "harness_error" in r:
            lines.append(f"| {name} | **FAIL (harness)** | | | {r['harness_error']} | | | {r.get('seconds', '')} |")
            continue
        dup = r.get("duplicate_effects")
        lines.append("| {} | {} | {} | {} | {} | {} | {} | {} |".format(
            name, "PASS" if r["pass"] else "**FAIL**", r.get("accepted", ""), r.get("sagas_in_flight_at_fault", ""),
            json.dumps(r.get("saga_status", {})),
            f"{r.get('unfinished', 0)} ({r.get('unfinished_with_unknown_payment', 0)})" if "unfinished" in r else "",
            ("n/a" if dup is None else "none" if all(v == 0 for v in dup.values()) else json.dumps(dup)),
            r.get("seconds", "")))
    extra = []
    for name, r in data["results"].items():
        if "retry_tiers_that_held_the_message" in r:
            extra.append(f"- `{name}`: retry tiers that held the message {r['retry_tiers_that_held_the_message']}, peak depth {r['peak_depth_per_retry_queue']}, finished {r['seconds_to_finish_after_restore']} s after the table was restored.")
        if "relay_or_sweeper_log_events_per_replica" in r:
            extra.append(f"- `{name}`: relay/sweeper log events per replica {r['relay_or_sweeper_log_events_per_replica']}.")
    return "\n".join(lines + [""] + extra)


def load_section():
    files = sorted(glob.glob(os.path.join(RES, "load-followups", "*.json")))
    if not files:
        return "_not run_"
    lines = ["Local Docker stack, simulated gateway (success, 200 ms), closed-loop clients. Simulated-dependency throughput, **not** real Postgres-backed or Razorpay payment throughput. Accepted and completed rates are reported separately; every run drained to 0 pending.", "",
             "| Run | Case | Workload | Clients | Offered | Accepted/s | Completed/s | E2E p50 ms | E2E p95 ms | E2E p99 ms | Paid / cancelled | 409 | 5xx | Deadlocks | Lock-wait max | Invariants | DB MB before -> after |",
             "|---|---|---|---:|---:|---:|---:|---:|---:|---:|---|---:|---:|---:|---:|---|---|"]
    for path in files:
        data = read_json(path)
        group = os.path.basename(path)[:-5]
        for r in data["runs"]:
            mix = r.get("status_mix") or {}
            lines.append("| {} / {} | {} | {} | {} | {} | {:.2f} | {:.2f} | {:.0f} | {:.0f} | {:.0f} | {} / {} | {} | {} | {} | {} | {} | {:.1f} -> {:.1f} |".format(
                group, r["label"], r["case"], r["workload"], r["clients"], r["offered"], r["accepted_per_s"], r["completed_per_s"],
                r["e2e_ms"]["p50_ms"], r["e2e_ms"]["p95_ms"], r["e2e_ms"]["p99_ms"], r["paid"], r["cancelled"],
                mix.get("business_conflict_409", 0), mix.get("server_5xx", 0), r["deadlocks"], r["lock_waiting_sessions_max"],
                "pass" if r["invariants_all_passed"] else "FAIL: " + ", ".join(r["invariants_failed"]),
                r["db_bytes_before"] / 1e6, r["db_bytes_after"] / 1e6))
    lines += ["", "The worker-concurrency rows differ in one variable only: payment-service `WORKER_CONCURRENCY` (4, 8, 16), 30 clients, 60 s."]
    return "\n".join(lines)


def correctness_section():
    path = os.path.join(RES, "correctness-three-modes", "correctness-test.md")
    try:
        text = open(path).read()
    except OSError:
        return "_report not found_"
    runs = re.findall(r"## Run — (\S+).*?Summary: \*\*(.*?)\*\*", text, re.S)
    return "\n".join(f"- `{name}`: {summary}" for name, summary in runs) + f"\n\nFull per-phase report: `{os.path.relpath(path, ROOT)}` (read here, not re-run by this script)."


def main():
    totals, table = integration_section()
    findings_path = os.path.join(ROOT, "testing", "report", "findings.md")
    findings = open(findings_path).read() if os.path.exists(findings_path) else "_none recorded_"
    now = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%d %H:%M UTC")
    report = f"""# Full test report

Generated {now} by `testing/report/build_report.py` from artefacts on disk.

## 1. Go tests with every database-gated integration test enabled

Run with `testing/run-integration.sh` against the dedicated `distri_test` database, race detector on.
Top-level tests only (subtests are not double counted).

**{totals['PASS']} passed, {totals['FAIL']} failed, {totals['SKIP']} skipped.**

{table}

## 2. Mutation checks (do the new tests fail when the code is broken?)

{mutation_section()}

## 3. Fault injection on the live local stack

`testing/chaos/chaos.py`. Only checkouts the orchestrator accepted (202) are judged. A scenario that hit an idle system (no saga in flight at the fault) fails as inconclusive.

{chaos_section()}

## 4. Load follow-ups

{load_section()}

## 5. End-to-end correctness suite (22 phases, three gateway modes)

{correctness_section()}

## 6. Findings and open items

{findings}
"""
    out = os.path.join(RES, "full-test-report.md")
    with open(out, "w") as handle:
        handle.write(report)
    print(out)


if __name__ == "__main__":
    main()
