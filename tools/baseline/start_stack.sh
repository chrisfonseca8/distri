#!/usr/bin/env bash
# Starts the simulator and payment-service on the pinned B0 baseline settings, then prints
# the effective configuration (secrets filtered) and fails if it is not the intended one.
# Every gateway call goes to the in-memory simulator; .env is only read by compose, never written.
set -euo pipefail
cd "$(dirname "$0")/../.."

# Forget any short fault-test timings left in this shell, then pin every tunable explicitly.
unset PAYMENT_WINDOW PAYMENT_RECONCILE_INTERVAL PAYMENT_RECONCILE_MIN_POLL PAYMENT_AUDIT_INTERVAL PAYMENT_AUDIT_WINDOW \
      COMPENSATION_INTERVAL COMPENSATION_LEASE COMPENSATION_PENDING_POLL COMPENSATION_ABSENCE_WINDOW \
      COMPENSATION_IDEMPOTENT_RETRY_WINDOW COMPENSATION_UNKNOWN_MAX_AGE COMPENSATION_RETRY_DELAY
export PAYMENT_WINDOW=5m PAYMENT_RECONCILE_INTERVAL=15s PAYMENT_RECONCILE_MIN_POLL=10s
export PAYMENT_AUDIT_INTERVAL=1m PAYMENT_AUDIT_WINDOW=1h
export COMPENSATION_INTERVAL=2s COMPENSATION_LEASE=1m COMPENSATION_PENDING_POLL=5s COMPENSATION_ABSENCE_WINDOW=2m
export COMPENSATION_IDEMPOTENT_RETRY_WINDOW=10m COMPENSATION_UNKNOWN_MAX_AGE=30m COMPENSATION_RETRY_DELAY=30s
export WEBHOOK_PROCESS_INTERVAL=1s WEBHOOK_MAX_ATTEMPTS=12 WEBHOOK_BACKOFF_BASE=1s WORKER_CONCURRENCY=4
export SIM_SEED=42 SIM_SCENARIO_MIX="capture=100" SIM_API_LATENCY=normal SIM_WEBHOOK_PROFILE=standard
export SIM_PAY_DELAY=2s SIM_CAPTURE_DELAY=500ms SIM_WEBHOOK_DELAY=300ms SIM_WEBHOOK_WORKERS=4 SIM_TIME_SCALE=1

env_hash_before=$(sha256sum .env | cut -d' ' -f1)
docker compose -f compose.local.yaml -f compose.simulator.yaml --profile simulator up -d --force-recreate gateway-simulator payment-service
for s in gateway-simulator payment-service; do
  until [ "$(docker inspect -f '{{.State.Health.Status}}' distri-local-$s-1)" = healthy ]; do sleep 2; done
done
[ "$(sha256sum .env | cut -d' ' -f1)" = "$env_hash_before" ] || { echo ".env changed"; exit 1; }

show() { docker exec "distri-local-$1-1" env | grep -E "$2" | grep -viE 'SECRET|PASS|KEY' | sort; }
echo "--- payment-service"; show payment-service '^(GATEWAY_|PAYMENT_|COMPENSATION_|WEBHOOK_|WORKER_|DB_MAX|RAZORPAY_BASE_URL)'
echo "--- gateway-simulator"; show gateway-simulator '^SIM_'

expect=(PAYMENT_WINDOW=5m PAYMENT_RECONCILE_INTERVAL=15s PAYMENT_RECONCILE_MIN_POLL=10s COMPENSATION_INTERVAL=2s COMPENSATION_LEASE=1m
        GATEWAY_PROVIDER=razorpay RAZORPAY_BASE_URL=http://gateway-simulator:9090 WORKER_CONCURRENCY=4 WEBHOOK_PROCESS_INTERVAL=1s)
have=$(docker exec distri-local-payment-service-1 env)
for e in "${expect[@]}"; do grep -qx "$e" <<<"$have" || { echo "MISMATCH: expected $e"; exit 1; }; done
sim=$(docker exec distri-local-gateway-simulator-1 env)
for e in SIM_SEED=42 SIM_SCENARIO_MIX=capture=100 SIM_API_LATENCY=normal SIM_WEBHOOK_PROFILE=standard; do
  grep -qx "$e" <<<"$sim" || { echo "MISMATCH: expected $e"; exit 1; }
done
grep -q '^SIM_WEBHOOK_URL=http://payment-service:8082/v1/webhooks/razorpay$' <<<"$sim" || { echo "MISMATCH: webhooks not enabled"; exit 1; }
echo "effective configuration OK; .env unchanged"
