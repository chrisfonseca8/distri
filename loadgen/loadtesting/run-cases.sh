#!/usr/bin/env bash
# Runs load-test cases against either the local Docker Compose stack or the two AWS
# hosts. Some cases need the services restarted with different settings (gateway
# mode, gateway latency, database pool size); this script does that, runs the case,
# and writes the result into the runner's results directory (load-test.json/.md).
#
# Usage (from the repository root):
#   loadgen/loadtesting/run-cases.sh c1|c2|c3|c4|c5|c6|c7|c9|c10|c11|c12|c13 [latency...]|c14 [latency] [size...]|restore|fetch
#
# ---- Local (the stack must be up: docker compose -f compose.local.yaml up --build --wait)
#   loadgen/loadtesting/run-cases.sh c10
#
# ---- Cloud compose file on the same host (services and runner together)
#   DISTRI_COMPOSE_FILE=compose.cloud.yaml DISTRI_ENV_FILE=deploy/cloud/.env.cloud \
#   LOADTEST_SERVICE=loadtest LOADTEST_PROFILE=loadtest loadgen/loadtesting/run-cases.sh c13 0ms
#
# ---- Two AWS hosts, driven from your machine over SSH (services on the SERVER, runner
#      on the LOAD_TEST host; results are copied back with "fetch")
#   DISTRI_REMOTE=1 SERVER_HOST=<ip> SERVER_KEY=./SERVER_MACHINE.pem \
#   LOADGEN_HOST=<ip> LOADGEN_KEY=./LOAD_TEST.pem loadgen/loadtesting/run-cases.sh c2
set -euo pipefail

# Variables forwarded to the remote commands when they are set.
forwarded_vars=(GATEWAY_MODE GATEWAY_LATENCY DB_MAX_OPEN_CONNS DB_MAX_IDLE_CONNS DB_POOL_SIZE DB_IDLE_SIZE
  LOAD_CLIENTS LOAD_RUNS LOAD_ORDERS LOAD_DURATION LOAD_CLIENT_STEPS LOAD_RATE_STEPS LOAD_TEST_ENVIRONMENT
  LOAD_MAX_CONNS RESULTS_SUBDIR)

if [ -n "${DISTRI_REMOTE:-}" ]; then
  : "${SERVER_HOST:?set SERVER_HOST}" "${SERVER_KEY:?set SERVER_KEY}" "${LOADGEN_HOST:?set LOADGEN_HOST}" "${LOADGEN_KEY:?set LOADGEN_KEY}"
  SERVER_PRIVATE_IP="${SERVER_PRIVATE_IP:-172.31.81.137}"
  ssh_opts=(-o BatchMode=yes -o ConnectTimeout=10 -o IdentitiesOnly=yes -o ServerAliveInterval=30)
  on_server() { ssh -i "$SERVER_KEY" "${ssh_opts[@]}" "ubuntu@$SERVER_HOST" "$1"; }
  on_loadgen() { ssh -i "$LOADGEN_KEY" "${ssh_opts[@]}" "ubuntu@$LOADGEN_HOST" "$1"; }

  # "VAR=value VAR2=value2 " for every forwarded variable that is set, shell-quoted.
  env_prefix() {
    local out="" name
    for name in "${forwarded_vars[@]}"; do
      if [ -n "${!name:-}" ]; then out+="$name=$(printf %q "${!name}") "; fi
    done
    printf '%s' "$out"
  }

  # The runner samples the SERVER's containers through the server's Docker socket,
  # forwarded over SSH to a local unix socket on the LOAD_TEST host (dedicated key).
  # Recreate the tunnel if it died.
  ensure_tunnel() {
    on_loadgen 'sock=/home/ubuntu/server-docker.sock
      if ! curl -s -m 3 --unix-socket "$sock" http://docker/_ping 2>/dev/null | grep -q OK; then
        rm -f "$sock"
        ssh -f -N -i ~/.ssh/tunnel_key -o BatchMode=yes -o IdentitiesOnly=yes -o ExitOnForwardFailure=yes \
          -o ServerAliveInterval=15 -o StrictHostKeyChecking=accept-new \
          -L "$sock":/var/run/docker.sock "ubuntu@'"$SERVER_PRIVATE_IP"'"
        sleep 1
      fi
      curl -s -m 3 --unix-socket "$sock" http://docker/_ping | grep -q OK && echo "docker tunnel: ok" || { echo "docker tunnel: FAILED"; exit 1; }'
  }

  restart_services() {
    on_server "cd ~/distri && $(env_prefix)docker compose -f compose.cloud.yaml -f deploy/cloud/compose.publish-internal.yaml --env-file deploy/cloud/.env.cloud up -d --force-recreate --no-deps --wait $*" 2>&1 | grep -vE "^ ?Container .*(Creating|Created|Recreate|Starting|Waiting)" || true
  }

  run_case() {
    ensure_tunnel >/dev/null
    on_loadgen "cd ~/distri && $(env_prefix)LOAD_CASE=$1 docker compose -f deploy/cloud/compose.loadgen.yaml --env-file deploy/cloud/.env.loadgen run --rm loadtest"
  }
