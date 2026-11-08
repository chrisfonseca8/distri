#!/usr/bin/env bash
# Load follow-ups on the LOCAL stack through the existing runner (tools/load-test/run-cases.sh):
# hot product (C5), 80/20 skew (C6), multi-item opposite order (C7) and a soak (C2 for 5 minutes).
# The runner TRUNCATES the local tables by design: dump the database first.
# COMPOSE_ENV_FILES=/dev/null keeps docker compose from parsing a stray root .env.
set -uo pipefail
cd "$(dirname "$0")/../../.."
export COMPOSE_ENV_FILES=/dev/null
OUT=test-results/local/load-followups
mkdir -p "$OUT"
size() { docker exec distri-local-postgres-1 psql -U distri -d distri -Atc "select pg_database_size('distri')"; }

keep() { # name
  cp test-results/local/load/load-test.json "$OUT/$1.json" 2>/dev/null && cp test-results/local/load/load-test.md "$OUT/$1.md" 2>/dev/null
}

for name in c5 c6 c7; do
  echo "== $name  db_bytes_before=$(size)"
  tools/load-test/run-cases.sh "$name" && keep "$name" || echo "   $name FAILED"
  echo "   db_bytes_after=$(size)"
done

echo "== soak (C2, 30 clients, 300 s)  db_bytes_before=$(size)"
LOAD_CLIENTS=30 LOAD_RUNS=1 LOAD_DURATION=300s LOAD_WARMUP=10s tools/load-test/run-cases.sh c2 && keep soak || echo "   soak FAILED"
echo "   db_bytes_after=$(size)"
tools/load-test/run-cases.sh restore
