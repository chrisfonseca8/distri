#!/usr/bin/env python3
"""Generate per-host mode-600 env files without printing credentials."""
from __future__ import annotations

import secrets
import stat
import subprocess
from pathlib import Path
from urllib.parse import urlparse

ROOT = Path(__file__).resolve().parents[2]
CLOUD = ROOT / "deploy" / "cloud"


def read_env(path: Path) -> dict[str, str]:
    values: dict[str, str] = {}
    for raw in path.read_text(encoding="utf-8").splitlines():
        if not raw or raw.lstrip().startswith("#") or "=" not in raw:
            continue
        key, value = raw.split("=", 1)
        values[key] = value.strip().strip('"').strip("'")
    return values


def write_env(name: str, values: list[str]) -> None:
    path = CLOUD / name
    path.write_text("\n".join(values) + "\n", encoding="utf-8")
    path.chmod(stat.S_IRUSR | stat.S_IWUSR)


databases = read_env(CLOUD / ".env.databases")
shared_path = CLOUD / ".env.shared"
if shared_path.exists():
    shared = read_env(shared_path)
else:
    shared = {
        "RABBITMQ_DEFAULT_USER": "distri",
        "RABBITMQ_DEFAULT_PASS": secrets.token_hex(24),
        "RABBITMQ_ERLANG_COOKIE": secrets.token_hex(32),
        "SIM_KEY_ID": "rzp_test_" + secrets.token_hex(8),
        "SIM_KEY_SECRET": secrets.token_hex(24),
        "SIM_WEBHOOK_SECRET": secrets.token_hex(24),
    }
    write_env(".env.shared", [f"{key}={value}" for key, value in shared.items()])

tag = subprocess.check_output(["git", "rev-parse", "--short", "HEAD"], cwd=ROOT, text=True).strip()
if subprocess.check_output(["git", "status", "--porcelain"], cwd=ROOT, text=True).strip():
    tag += "-working"
repo = "distri-cloud/distri"
dummy = "postgresql://unused:unused@127.0.0.1:5432/unused?sslmode=disable"
db_keys = {
    "order": "ORDER_DATABASE_URL",
    "inventory": "INVENTORY_DATABASE_URL",
    "payment": "PAYMENT_DATABASE_URL",
    "orchestrator": "ORCHESTRATOR_DATABASE_URL",
}

for service, own_key in db_keys.items():
    lines = [f"DISTRI_REPO={repo}", f"IMAGE_TAG={tag}"]
    for key in db_keys.values():
        lines.append(f"{key}={databases[key] if key == own_key else dummy}")
    lines += [
        f"RABBITMQ_URL=amqp://{shared['RABBITMQ_DEFAULT_USER']}:{shared['RABBITMQ_DEFAULT_PASS']}@10.200.0.1:5672/",
        "INVENTORY_URL=http://10.200.0.4:8081",
        f"RAZORPAY_KEY_ID={shared['SIM_KEY_ID']}",
        f"RAZORPAY_KEY_SECRET={shared['SIM_KEY_SECRET']}",
        "RAZORPAY_BASE_URL=http://10.200.0.6:9090",
        f"RAZORPAY_WEBHOOK_SECRET={shared['SIM_WEBHOOK_SECRET']}",
        "DB_MAX_OPEN_CONNS=10", "DB_MAX_IDLE_CONNS=10",
        "WORKER_CONCURRENCY=4", "WEBHOOK_PROCESS_BATCH=100", "WEBHOOK_PROCESS_INTERVAL=1s",
        "ORDER_OUTBOX_CONCURRENCY=4", "ORDER_OUTBOX_POLL_INTERVAL=100ms",
        "ORCHESTRATOR_OUTBOX_CONCURRENCY=4", "ORCHESTRATOR_OUTBOX_POLL_INTERVAL=100ms",
    ]
    write_env(f".env.{service}", lines)

write_env(".env.broker", [
    f"RABBITMQ_DEFAULT_USER={shared['RABBITMQ_DEFAULT_USER']}",
    f"RABBITMQ_DEFAULT_PASS={shared['RABBITMQ_DEFAULT_PASS']}",
    f"RABBITMQ_ERLANG_COOKIE={shared['RABBITMQ_ERLANG_COOKIE']}",
])
write_env(".env.simulator", [
    f"DISTRI_REPO={repo}", f"IMAGE_TAG={tag}",
    f"SIM_KEY_ID={shared['SIM_KEY_ID']}",
    f"SIM_KEY_SECRET={shared['SIM_KEY_SECRET']}",
    f"SIM_WEBHOOK_SECRET={shared['SIM_WEBHOOK_SECRET']}",
    "SIM_WEBHOOK_URL=http://10.200.0.6:8082/v1/webhooks/razorpay",
])
write_env(".env.loadgen", [
    f"DISTRI_REPO={repo}", f"IMAGE_TAG={tag}",
    "ORDER_URL=http://10.200.0.5:8080", "INVENTORY_URL=http://10.200.0.4:8081",
    "PAYMENT_URL=http://10.200.0.6:8082", "ORCHESTRATOR_URL=http://10.200.0.2:8083",
    f"LOADTEST_DATABASE_URL={databases['LOADTEST_DATABASE_URL']}",
    f"LOAD_TEST_ALLOWED_DB_HOST={urlparse(databases['LOADTEST_DATABASE_URL']).hostname}",
    *[f"{key}={value}" for key, value in databases.items() if key.startswith("AUDIT_")],
    "ORDER_DOCKER_SOCK=/home/ubuntu/sockets/order.sock",
    "INVENTORY_DOCKER_SOCK=/home/ubuntu/sockets/inventory.sock",
    "PAYMENT_DOCKER_SOCK=/home/ubuntu/sockets/payment.sock",
    "ORCHESTRATOR_DOCKER_SOCK=/home/ubuntu/sockets/orchestrator.sock",
    "LOCAL_UID=1000", "LOCAL_GID=1000", "SERVER_DOCKER_GID=0",
])

print(f"generated per-host env files for image tag {tag}; no credentials printed")