else
  compose=(docker compose -f "${DISTRI_COMPOSE_FILE:-compose.local.yaml}")
  if [ -n "${DISTRI_ENV_FILE:-}" ]; then compose+=(--env-file "$DISTRI_ENV_FILE"); fi
  loadtest_service="${LOADTEST_SERVICE:-local-loadtest}"
  loadtest_profile="${LOADTEST_PROFILE:-local-loadtest}"

  # Recreate only the named services (no migrations, no reseed) with the given env.
  restart_services() {
    "${compose[@]}" up -d --force-recreate --no-deps --wait "$@"
  }

  run_case() {
    local case_name="$1" docker_gid
    docker_gid="$(stat -c %g /var/run/docker.sock)"
    LOCAL_UID="$(id -u)" LOCAL_GID="$(id -g)" LOCAL_DOCKER_GID="$docker_gid" DOCKER_GID="$docker_gid" \
      LOAD_CASE="$case_name" "${compose[@]}" --profile "$loadtest_profile" run --rm --no-deps "$loadtest_service"
  }
fi

# Default stack settings: success gateway, 200 ms latency, pool 10 open / 5 idle
# locally (the cloud env file sets idle equal to open).
restore_defaults() {
  unset GATEWAY_MODE GATEWAY_LATENCY DB_MAX_OPEN_CONNS DB_MAX_IDLE_CONNS DB_POOL_SIZE DB_IDLE_SIZE
  restart_services inventory-service payment-service order-service
}

case "${1:-}" in
  c1|c2|c3|c4|c5|c6|c7|c9)
    # Always restart the payment service: the runner only checks its own
    # GATEWAY_MODE variable, not the mode the running service was started with.
    export GATEWAY_MODE=success GATEWAY_LATENCY=200ms
    restart_services payment-service
    run_case "$(echo "$1" | tr '[:lower:]' '[:upper:]')"
    ;;
  c10)
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
  fetch)
    [ -n "${DISTRI_REMOTE:-}" ] || { echo "fetch only applies to the two-host mode (DISTRI_REMOTE=1)" >&2; exit 2; }
    results_subdir="${RESULTS_SUBDIR:-cloud-load-test}"
    mkdir -p "test-results/$results_subdir"
    scp -i "$LOADGEN_KEY" "${ssh_opts[@]}" "ubuntu@$LOADGEN_HOST:~/distri/test-results/$results_subdir/load-test.md" \
      "ubuntu@$LOADGEN_HOST:~/distri/test-results/$results_subdir/load-test.json" "test-results/$results_subdir/"
    echo "report copied to test-results/$results_subdir/"
    ;;
  *)
    echo "usage: $0 c1|c2|c3|c4|c5|c6|c7|c9|c10|c11|c12|c13 [latency...]|c14 [latency] [size...]|restore|fetch" >&2
    exit 2
    ;;
esac
