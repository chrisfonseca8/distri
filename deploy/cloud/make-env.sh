#!/usr/bin/env bash
# Writes deploy/cloud/.env.cloud from the TEST database URL in the repo's .env.
# Run this on your own machine, then copy the file to the EC2 host (see README).
# The file holds database credentials: it is git-ignored and created with mode 600.
# Nothing secret is printed.
#
# Writes two files: .env.cloud (for the SERVER: services) and .env.loadgen (for the
# LOAD_TEST host: runner only; it gets just the runner's database URL).
#
# Usage:  deploy/cloud/make-env.sh <dockerhub-repo> <image-tag> [server-private-ip]
#         e.g. deploy/cloud/make-env.sh ungoodcoder/distri 2026-10-19 172.31.81.137
set -euo pipefail

repo="${1:?usage: make-env.sh <dockerhub-repo> <image-tag>}"
tag="${2:?usage: make-env.sh <dockerhub-repo> <image-tag>}"
cd "$(dirname "$0")/../.."

server_ip="${3:-}"

python3 - "$repo" "$tag" "$server_ip" <<'PY'
import os, stat, sys
from urllib.parse import urlparse, urlunparse, parse_qsl, urlencode

repo, tag, server_ip = sys.argv[1], sys.argv[2], sys.argv[3]
test_url = ""
for line in open(".env"):
    line = line.strip()
    if line.startswith("DATABASE_TEST_URL="):
        test_url = line.split("=", 1)[1].strip().strip('"').strip("'")
if not test_url:
    sys.exit("DATABASE_TEST_URL is not set in .env")

parsed = urlparse(test_url)
if "-pooler" in (parsed.hostname or ""):
    sys.exit("DATABASE_TEST_URL points at a pooled endpoint; the services need the direct one")

def with_search_path(schema):
    query = [(k, v) for k, v in parse_qsl(parsed.query) if k != "search_path"]
    query.append(("search_path", schema))
    return urlunparse(parsed._replace(query=urlencode(query)))

def runner_url():
    query = [(k, v) for k, v in parse_qsl(parsed.query) if k != "search_path"]
    return urlunparse(parsed._replace(query=urlencode(query)))

lines = [
    f"DISTRI_REPO={repo}",
    f"IMAGE_TAG={tag}",
    f"ORDER_DATABASE_URL={with_search_path('orders')}",
    f"INVENTORY_DATABASE_URL={with_search_path('inventory')}",
    f"PAYMENT_DATABASE_URL={with_search_path('payments')}",
    f"LOADTEST_DATABASE_URL={runner_url()}",
    f"LOAD_TEST_ALLOWED_DB_HOST={parsed.hostname}",
    "GATEWAY_MODE=success",
    "GATEWAY_LATENCY=200ms",
    "DB_MAX_OPEN_CONNS=10",
    "DB_MAX_IDLE_CONNS=10",
]
path = "deploy/cloud/.env.cloud"
with open(path, "w") as handle:
    handle.write("\n".join(lines) + "\n")
os.chmod(path, stat.S_IRUSR | stat.S_IWUSR)
print(f"wrote {path} (mode 600) for repo {repo}, tag {tag}")
if server_ip:
    loadgen = [
        f"DISTRI_REPO={repo}",
        f"IMAGE_TAG={tag}",
        f"ORDER_URL=http://{server_ip}:8080",
        f"INVENTORY_URL=http://{server_ip}:8081",
        f"PAYMENT_URL=http://{server_ip}:8082",
        f"LOADTEST_DATABASE_URL={runner_url()}",
        f"LOAD_TEST_ALLOWED_DB_HOST={parsed.hostname}",
        "SERVER_DOCKER_SOCK=/home/ubuntu/server-docker.sock",
        "GATEWAY_MODE=success",
        "GATEWAY_LATENCY=200ms",
    ]
    loadgen_path = "deploy/cloud/.env.loadgen"
    with open(loadgen_path, "w") as handle:
        handle.write("\n".join(loadgen) + "\n")
    os.chmod(loadgen_path, stat.S_IRUSR | stat.S_IWUSR)
    print(f"wrote {loadgen_path} (mode 600), services at {server_ip}")
print(f"database host (allow-listed for the runner): {parsed.hostname}; database: {parsed.path.lstrip('/')}")
PY
