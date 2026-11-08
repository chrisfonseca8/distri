#!/usr/bin/env bash
# Test 3 (fault tests on the AWS staging stack), one observability run per scenario.
#
#   scripts/run-test3.sh                      # all seven scenarios, in order
#   scripts/run-test3.sh payment-kill9-mid-flight db-connection-drop
#
# For each scenario it: starts a labelled observability run, runs the fault test with the run id in
# X-Test-Run-ID, stops the run, and exports metrics, logs and the audit to test-results/<run id>/
# next to the fault test's own chaos-test.json / chaos-test.md.
#
# Needs (see deploy/cloud/README.md): the local observability stack up, the SSH tunnels
# running (deploy/observability/scripts/obs-tunnels.sh start, in its own terminal), tc installed on
# the payment host, and an empty dead queue.
set -euo pipefail
cd "$(dirname "$0")/.."
# The published Prometheus port changes between stack restarts, so ask Docker.
detected="$(docker port distri-obs-prometheus-1 9090/tcp 2>/dev/null | head -n1 | sed 's/.*://')"
export CHAOS_TARGET=cloud PROMETHEUS_PORT="${PROMETHEUS_PORT:-${detected:-9090}}"
obs=deploy/observability/scripts
scenarios=("$@")
[ ${#scenarios[@]} -gt 0 ] || scenarios=(payment-kill9-mid-flight orchestrator-kill9-mid-burst order-service-kill9-mid-message
  inventory-kill9-mid-message broker-restart-mid-burst db-connection-drop network-delay)

"$obs/obs-tunnels.sh" status | grep -q "port 19091: scrapeable" ||
  { echo "metrics tunnels are down: run  $obs/obs-tunnels.sh start  in another terminal" >&2; exit 1; }
curl -fs -m 5 "http://127.0.0.1:$PROMETHEUS_PORT/-/ready" >/dev/null ||
  { echo "Prometheus not ready on port $PROMETHEUS_PORT (set PROMETHEUS_PORT)" >&2; exit 1; }
python3 tools/chaos/chaos.py preflight

failed=0
for scenario in "${scenarios[@]}"; do
  run="cloud-test3-${scenario}-$(date -u +%Y%m%dT%H%M%SZ)"
  echo; echo "######## $scenario  ($run)"
  "$obs/obs-run.sh" start "$run" tunnel staging
  set +e; CHAOS_RUN_ID="$run" python3 tools/chaos/chaos.py "$scenario"; rc=$?; set -e
  [ $rc -eq 0 ] || failed=$((failed + 1))
  sleep 45                       # let Prometheus and Loki catch the recovery
  "$obs/obs-run.sh" stop "$run"
  python3 "$obs/obs-export-run.py" "$run" || echo "(export failed: rerun  python3 $obs/obs-export-run.py $run)"
  echo "$scenario: exit $rc -> test-results/$run"
done
echo; echo "$failed of ${#scenarios[@]} scenario(s) failed"
exit $((failed > 0))
