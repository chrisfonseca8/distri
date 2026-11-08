#!/usr/bin/env bash
# Copy the canonical observability package into every service. Each service is
# its own Go module, so it cannot import a sibling directory; edit
# observability/lib and re-run this script (CI/`make` can diff to catch drift).
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
for svc in order inventory payment orchestrator; do
  dest="$root/$svc-service/pkg/observability"
  mkdir -p "$dest"
  rm -f "$dest"/*.go
  cp "$root"/observability/lib/*.go "$dest"/
done
echo "synced observability/lib -> {order,inventory,payment,orchestrator}-service/pkg/observability"

    