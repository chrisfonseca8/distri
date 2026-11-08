#!/usr/bin/env bash
# Runs every Go test, including the database-gated integration tests, against
# the dedicated *_test database from compose.local.yaml.
#
# Test binaries are compiled, then executed from an empty directory so the
# upward ".env" search used by the service config loaders cannot pick up a
# stray file. Verbose output per package goes to $OUT/<module>/<package>.log.
#
# Usage: tools/integration/run-integration.sh [module ...]     (default: all modules)
# Env:   TEST_DB_URL   base URL of the *_test database (no search_path)
#        OUT           log directory (default test-results/local/integration)
#        RACE=0        disable the race detector
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TEST_DB_URL="${TEST_DB_URL:-postgres://distri:distri@127.0.0.1:5432/distri_test?sslmode=disable}"
OUT="${OUT:-$ROOT/test-results/local/integration}"
RACE_FLAG="-race"; [ "${RACE:-1}" = "0" ] && RACE_FLAG=""

declare -A SCHEMA=(
  [order-service]=orders
  [inventory-service]=inventory
  [payment-service]=payments
  [orchestrator-service]=orchestrator
  [gateway-simulator]=public
  [tools]=public
)
MODULES=("$@")
[ ${#MODULES[@]} -eq 0 ] && MODULES=(gateway-simulator inventory-service order-service orchestrator-service payment-service tools)

GATES=(RUN_ADMIN_INTEGRATION RUN_CHECKOUT_INTEGRATION RUN_INVENTORY_INTEGRATION
  RUN_ORDER_CREATE_INTEGRATION RUN_OUTBOX_INTEGRATION RUN_PAYMENT_JOBS_INTEGRATION
  RUN_REPLY_INTEGRATION RUN_RETENTION_INTEGRATION RUN_SWEEPER_INTEGRATION
  ${EXTRA_GATES:-})
for gate in "${GATES[@]}"; do export "$gate=1"; done

CLEAN="$(mktemp -d)"; BIN="$(mktemp -d)"
trap 'rm -rf "$CLEAN" "$BIN"' EXIT
rm -rf "$OUT"; mkdir -p "$OUT"
status=0

for module in "${MODULES[@]}"; do
  mkdir -p "$OUT/$module"
  export DATABASE_TEST_URL="$TEST_DB_URL&search_path=${SCHEMA[$module]}"
  while read -r pkg; do
    name="$(echo "${pkg#*/$module}" | tr '/' '_' | sed 's/^_//')"; name="${name:-root}"
    bin="$BIN/$module-$name.test"
    if ! (cd "$ROOT/$module" && go test -c $RACE_FLAG -o "$bin" "$pkg") >"$OUT/$module/$name.build.log" 2>&1; then
      echo "BUILD-FAIL $module $pkg"; status=1; continue
    fi
    [ -f "$bin" ] || continue
    # -p 1 equivalent: packages run one after another, they share one database.
    (cd "$CLEAN" && "$bin" -test.v -test.count=1 -test.timeout=10m) >"$OUT/$module/$name.log" 2>&1
    rc=$?
    [ $rc -ne 0 ] && status=1
    printf '%-4s %s %s\n' "$([ $rc -eq 0 ] && echo ok || echo FAIL)" "$module" "$pkg"
  done < <(cd "$ROOT/$module" && go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./...)
done

# One-line-per-test summary for the report.
grep -rhoE '^\s*--- (PASS|FAIL|SKIP): [^ ]+' "$OUT" | sed 's/^\s*//' | awk '{print $2}' | sort | uniq -c
exit $status
