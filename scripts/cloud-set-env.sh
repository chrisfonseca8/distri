#!/usr/bin/env bash
# Change one tuning setting on a service host and restart only that service (test 7). Run on the load generator.
#
#   scripts/cloud-set-env.sh show  <service>
#   scripts/cloud-set-env.sh set   <service> KEY=VALUE [KEY=VALUE ...]
#   scripts/cloud-set-env.sh reset <service>          # back to the settings from before the first `set`
#
# service: orchestrator | order-service | inventory-service | payment-service
# The settings live in ~/distri/deploy/cloud/.env.<orchestrator|order|inventory|payment> on the service host and are read by
# compose.cloud.yaml. Only variables that compose.cloud.yaml reads as ${NAME:-default} take effect.
# `show` prints tuning settings only, never URLs or passwords.
set -euo pipefail
action="${1:?usage: cloud-set-env.sh show|set|reset <service> [KEY=VALUE...]}"; service="${2:?service required}"; shift 2 || true
case "$service" in
  orchestrator) host=10.40.1.10; envname=orchestrator ;; order-service) host=10.40.1.11; envname=order ;;
  inventory-service) host=10.40.1.12; envname=inventory ;; payment-service) host=10.40.1.13; envname=payment ;;
  *) echo "unknown service: $service" >&2; exit 2 ;;
esac
key="$HOME/.ssh/distri-staging-sydney"
on_host() { ssh -i "$key" -o BatchMode=yes -o ConnectTimeout=15 -o LogLevel=ERROR "ec2-user@$host" "$1"; }
file=deploy/cloud/.env.$envname; orig=deploy/cloud/.env.$envname.test7-orig
restart="cd ~/distri && docker compose --env-file $file -f compose.cloud.yaml up -d --no-deps --force-recreate --wait $service"
tuning='^(WORKER_CONCURRENCY|[A-Z_]*CONSUMER[A-Z_]*|[A-Z_]*OUTBOX[A-Z_]*|DB_MAX_[A-Z_]*|PAYMENT_(RESOLVE|RECONCILE)[A-Z_]*|WEBHOOK_[A-Z_]*|SAGA_[A-Z_]*|IMAGE_TAG)='

case "$action" in
  show)  on_host "cd ~/distri && grep -E '$tuning' $file || echo '(no tuning overrides set: compose defaults apply)'" ;;
  set)
    [ $# -gt 0 ] || { echo "give at least one KEY=VALUE" >&2; exit 2; }
    cmd="cd ~/distri && { [ -f $orig ] || cp $file $orig; }"
    for pair in "$@"; do
      [[ "$pair" =~ ^[A-Z][A-Z0-9_]*=[A-Za-z0-9._:/-]+$ ]] || { echo "refusing '$pair' (KEY=VALUE, letters/digits/._:/- only)" >&2; exit 2; }
      k="${pair%%=*}"
      cmd+=" && sed -i '/^$k=/d' $file && echo '$pair' >> $file"
    done
    on_host "$cmd" && on_host "$restart" && echo "set on $service: $*" && on_host "cd ~/distri && grep -E '$tuning' $file" ;;
  reset)
    on_host "cd ~/distri && [ -f $orig ] && cp $orig $file && echo restored || echo 'nothing to restore'"
    on_host "$restart" && echo "$service restarted with the original settings" ;;
  *) echo "unknown action: $action" >&2; exit 2 ;;
esac
