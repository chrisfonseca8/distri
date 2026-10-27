#!/usr/bin/env bash
# Builds a test-only audit database that exposes the four service schemas through
# postgres_fdw. Application services never connect to this database.
set -euo pipefail

cd "$(dirname "$0")/../.."

mapfile -t fields < <(python3 <<'PY'
from pathlib import Path
from urllib.parse import urlparse, urlunparse

values = {}
for raw in Path('deploy/cloud/.env.databases').read_text(encoding='utf-8').splitlines():
    if raw and '=' in raw:
        key, value = raw.split('=', 1)
        values[key] = value

def parts(key):
    u = urlparse(values[key])
    print('\t'.join([u.hostname or '', str(u.port or 5432), u.path.lstrip('/'), u.username or '', u.password or '']))

admin = urlparse(values['AUDIT_ORDER_DATABASE_URL'])
audit = urlunparse(admin._replace(path='/distri_audit', query=admin.query))
print(values['AUDIT_ORDER_DATABASE_URL'])
print(audit)
for key in ('ORDER_DATABASE_URL', 'INVENTORY_DATABASE_URL', 'PAYMENT_DATABASE_URL', 'ORCHESTRATOR_DATABASE_URL'):
    parts(key)
PY
)

participant_admin_url="${fields[0]}"
audit_url="${fields[1]}"

docker run --rm -e ADMIN_URL="$participant_admin_url" postgres:17-alpine sh -ceu '
  psql "$ADMIN_URL" -X -v ON_ERROR_STOP=1 <<'"'"'SQL'"'"' >/dev/null
SELECT '"'"'CREATE DATABASE distri_audit'"'"'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname='"'"'distri_audit'"'"') \gexec
SQL
'

configure_server() {
  server="$1"; schema="$2"; row="$3"
  IFS=$'\t' read -r host port database user password <<<"$row"
  docker run --rm \
    -e AUDIT_URL="$audit_url" -e SERVER_NAME="$server" -e SCHEMA_NAME="$schema" \
    -e REMOTE_HOST="$host" -e REMOTE_PORT="$port" -e REMOTE_DATABASE="$database" \
    -e REMOTE_USER="$user" -e REMOTE_PASSWORD="$password" \
    postgres:17-alpine sh -ceu '
      psql "$AUDIT_URL" -X -v ON_ERROR_STOP=1 \
        -v server="$SERVER_NAME" -v schema="$SCHEMA_NAME" -v host="$REMOTE_HOST" \
        -v port="$REMOTE_PORT" -v database="$REMOTE_DATABASE" \
        -v remote_user="$REMOTE_USER" -v remote_password="$REMOTE_PASSWORD" <<'"'"'SQL'"'"' >/dev/null
CREATE EXTENSION IF NOT EXISTS postgres_fdw;
SELECT format('"'"'DROP SCHEMA IF EXISTS %I CASCADE'"'"', :'"'"'schema'"'"') \gexec
SELECT format('"'"'DROP SERVER IF EXISTS %I CASCADE'"'"', :'"'"'server'"'"') \gexec
SELECT format('"'"'CREATE SERVER %I FOREIGN DATA WRAPPER postgres_fdw OPTIONS (host %L, port %L, dbname %L, sslmode %L)'"'"',
              :'"'"'server'"'"', :'"'"'host'"'"', :'"'"'port'"'"', :'"'"'database'"'"', '"'"'require'"'"') \gexec
SELECT format('"'"'CREATE USER MAPPING FOR CURRENT_USER SERVER %I OPTIONS (user %L, password %L)'"'"',
              :'"'"'server'"'"', :'"'"'remote_user'"'"', :'"'"'remote_password'"'"') \gexec
SELECT format('"'"'CREATE SCHEMA %I'"'"', :'"'"'schema'"'"') \gexec
SELECT format('"'"'IMPORT FOREIGN SCHEMA %I FROM SERVER %I INTO %I'"'"', :'"'"'schema'"'"', :'"'"'server'"'"', :'"'"'schema'"'"') \gexec
SQL
    '
}

configure_server orders_server orders "${fields[2]}"
configure_server inventory_server inventory "${fields[3]}"
configure_server payments_server payments "${fields[4]}"
configure_server orchestrator_server orchestrator "${fields[5]}"

export audit_url
python3 <<'PY'
import os
from pathlib import Path
path = Path('deploy/cloud/.env.databases')
lines = [line for line in path.read_text(encoding='utf-8').splitlines() if not line.startswith('LOADTEST_DATABASE_URL=')]
lines.append('LOADTEST_DATABASE_URL=' + os.environ['audit_url'])
path.write_text('\n'.join(lines) + '\n', encoding='utf-8')
path.chmod(0o600)
PY
unset audit_url participant_admin_url
echo "created test-only distri_audit database with four foreign schemas"
