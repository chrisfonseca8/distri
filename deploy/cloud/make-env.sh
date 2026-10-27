#!/usr/bin/env bash
# Generate the three mode-600 environment files used by the staging hosts.
# Reads DATABASE_TEST_URL from the repository .env. Nothing secret is printed.
# Usage: make-env.sh <repo> <tag> <orchestrator-ip> <order-ip> <inventory-ip> <payment-ip> <broker-ip> <loadgen-ip>
set -euo pipefail

repo="${1:?usage: make-env.sh <repo> <tag> <orchestrator-ip> <order-ip> <inventory-ip> <payment-ip> <broker-ip> <loadgen-ip>}"
tag="${2:?usage: make-env.sh <repo> <tag> <orchestrator-ip> <order-ip> <inventory-ip> <payment-ip> <broker-ip> <loadgen-ip>}"
orchestrator_ip="${3:?set the orchestrator private IP from terraform output}"
order_ip="${4:?set the order private IP from terraform output}"
inventory_ip="${5:?set the inventory private IP from terraform output}"
payment_ip="${6:?set the payment private IP from terraform output}"
broker_ip="${7:?set the broker private IP from terraform output}"
loadgen_ip="${8:?set the load-generator private IP from terraform output}"
cd "$(dirname "$0")/../.."

python3 - "$repo" "$tag" "$orchestrator_ip" "$order_ip" "$inventory_ip" "$payment_ip" "$broker_ip" "$loadgen_ip" <<'PY'
import ipaddress
import os
import secrets
import stat
import sys
from urllib.parse import parse_qsl, urlencode, urlparse, urlunparse

repo, tag, orchestrator_ip, order_ip, inventory_ip, payment_ip, broker_ip, loadgen_ip = sys.argv[1:]
for value in (orchestrator_ip, order_ip, inventory_ip, payment_ip, broker_ip, loadgen_ip):
    ipaddress.ip_address(value)

test_url = ""
with open(".env", encoding="utf-8") as source:
    for raw in source:
        line = raw.strip()
        if line.startswith("DATABASE_TEST_URL="):
            test_url = line.split("=", 1)[1].strip().strip('"').strip("'")
if not test_url:
    sys.exit("DATABASE_TEST_URL is not set in .env")

parsed = urlparse(test_url)
if parsed.scheme not in ("postgres", "postgresql") or not parsed.hostname:
    sys.exit("DATABASE_TEST_URL is not a valid PostgreSQL URL")
if "-pooler" in parsed.hostname:
    sys.exit("DATABASE_TEST_URL points at a pooled endpoint; use the direct Neon URL")

def with_schema(schema):
    query = [(key, value) for key, value in parse_qsl(parsed.query) if key != "search_path"]
    query.append(("search_path", schema))
    return urlunparse(parsed._replace(query=urlencode(query)))

def without_schema():
    query = [(key, value) for key, value in parse_qsl(parsed.query) if key != "search_path"]
    return urlunparse(parsed._replace(query=urlencode(query)))

def write_env(path, lines):
    with open(path, "w", encoding="utf-8") as target:
        target.write("\n".join(lines) + "\n")
    os.chmod(path, stat.S_IRUSR | stat.S_IWUSR)

rabbit_user = "distri"
rabbit_password = secrets.token_hex(24)
rabbit_cookie = secrets.token_hex(32)
sim_key_id = "rzp_test_" + secrets.token_hex(8)
sim_key_secret = secrets.token_hex(24)
webhook_secret = secrets.token_hex(24)
common = [f"DISTRI_REPO={repo}", f"IMAGE_TAG={tag}"]

app = common + [
    f"ORDER_DATABASE_URL={with_schema('orders')}",
    f"INVENTORY_DATABASE_URL={with_schema('inventory')}",
    f"PAYMENT_DATABASE_URL={with_schema('payments')}",
    f"ORCHESTRATOR_DATABASE_URL={with_schema('orchestrator')}",
    f"RABBITMQ_URL=amqp://{rabbit_user}:{rabbit_password}@{broker_ip}:5672/",
    f"INVENTORY_URL=http://{inventory_ip}:8081",
    f"RAZORPAY_KEY_ID={sim_key_id}",
    f"RAZORPAY_KEY_SECRET={sim_key_secret}",
    f"RAZORPAY_BASE_URL=http://{loadgen_ip}:9090",
    f"RAZORPAY_WEBHOOK_SECRET={webhook_secret}",
    "GATEWAY_MODE=success", "GATEWAY_LATENCY=200ms",
    "DB_MAX_OPEN_CONNS=10", "DB_MAX_IDLE_CONNS=10",
    "WORKER_CONCURRENCY=4", "WEBHOOK_PROCESS_BATCH=100", "WEBHOOK_PROCESS_INTERVAL=1s",
    "ORDER_OUTBOX_CONCURRENCY=4", "ORDER_OUTBOX_POLL_INTERVAL=100ms",
    "ORCHESTRATOR_OUTBOX_CONCURRENCY=4", "ORCHESTRATOR_OUTBOX_POLL_INTERVAL=100ms",
]
broker = [
    f"RABBITMQ_DEFAULT_USER={rabbit_user}",
    f"RABBITMQ_DEFAULT_PASS={rabbit_password}",
    f"RABBITMQ_ERLANG_COOKIE={rabbit_cookie}",
]
loadgen = common + [
    f"ORDER_URL=http://{order_ip}:8080", f"INVENTORY_URL=http://{inventory_ip}:8081",
    f"PAYMENT_URL=http://{payment_ip}:8082", f"ORCHESTRATOR_URL=http://{orchestrator_ip}:8083",
    f"LOADTEST_DATABASE_URL={without_schema()}",
    f"LOAD_TEST_ALLOWED_DB_HOST={parsed.hostname}",
    f"SIM_KEY_ID={sim_key_id}", f"SIM_KEY_SECRET={sim_key_secret}",
    f"SIM_WEBHOOK_URL=http://{payment_ip}:8082/v1/webhooks/razorpay",
    f"SIM_WEBHOOK_SECRET={webhook_secret}",
    "ORDER_DOCKER_SOCK=/home/ec2-user/sockets/order.sock",
    "INVENTORY_DOCKER_SOCK=/home/ec2-user/sockets/inventory.sock",
    "PAYMENT_DOCKER_SOCK=/home/ec2-user/sockets/payment.sock",
    "ORCHESTRATOR_DOCKER_SOCK=/home/ec2-user/sockets/orchestrator.sock",
    "SERVER_DOCKER_GID=0",
    "LOCAL_UID=1000", "LOCAL_GID=1000", "GATEWAY_MODE=success", "GATEWAY_LATENCY=200ms",
]

write_env("deploy/cloud/.env.cloud", app)
write_env("deploy/cloud/.env.broker", broker)
write_env("deploy/cloud/.env.loadgen", loadgen)
print("wrote deploy/cloud/.env.cloud, .env.broker and .env.loadgen (mode 600)")
print(f"services: orchestrator={orchestrator_ip}, order={order_ip}, inventory={inventory_ip}, payment={payment_ip}")
print(f"broker={broker_ip}, loadgen={loadgen_ip}")
print(f"database allow-list: {parsed.hostname}; database: {parsed.path.lstrip('/')}")
PY
