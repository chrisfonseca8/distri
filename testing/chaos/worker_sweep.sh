#!/usr/bin/env bash
# One variable: payment-service WORKER_CONCURRENCY (4, 8, 16) with everything else fixed
# (30 closed-loop clients, 60 s, 200 ms simulated gateway). Tests the hypothesis that
# completed/s is capped by WORKER_CONCURRENCY / gateway latency (4 / 0.2 s = 20 per second).
set -uo pipefail
cd "$(dirname "$0")/../.."
export COMPOSE_ENV_FILES=/dev/null
for w in 4 8 16; do
  since=$(date -u +%Y-%m-%dT%H:%M:%S)
  echo "== WORKER_CONCURRENCY=$w"
  WORKER_CONCURRENCY=$w LOAD_CLIENTS=30 LOAD_RUNS=1 LOAD_DURATION=60s LOAD_WARMUP=10s testing/loadtesting/run-cases.sh c2 | grep -E 'offered='
  python3 testing/chaos/summarize_load.py "$since" "test-results/local/load-followups/worker-concurrency-$w.json"
done
unset WORKER_CONCURRENCY
testing/loadtesting/run-cases.sh restore
