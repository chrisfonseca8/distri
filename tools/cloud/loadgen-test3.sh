#!/usr/bin/env bash
# Test 3 on the load-generator host (inside the VPC). Run it with nohup.
#
#   tools/cloud/loadgen-test3.sh <obs-run-id> [scenario ...]     (no scenarios = all seven, in order)
#
# Faults reach the four service hosts and the broker over SSH on their private addresses, the
# orchestrator is called directly, and the database is read through the audit database in
# deploy/cloud/.env.loadgen. Results: ~/distri/test-results/<obs-run-id>/chaos-test.{md,json}
set -euo pipefail
cd "$(dirname "$0")/../.."
run="${1:?usage: loadgen-test3.sh <obs-run-id> [scenario ...]}"; shift
scenarios=("$@")
[ ${#scenarios[@]} -gt 0 ] || scenarios=(payment-kill9-mid-flight orchestrator-kill9-mid-burst order-service-kill9-mid-message
  inventory-kill9-mid-message broker-restart-mid-burst db-connection-drop network-delay)

# Private addresses of the staging hosts (deploy/cloud/ip_details.txt); they do not change on stop/start.
hosts=deploy/cloud/hosts.loadgen.env
cat > "$hosts" <<HOSTS
SSH_USER=ec2-user
SSH_KEY=$HOME/.ssh/distri-staging-sydney
ORCHESTRATOR_HOST=10.40.1.10
ORDER_HOST=10.40.1.11
INVENTORY_HOST=10.40.1.12
PAYMENT_HOST=10.40.1.13
BROKER_HOST=10.40.1.20
HOSTS

export CHAOS_TARGET=cloud CHAOS_DIRECT=1 HOSTS_ENV="$PWD/$hosts" CHAOS_RUN_ID="$run" ORCH_URL=http://10.40.1.10:8083
docker pull -q postgres:17-alpine >/dev/null          # psql runs in a container: no client is installed here

echo "== preflight"; python3 tools/fault-injection/chaos.py preflight
echo "== test 3, run id $run, scenarios: ${scenarios[*]}"
python3 tools/fault-injection/chaos.py "${scenarios[@]}" && status=0 || status=$?
python3 tools/fault-injection/chaos.py heal || true            # never leave an iptables/tc fault behind
echo "== done (exit $status) -> test-results/$run"
exit $status
