#!/usr/bin/env bash
# Start or stop a second orchestrator replica (test 8). Run on the load generator.
#
#   tools/cloud/cloud-replica.sh start | stop | status
#
# The replica is a second container of the same compose service on the orchestrator host: same image,
# same environment, same database and broker, no published port. It shares the RabbitMQ queues
# (competing consumers) and the outbox/sweeper work (FOR UPDATE SKIP LOCKED) with the first one.
# Load is still sent to the first replica's port 8083.
set -euo pipefail
key="$HOME/.ssh/distri-staging-sydney"; host=10.40.1.10; name=distri-orch2
on_host() { ssh -i "$key" -o BatchMode=yes -o ConnectTimeout=15 -o LogLevel=ERROR "ec2-user@$host" "$1"; }
case "${1:?usage: cloud-replica.sh start|stop|status}" in
  start)  on_host "docker rm -f $name >/dev/null 2>&1; cd ~/distri && docker compose --env-file deploy/cloud/.env.orchestrator -f compose.cloud.yaml run -d --no-deps --name $name orchestrator" ;;
  stop)   on_host "docker rm -f $name" ;;
  status) on_host "docker ps -a --filter label=com.docker.compose.service=orchestrator --format '{{.Names}}  {{.Status}}'" ;;
  *) echo "usage: cloud-replica.sh start|stop|status" >&2; exit 2 ;;
esac
