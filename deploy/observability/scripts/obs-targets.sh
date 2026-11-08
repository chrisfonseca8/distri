#!/usr/bin/env bash
# Write Prometheus file_sd target files for one test run.
#
#   obs-targets.sh <test_run_id> [local|tunnel] [environment]
#
# local  : services run in the local docker compose project; Prometheus reaches them
#          over the shared docker network by service name.
# tunnel : services run on EC2; Prometheus reaches them through the SSH tunnels from
#          obs-tunnels.sh (local ports 19091-19094, 191xx for node/cadvisor, 19115 rabbitmq).
#
# The test_run_id becomes a label on every series, so Grafana can filter on it.
set -euo pipefail
run="${1:?usage: obs-targets.sh <test_run_id> [local|tunnel] [environment]}"
mode="${2:-local}"
env="${3:-$([ "$mode" = tunnel ] && echo staging || echo local)}"
dir="$(cd "$(dirname "$0")/.." && pwd)/prometheus/targets"
mkdir -p "$dir"

target() { # service host:port
  printf -- '- targets: ["%s"]\n  labels: { service: "%s", environment: "%s", test_run_id: "%s" }\n' "$2" "$1" "$env" "$run"
}

if [ "$mode" = tunnel ]; then
  H=host.docker.internal
  { target orchestrator-service $H:19091; target payment-service $H:19092
    target inventory-service $H:19093;   target order-service $H:19094; } > "$dir/services.yml"
  { target orchestrator-host $H:19101; target payment-host $H:19102
    target inventory-host $H:19103;    target order-host $H:19104; target broker-host $H:19105
    target orchestrator-containers $H:19111; target payment-containers $H:19112
    target inventory-containers $H:19113;    target order-containers $H:19114; } > "$dir/infra.yml"
  target rabbitmq $H:19115 > "$dir/rabbitmq.yml"
else
  { target orchestrator-service orchestrator:9091; target payment-service payment-service:9092
    target inventory-service inventory-service:9093; target order-service order-service:9094; } > "$dir/services.yml"
  # node-exporter runs in the obs stack's `host` profile (the cAdvisor/container view is EC2-only).
  target local-host node-exporter:9100 > "$dir/infra.yml"
  target rabbitmq rabbitmq:15692 > "$dir/rabbitmq.yml"
fi
echo "targets written for run=$run mode=$mode environment=$env"
