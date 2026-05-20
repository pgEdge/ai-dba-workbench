#!/usr/bin/env bash
#
# rpm-entrypoint.sh -- Start pgEdge AI DBA Workbench services inside
# the RPM installer container and keep the container running.
#
# Services started:
#   ai-dba-server     -- REST API and MCP server (port 8443)
#   ai-dba-collector  -- Metrics collection service
#   ai-dba-alerter    -- Alert evaluation service
#   nginx             -- Serves the React client (port 8444)
#
# All database connection parameters are passed via CLI flags so that
# the RPM-installed YAML config files are never modified (avoiding
# YAML corruption from sed on richly-commented config files).
# Only the secret_file path is patched via a safe top-level sed.
#

set -euo pipefail

CONFIG_DIR="/etc/pgedge"
DATA_DIR="/data"
LOG_DIR="/var/log/pgedge"

mkdir -p "${LOG_DIR}"

# -----------------------------------------------------------------------
# Print installed pgEdge AI DBA package versions.
# -----------------------------------------------------------------------
echo "[entrypoint] Installed pgEdge AI DBA packages:"
if command -v rpm >/dev/null 2>&1; then
    rpm -qa 'pgedge-ai*' | sort | sed 's/^/  /'
elif command -v dpkg >/dev/null 2>&1; then
    dpkg -l 'pgedge-ai*' 2>/dev/null | awk 'NR>2 {print "  " $2, $3}'
else
    echo "  [entrypoint] WARNING: No package manager found to query versions."
fi

# -----------------------------------------------------------------------
# SELinux prerequisites (no-op if SELinux tools are not present).
# -----------------------------------------------------------------------
echo "[entrypoint] Applying SELinux prerequisites (if available)..."
semanage port -a -t http_port_t -p tcp 8444 2>/dev/null || true
setsebool -P httpd_can_network_connect 1 2>/dev/null || true

# -----------------------------------------------------------------------
# Network connectivity preflight check
# Validates DNS resolution and TCP/HTTP connectivity to all supporting
# containers before starting workbench services. Fails fast on error.
# -----------------------------------------------------------------------
echo "[entrypoint] Running network connectivity preflight check..."
if ! /usr/local/bin/rpm-preflight.sh; then
    echo "[entrypoint] ERROR: Network preflight failed. Aborting startup."
    exit 1
fi

# -----------------------------------------------------------------------
# Generate secrets and credential files.
# -----------------------------------------------------------------------
echo "[entrypoint] Generating collector secret..."
openssl rand -base64 32 > "${CONFIG_DIR}/ai-dba-collector.secret"
chmod 600 "${CONFIG_DIR}/ai-dba-collector.secret"
chown pgedge:pgedge "${CONFIG_DIR}/ai-dba-collector.secret" 2>/dev/null || true

echo "[entrypoint] Copying collector secret to server secret..."
cp "${CONFIG_DIR}/ai-dba-collector.secret" "${CONFIG_DIR}/ai-dba-server.secret"
chmod 600 "${CONFIG_DIR}/ai-dba-server.secret"
chown pgedge:pgedge "${CONFIG_DIR}/ai-dba-server.secret" 2>/dev/null || true

echo "[entrypoint] Writing collector database password..."
printf 'test' > "${CONFIG_DIR}/dba-collector-password"
chmod 600 "${CONFIG_DIR}/dba-collector-password"
chown pgedge:pgedge "${CONFIG_DIR}/dba-collector-password" 2>/dev/null || true

echo "[entrypoint] Writing alerter database password..."
printf 'test' > "${CONFIG_DIR}/dba-alerter-password"
chmod 600 "${CONFIG_DIR}/dba-alerter-password"
chown pgedge:pgedge "${CONFIG_DIR}/dba-alerter-password" 2>/dev/null || true

# -----------------------------------------------------------------------
# Patch secret_file paths in server and collector configs.
# The secret_file path and allow_internal_networks flag are patched
# (both are safe, unambiguous lines). All DB connection params are
# passed via CLI flags so the rest of the YAML files are never modified.
# -----------------------------------------------------------------------
echo "[entrypoint] Patching secret_file paths..."
sed -i "s|^secret_file:.*|secret_file: ${CONFIG_DIR}/ai-dba-server.secret|"    "${CONFIG_DIR}/ai-dba-server.yaml"
sed -i "s|^secret_file:.*|secret_file: ${CONFIG_DIR}/ai-dba-collector.secret|" "${CONFIG_DIR}/ai-dba-collector.yaml"
sed -i "s/allow_internal_networks: false/allow_internal_networks: true/" "${CONFIG_DIR}/ai-dba-server.yaml"

