#!/usr/bin/env bash
# Cloud load presets for tests 6, 7 and 9. Run on the load-generator host.
#
#   scripts/loadgen-load.sh <preset> <obs-run-id>
#
# presets
#   test6-clients  C3 client sweep (closed loop): STEPS=1,2,5,10,20,50,100,200
#   test6-rate     C4 offered-rate sweep (open loop, requests/s): RATES=1,2,4,8,16,32
#   test7          C2 repeated three times at CLIENTS (default 50): for one-variable bottleneck experiments
#   test9          C2 once for DURATION (default 45m) at CLIENTS (required): the soak
#   audit          only the phase-22 database audit (test 10 final audit)
#
# Optional: LABEL names the deployment in the report (default cloud-<preset>).
# After the load it waits 30 s and runs the phase-22 database audit. The exit status is the load run's.
# Results: ~/distri/test-results/cloud/<obs-run-id>/ (load-test.md, load-test.json, audit/).
set -euo pipefail
cd "$(dirname "$0")/.."
preset="${1:?usage: loadgen-load.sh <test6-clients|test6-rate|test7|test9> <obs-run-id>}"
run="${2:?usage: loadgen-load.sh <preset> <obs-run-id>}"
compose=(docker compose --env-file deploy/cloud/.env.loadgen -f deploy/cloud/compose.loadgen.yaml)

export TEST_RUN_ID="$run" RESULTS_SUBDIR="cloud/$run" LOAD_TEST_ENVIRONMENT="${LABEL:-cloud-$preset}"
export LOAD_MAX_CONNS=1000 LOAD_WORKFLOW_TIMEOUT=180s    # baseline end-to-end p95 is already 11 s: never judge by the 15-30 s defaults
case "$preset" in
  test6-clients) export LOAD_CASE=C3 LOAD_CLIENT_STEPS="${STEPS:-1,2,5,10,20,50,100,200}" ;;
  test6-rate)    export LOAD_CASE=C4 LOAD_RATE_STEPS="${RATES:-1,2,4,8,16,32}" ;;
  test7)         export LOAD_CASE=C2 LOAD_CLIENTS="${CLIENTS:-50}" LOAD_RUNS=3 ;;
  test9)         export LOAD_CASE=C2 LOAD_CLIENTS="${CLIENTS:?set CLIENTS to about 70% of the measured limit}" LOAD_RUNS=1 \
                        LOAD_DURATION="${DURATION:-45m}" LOAD_WORKFLOW_TIMEOUT=300s ;;
  audit)         ;;
  *) echo "unknown preset: $preset" >&2; exit 2 ;;
esac

rc=0
if [ "$preset" != audit ]; then
  echo "== $preset  run=$run  case=$LOAD_CASE"
  set +e
  "${compose[@]}" run --rm -T loadtest        # -T: no pseudo-terminal, so it also works under nohup
  rc=$?
  set -e
  echo "== load finished (exit $rc); auditing in 30 s"
  sleep 30
fi
CORRECTNESS_RESULTS_SUBDIR="cloud/$run/audit" PHASES=22 RUN_LABEL="$LOAD_TEST_ENVIRONMENT" \
  "${compose[@]}" run --rm -T correctness || { echo "AUDIT FAILED OR NOT RUN"; rc=1; }
echo "== done -> ~/distri/test-results/cloud/$run   (load exit $rc)"
exit $rc
