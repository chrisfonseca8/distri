#!/usr/bin/env bash
# SSH tunnels between this machine and the EC2 hosts. No monitoring port is ever
# opened to the internet.
#
#   obs-tunnels.sh start | stop | status
#
# Forward (-L), laptop -> EC2, scraped by the local Prometheus:
#   19091-19094  service /metrics   orchestrator, payment, inventory, order
#   19101-19105  node-exporter      orchestrator, payment, inventory, order, broker
#   19111-19114  cAdvisor           orchestrator, payment, inventory, order
#   19115        RabbitMQ prometheus plugin on the broker
# Reverse (-R), EC2 -> laptop, used by alloy on each host:
#   13100 -> local Loki   14318 -> local Tempo (OTLP/HTTP)
#
# Each tunnel runs in a retry loop, so a network blip reconnects by itself.
set -euo pipefail
here="$(cd "$(dirname "$0")/.." && pwd)"
[ -f "$here/hosts.env" ] || { echo "create $here/hosts.env from hosts.env.example" >&2; exit 1; }
# shellcheck disable=SC1091
source "$here/hosts.env"
state="${XDG_RUNTIME_DIR:-/tmp}/distri-obs-tunnels"; mkdir -p "$state"
SSH_OPTS=(-N -T -i "${SSH_KEY/#\~/$HOME}" -o StrictHostKeyChecking=accept-new -o ServerAliveInterval=15
          -o ServerAliveCountMax=3 -o ExitOnForwardFailure=yes -o ConnectTimeout=10)

run_tunnel() { # name host forwards...
  local name="$1" host="$2"; shift 2
  [ -n "$host" ] || return 0
  ( while true; do ssh "${SSH_OPTS[@]}" "$@" "$SSH_USER@$host" || true; sleep 3; done ) &
  echo $! > "$state/$name.pid"
  echo "tunnel $name -> $host (pid $(cat "$state/$name.pid"))"
}
reverse=(-R 13100:127.0.0.1:3100 -R 14318:127.0.0.1:14318)

case "${1:-}" in
  start)
    run_tunnel orchestrator "$ORCHESTRATOR_HOST" -L 19091:127.0.0.1:9091 -L 19101:127.0.0.1:9100 -L 19111:127.0.0.1:8088 "${reverse[@]}"
    run_tunnel payment      "$PAYMENT_HOST"      -L 19092:127.0.0.1:9092 -L 19102:127.0.0.1:9100 -L 19112:127.0.0.1:8088 "${reverse[@]}"
    run_tunnel inventory    "$INVENTORY_HOST"    -L 19093:127.0.0.1:9093 -L 19103:127.0.0.1:9100 -L 19113:127.0.0.1:8088 "${reverse[@]}"
    run_tunnel order        "$ORDER_HOST"        -L 19094:127.0.0.1:9094 -L 19104:127.0.0.1:9100 -L 19114:127.0.0.1:8088 "${reverse[@]}"
    run_tunnel broker       "$BROKER_HOST"       -L 19105:127.0.0.1:9100 -L 19115:127.0.0.1:15692 "${reverse[@]}"
    wait ;;
  stop)
    for f in "$state"/*.pid; do [ -e "$f" ] || continue; kill "$(cat "$f")" 2>/dev/null || true; rm -f "$f"; done
    pkill -f "ssh .*-L 1909[1-4]" 2>/dev/null || true; echo stopped ;;
  status)
    for f in "$state"/*.pid; do [ -e "$f" ] || continue; n=$(basename "$f" .pid)
      kill -0 "$(cat "$f")" 2>/dev/null && echo "$n: running" || echo "$n: dead"; done
    for p in 19091 19092 19093 19094 19115; do
      curl -fs -m 2 "http://127.0.0.1:$p/metrics" -o /dev/null && echo "port $p: scrapeable" || echo "port $p: not reachable"; done ;;
  *) echo "usage: $0 start|stop|status" >&2; exit 2 ;;
esac