# -----------------------------------------------------------------------
# Create database users before starting services.
# ON_ERROR_STOP=0 silently skips "already exists" on reruns.
# -----------------------------------------------------------------------
echo "[entrypoint] Creating database users and granting schema privileges..."
PGPASSWORD=postgres psql -h postgres -U postgres -d ai_workbench \
    -v ON_ERROR_STOP=0 <<'EOSQL' || true
CREATE EXTENSION IF NOT EXISTS vector;
CREATE SCHEMA IF NOT EXISTS metrics;
CREATE ROLE dba_collector WITH LOGIN PASSWORD 'test';
CREATE USER dba_server WITH PASSWORD 'test';
CREATE USER dba_alerter WITH PASSWORD 'test';
GRANT ALL ON SCHEMA public TO dba_server;
GRANT ALL ON SCHEMA public TO dba_collector;
GRANT ALL ON SCHEMA metrics TO dba_collector;
ALTER SCHEMA metrics OWNER TO dba_collector;
GRANT CREATE ON DATABASE ai_workbench TO dba_collector;
EOSQL
echo "[entrypoint] Database users ready."

echo "[entrypoint] Starting pgEdge AI DBA Workbench services..."

if [ -d /run/systemd/system ]; then
    echo "[entrypoint] systemd detected — starting services via systemctl..."
    systemctl start pgedge-ai-dba-collector.service
    systemctl start pgedge-ai-dba-server.service

    # Wait for server to finish migrations, then apply database grants.
    echo "[entrypoint] Waiting for server to finish migrations..."
    until curl -sf http://localhost:8443/health >/dev/null 2>&1; do
        sleep 2
    done
    echo "[entrypoint] Applying database grants..."
    PGPASSWORD=postgres psql -h postgres -U postgres -d ai_workbench \
        -v ON_ERROR_STOP=0 \
        -f /usr/local/share/init-db.sql
    echo "[entrypoint] Database grants applied."

    systemctl start pgedge-ai-dba-alerter.service
    systemctl start nginx.service

    echo "[entrypoint] All services started via systemctl. Tailing logs..."
else
    echo "[entrypoint] No systemd — starting services directly..."

    # Start ai-dba-collector first (creates its own schema/tables)
    /usr/bin/ai-dba-collector \
        -config "${CONFIG_DIR}/ai-dba-collector.yaml" \
        -pg-host postgres \
        -pg-database ai_workbench \
        -pg-username dba_collector \
        -pg-password-file "${CONFIG_DIR}/dba-collector-password" \
        -pg-sslmode disable \
        > "${LOG_DIR}/collector.log" 2>&1 &
    echo "[entrypoint] ai-dba-collector started (PID $!)"

    echo "[entrypoint] Waiting for collector to initialize..."
    until grep -q "running\|started\|Collector is running" "${LOG_DIR}/collector.log" 2>/dev/null; do
        sleep 2
    done
    echo "[entrypoint] Collector initialized."

    # Start ai-dba-server
    /usr/bin/ai-dba-server \
        -config "${CONFIG_DIR}/ai-dba-server.yaml" \
        -data-dir "${DATA_DIR}" \
        -db-host postgres \
        -db-name ai_workbench \
        -db-user dba_server \
        -db-password test \
        -db-sslmode disable \
        > "${LOG_DIR}/server.log" 2>&1 &
    echo "[entrypoint] ai-dba-server started (PID $!)"

    echo "[entrypoint] Waiting for server to finish migrations..."
    until curl -sf http://localhost:8443/health >/dev/null 2>&1; do
        sleep 2
    done
    echo "[entrypoint] Server is healthy — migrations complete."

    echo "[entrypoint] Applying database grants..."
    PGPASSWORD=postgres psql -h postgres -U postgres -d ai_workbench \
        -v ON_ERROR_STOP=0 \
        -f /usr/local/share/init-db.sql
    echo "[entrypoint] Database grants applied."

    # Start ai-dba-alerter
    /usr/bin/ai-dba-alerter \
        -config "${CONFIG_DIR}/ai-dba-alerter.yaml" \
        -db-host postgres \
        -db-name ai_workbench \
        -db-user dba_alerter \
        -db-password-file "${CONFIG_DIR}/dba-alerter-password" \
        -db-sslmode disable \
        > "${LOG_DIR}/alerter.log" 2>&1 &
    echo "[entrypoint] ai-dba-alerter started (PID $!)"

    # Start nginx (serves the React client)
    # Remove default nginx site so only the pgEdge client config serves on port 8444
    rm -f /etc/nginx/sites-enabled/default
    nginx -g "daemon off;" > "${LOG_DIR}/nginx.log" 2>&1 &
    echo "[entrypoint] nginx started (PID $!)"

    echo "[entrypoint] All services started. Tailing logs..."
fi

# Keep container running and surface log output to docker logs.
tail -F \
    "${LOG_DIR}/server.log" \
    "${LOG_DIR}/collector.log" \
    "${LOG_DIR}/alerter.log" \
    "${LOG_DIR}/nginx.log"
