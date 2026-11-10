#!/usr/bin/env python3
"""Ramp vs steady state for one autoscaled run. usage: ramp_summary.py <data-dir>   (reads load-test.json times, local Prometheus)"""
import json, sys, urllib.parse, urllib.request, datetime as dt
d = sys.argv[1]
r = json.load(open(f"{d}/load-test.json"))["runs"][0]
ts = lambda s: dt.datetime.fromisoformat(s[:26].rstrip("Z") + "+00:00").timestamp()
m0, t1 = ts(r["started_at"]), ts(r["finished_at"])
def api(p, **q): return json.load(urllib.request.urlopen("http://localhost:9090/api/v1/" + p + "?" + urllib.parse.urlencode(q)))["data"]["result"]
def rng(q, a, b, step=5):
    res = api("query_range", query=q, start=a, end=b, step=step)
    return [(float(t), float(v)) for t, v in (res[0]["values"] if res else []) if v != "NaN"]
hh = lambda t: dt.datetime.fromtimestamp(t, dt.timezone.utc).strftime("%H:%M:%S")
# load start = first sample where sagas are being started
st = rng("sum(rate(sagas_started_total[10s]))", m0 - 200, m0)
t0 = next(t for t, v in st if v > 5) - 5
print("load start (first started-rate > 5/s, minus one step)", hh(t0), " measured window starts", hh(m0), f"(+{m0-t0:.0f}s)")
end_ramp = t0 + 150
w = rng("payment_webhook_workers_active", t0 - 10, end_ramp)
first = {}
for t, v in w:
    n = int(v)
    if n >= 2 and n not in first: first[n] = t - t0
    if n >= 3 and n not in first: first[n] = t - t0
    if n >= 4 and n not in first: first[n] = t - t0
print("first time at N workers (s after load start):", {k: round(v) for k, v in sorted(first.items())})
back = rng("payment_webhook_ready_backlog", t0, end_ramp)
age = rng("payment_webhook_oldest_ready_age_seconds", t0, end_ramp)
pk = max(back, key=lambda x: x[1]); pa = max(age, key=lambda x: x[1])
print(f"peak claimable backlog {pk[1]:.0f} at +{pk[0]-t0:.0f}s ; oldest claimable wait max {pa[1]:.1f}s at +{pa[0]-t0:.0f}s")
clear = next((t - t0 for t, v in back if t > pk[0] and v < 20), None)
print("backlog < 20 first at +%ss after load start" % (round(clear) if clear is not None else "never"))
# saga duration histogram over completions in first ~85s (approximation of the first-minute cohort)
for lab, a, b in [("first-minute cohort (completions in +0..+85s)", t0, t0 + 85), ("steady (completions in measured window)", m0, m0 + 180)]:
    q = f'histogram_quantile(0.95, sum by (le)(increase(saga_duration_seconds_bucket{{service="orchestrator-service"}}[{int(b-a)}s]))) '
    res = api("query", query=q, time=b)
    print(f"saga p95 {lab}: {float(res[0]['value'][1]):.1f}s" if res else f"saga p95 {lab}: n/a")
# payment pool trigger evidence
cap = rng("payment_db_pool_cap", t0, m0 + 180)
inuse = rng("payment_db_pool_in_use", t0, m0 + 180)
wait = rng("payment_db_pool_acquire_wait_seconds", t0, m0 + 180)
blk = rng("payment_db_pool_blocked_acquisitions_per_second", t0, m0 + 180)
held = rng("payment_db_pool_scale_up_held", t0, m0 + 180)
n = min(len(cap), len(inuse), len(wait), len(blk))
near = hw = both = 0; 
for i in range(n):
    c = cap[i][1]; nf = inuse[i][1] >= 0.8 * c; h = blk[i][1] * 5 >= 10 and wait[i][1] >= 0.005
    near += nf; hw += h; both += (nf and h)
print(f"payment pool samples={n} cap range {min(x[1] for x in cap):.0f}-{max(x[1] for x in cap):.0f}; near-full(>=80%) {near}; elevated wait (>=5ms avg, >=10 blocked) {hw}; both {both}; max avg wait {max(x[1] for x in wait)*1000:.1f} ms; max blocked/s {max(x[1] for x in blk):.0f}; guard-held samples {sum(1 for x in held if x[1]>0)}; max in-use {max(x[1] for x in inuse):.0f}")
# longest consecutive run of both
run = best = 0
for i in range(n):
    c = cap[i][1]
    if inuse[i][1] >= 0.8 * c and blk[i][1] * 5 >= 10 and wait[i][1] >= 0.005: run += 1; best = max(best, run)
    else: run = 0
print("longest consecutive run of samples meeting both conditions:", best, "(needs 2 to scale)")
