#!/usr/bin/env python3
"""Add Grafana annotations for the faults recorded in a chaos-test.json (run it on the laptop after Test 3).

  obs-annotate-chaos.py final/results/<run id>/chaos-test.json <run id>
"""
import base64, datetime as dt, json, os, sys, urllib.request

path, run = sys.argv[1:3]
grafana = os.environ.get("GRAFANA_URL", "http://127.0.0.1:3000")
auth = base64.b64encode(f"admin:{os.environ.get('GRAFANA_ADMIN_PASSWORD', 'admin')}".encode()).decode()


def ms(iso):
    return int(dt.datetime.fromisoformat(iso).timestamp() * 1000)


for name, r in json.load(open(path))["results"].items():
    if "fault_started_at" not in r:
        continue
    body = {"time": ms(r["fault_started_at"]), "timeEnd": ms(r["fault_ended_at"]), "tags": ["test_run", run, "fault", name],
            "text": f"{name}: {'PASS' if r['pass'] else 'FAIL'} (accepted {r.get('accepted')}, in flight {r.get('sagas_in_flight_at_fault')})"}
    request = urllib.request.Request(grafana + "/api/annotations", data=json.dumps(body).encode(), method="POST",
                                     headers={"Content-Type": "application/json", "Authorization": "Basic " + auth})
    urllib.request.urlopen(request, timeout=10).close()
    print("annotated", name)
