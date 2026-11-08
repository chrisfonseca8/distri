#!/usr/bin/env bash
# Begin or end a labelled test run.
#
#   obs-run.sh start <test_run_id> [local|tunnel] [environment]
#   obs-run.sh stop  <test_run_id>
#
# start : points Prometheus at the services with test_run_id as a label, records
#         test-results/<id>/run.json and adds a Grafana annotation.
# stop  : stamps the end time. Use the same id in the X-Test-Run-ID header of every
#         request (loadgen, smoke scripts) so logs and traces carry it too.
set -euo pipefail
here="$(cd "$(dirname "$0")/.." && pwd)"; root="$(cd "$here/../.." && pwd)"
cmd="${1:?usage: obs-run.sh start|stop <test_run_id> ...}"; run="${2:?test_run_id required}"
out="$root/test-results/$run"; GRAFANA="${GRAFANA_URL:-http://127.0.0.1:3000}"
auth="admin:${GRAFANA_ADMIN_PASSWORD:-admin}"
now() { date -u +%Y-%m-%dT%H:%M:%SZ; }
annotate() { curl -fs -m 5 -u "$auth" -H 'Content-Type: application/json' -X POST "$GRAFANA/api/annotations" \
  -d "{\"text\":\"$1\",\"tags\":[\"test_run\",\"$run\"]}" -o /dev/null || echo "(grafana annotation skipped)"; }
case "$cmd" in
  start)
    mode="${3:-local}"; envname="${4:-$([ "$mode" = tunnel ] && echo staging || echo local)}"
    mkdir -p "$out"
    "$here/scripts/obs-targets.sh" "$run" "$mode" "$envname"
    printf '{\n  "test_run_id": "%s",\n  "mode": "%s",\n  "environment": "%s",\n  "started_at": "%s",\n  "ended_at": null\n}\n' "$run" "$mode" "$envname" "$(now)" > "$out/run.json"
    annotate "run $run started"; echo "run $run started -> $out/run.json"
    echo "send header: X-Test-Run-ID: $run" ;;
  stop)
    [ -f "$out/run.json" ] || { echo "no such run: $run" >&2; exit 1; }
    python3 - "$out/run.json" "$(now)" <<'PY'
import json,sys
p,t=sys.argv[1:3]; d=json.load(open(p)); d["ended_at"]=t; json.dump(d,open(p,"w"),indent=2)
PY
    annotate "run $run ended"; echo "run $run ended" ;;
  *) echo "usage: obs-run.sh start|stop <test_run_id>" >&2; exit 2 ;;
esac
