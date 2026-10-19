#!/usr/bin/env bash
# Runs load-test cases C10-C14 against the local Docker Compose stack.
#
# Some cases need the services restarted with different settings (gateway mode,
# gateway latency, database pool size). This script does that, runs the case,
# and writes the result into test-results/load-test/ (load-test.json/.md).
#
# Usage (from the repository root):
#   loadgen/loadtesting/run-cases.sh c10|c11|c12|c13 [latency...]|c14 [latency] [size...]|restore
#
# The stack must already be up:  docker compose -f compose.local.yaml up --build --wait
set -euo pipefail

compose=(docker compose -f compose.local.yaml)

# Recreate only the named services (no migrations, no reseed) with the given env.
restart_services() {
  "${compose[@]}" up -d --force-recreate --no-deps --wait "$@"
}

run_case() {
  local case_name="$1"
  LOCAL_UID="$(id -u)" LOCAL_GID="$(id -g)" LOCAL_DOCKER_GID="$(stat -c %g /var/run/docker.sock)" \
    LOAD_CASE="$case_name" "${compose[@]}" --profile local-loadtest run --rm --no-deps local-loadtest
}

# Default stack settings: success gateway, 200 ms latency, pool 10 open / 5 idle.
restore_defaults() {
  unset GATEWAY_MODE GATEWAY_LATENCY DB_MAX_OPEN_CONNS DB_MAX_IDLE_CONNS DB_POOL_SIZE DB_IDLE_SIZE
  restart_services inventory-service payment-service order-service
}

case "${1:-}" in
  c10)
    # Always restart the payment service: the runner only checks its own
    # GATEWAY_MODE variable, not the mode the running service was started with.
    export GATEWAY_MODE=success GATEWAY_LATENCY=200ms
    restart_services payment-service
    run_case C10
    ;;
  c11)
    export GATEWAY_MODE=decline
    restart_services payment-service
    run_case C11
    ;;
  c12)
    export GATEWAY_MODE=random
    restart_services payment-service
    run_case C12
    ;;
  c13)
    # 0 ms removes the simulator sleep. 3 s would hit the payment service's own
    # 3 s gateway timeout, so the sweep stops at 2 s.
    # Optional arguments choose specific latencies, e.g. "c13 0ms".
    export GATEWAY_MODE=success
    latencies=("${@:2}")
    if [ "${#latencies[@]}" -eq 0 ]; then latencies=(0ms 200ms 1000ms 2000ms); fi
    for latency in "${latencies[@]}"; do
      export GATEWAY_LATENCY="$latency"
      restart_services payment-service
      run_case C13
    done
    ;;
  c14)
    # Optional second argument sets the gateway latency (default 200ms). At 200ms the
    # pool is not the limit; run it again with 0ms to probe the pool itself.
    # MaxIdleConns equals MaxOpenConns unless IDLE_SIZE is set, e.g.:
    #   IDLE_SIZE=5 loadgen/loadtesting/run-cases.sh c14 0ms 10
    # so connection churn is not a second variable by default.
    export GATEWAY_MODE=success GATEWAY_LATENCY="${2:-200ms}"
    sizes=("${@:3}")
    if [ "${#sizes[@]}" -eq 0 ]; then sizes=(2 5 10 20); fi
    for size in "${sizes[@]}"; do
      idle="${IDLE_SIZE:-$size}"
      export DB_MAX_OPEN_CONNS="$size" DB_MAX_IDLE_CONNS="$idle" DB_POOL_SIZE="$size" DB_IDLE_SIZE="$idle"
      restart_services inventory-service payment-service order-service
      run_case C14
    done
    ;;
  restore)
    restore_defaults
    ;;
  *)
    echo "usage: $0 c10|c11|c12|c13|c14|restore" >&2
    exit 2
    ;;
esac
