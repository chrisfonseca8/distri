#!/usr/bin/env bash
# Creates the four service-owned databases/roles across the two empty Neon computes.
# Reads the two admin URLs from the repository .env and writes mode-600 service and
# audit URLs to deploy/cloud/.env.databases. Secrets are never printed.
set -euo pipefail

cd "$(dirname "$0")/../.."

read_url() {
  python3 - "$1" <<'PY'
import sys
from pathlib import Path
key = sys.argv[1]
for raw in Path('.env').read_text(encoding='utf-8').splitlines():
    if raw.startswith(key + '='):
        value = raw.split('=', 1)[1].strip().strip('"').strip("'")
        print(value.replace('-pooler.', '.'))
        break
else:
    raise SystemExit(f'{key} is not set in .env')
PY
}

participant_admin_url="$(read_url PARTICIPANT_NEON_ADMIN_URL)"
orchestrator_admin_url="$(read_url ORCHESTRATOR_NEON_ADMIN_URL)"
order_password="$(openssl rand -hex 24)"
inventory_password="$(openssl rand -hex 24)"
payment_password="$(openssl rand -hex 24)"
orchestrator_password="$(openssl rand -hex 24)"

provision() {
  admin_url="$1"
  role="$2"
  password="$3"
  database="$4"
  docker run --rm \
    -e ADMIN_URL="$admin_url" -e APP_ROLE="$role" -e APP_PASSWORD="$password" -e APP_DATABASE="$database" \
    postgres:17-alpine sh -ceu '
      psql "$ADMIN_URL" -X -v ON_ERROR_STOP=1 \
        -v role="$APP_ROLE" -v password="$APP_PASSWORD" -v database="$APP_DATABASE" <<'"'"'SQL'"'"'
SELECT format('"'"'CREATE ROLE %I LOGIN PASSWORD %L'"'"', :'"'"'role'"'"', :'"'"'password'"'"')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = :'"'"'role'"'"') \gexec
SELECT format('"'"'ALTER ROLE %I PASSWORD %L'"'"', :'"'"'role'"'"', :'"'"'password'"'"') \gexec
SELECT format('"'"'GRANT %I TO %I'"'"', :'"'"'role'"'"', current_user) \gexec
SELECT format('"'"'CREATE DATABASE %I OWNER %I'"'"', :'"'"'database'"'"', :'"'"'role'"'"')
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = :'"'"'database'"'"') \gexec
SQL
    ' >/dev/null
}

provision "$participant_admin_url" distri_order "$order_password" orders_db
provision "$participant_admin_url" distri_inventory "$inventory_password" inventory_db
provision "$participant_admin_url" distri_payment "$payment_password" payments_db
provision "$orchestrator_admin_url" distri_orchestrator "$orchestrator_password" orchestrator_db

export participant_admin_url orchestrator_admin_url
export order_password inventory_password payment_password orchestrator_password
python3 <<'PY'
import os
import stat
from pathlib import Path
from urllib.parse import quote, urlparse, urlunparse

def url_for(admin_key, database, role=None, password=None, schema=None):
    parsed = urlparse(os.environ[admin_key])
    if role is not None:
        host = parsed.hostname or ''
        port = f':{parsed.port}' if parsed.port else ''
        netloc = f'{quote(role)}:{quote(password or "")}@{host}{port}'
    else:
        netloc = parsed.netloc
    query = parsed.query
    if schema:
        query = f'{query}&search_path={schema}' if query else f'search_path={schema}'
    return urlunparse(parsed._replace(netloc=netloc, path='/' + database, query=query))

lines = [
    'ORDER_DATABASE_URL=' + url_for('participant_admin_url', 'orders_db', 'distri_order', os.environ['order_password'], 'orders'),
    'INVENTORY_DATABASE_URL=' + url_for('participant_admin_url', 'inventory_db', 'distri_inventory', os.environ['inventory_password'], 'inventory'),
    'PAYMENT_DATABASE_URL=' + url_for('participant_admin_url', 'payments_db', 'distri_payment', os.environ['payment_password'], 'payments'),
    'ORCHESTRATOR_DATABASE_URL=' + url_for('orchestrator_admin_url', 'orchestrator_db', 'distri_orchestrator', os.environ['orchestrator_password'], 'orchestrator'),
    'AUDIT_ORDER_DATABASE_URL=' + url_for('participant_admin_url', 'orders_db'),
    'AUDIT_INVENTORY_DATABASE_URL=' + url_for('participant_admin_url', 'inventory_db'),
    'AUDIT_PAYMENT_DATABASE_URL=' + url_for('participant_admin_url', 'payments_db'),
    'AUDIT_ORCHESTRATOR_DATABASE_URL=' + url_for('orchestrator_admin_url', 'orchestrator_db'),
]
path = Path('deploy/cloud/.env.databases')
path.write_text('\n'.join(lines) + '\n', encoding='utf-8')
path.chmod(stat.S_IRUSR | stat.S_IWUSR)
print(f'wrote {path} (mode 600); no credentials printed')
PY

unset participant_admin_url orchestrator_admin_url
unset order_password inventory_password payment_password orchestrator_password
echo "created orders_db, inventory_db, payments_db and orchestrator_db with dedicated roles"
