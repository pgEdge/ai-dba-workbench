#!/usr/bin/env bash
#-------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench - E2E collector-schema applier
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------
#
# Builds the collector binary (if necessary), runs it briefly against
# the e2e datastore so its embedded schema migrations create the
# operational tables (cluster_groups, alerts, blackouts, etc.), then
# stops it. The server's HTTP handlers read from these tables, so
# without this step the post-login dashboard endpoints return HTTP 500.
#
# This script is idempotent: re-running it against a database that
# already has the latest schema is a fast no-op.
#
set -euo pipefail

: "${E2E_RUN_DIR:?must be set}"
: "${E2E_DB_HOST:?must be set}"
: "${E2E_DB_PORT:?must be set}"
: "${E2E_DB_NAME:?must be set}"
: "${E2E_DB_USER:?must be set}"
: "${E2E_DB_PASSWORD:?must be set}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

COLLECTOR_BIN="${E2E_COLLECTOR_BIN:-${REPO_ROOT}/bin/ai-dba-collector}"
COLLECTOR_LOG="${E2E_RUN_DIR}/logs/collector-migrate.log"
COLLECTOR_CONFIG="${E2E_RUN_DIR}/ai-dba-collector.yaml"

mkdir -p "${E2E_RUN_DIR}/logs"

# Render the collector YAML using the shared secret/DSN.
"${SCRIPTS_DIR}/render-collector-config.sh"

# Build the collector binary if we don't already have one cached. CI
# pre-builds the collector via a workflow step, so this branch is
# usually a no-op there; the local runner relies on it.
if [ ! -x "${COLLECTOR_BIN}" ]; then
    echo "==> Building collector binary"
    (
        cd "${REPO_ROOT}/collector/src"
        go build -o "${COLLECTOR_BIN}" .
    )
fi

# Run the collector long enough for NewDatastore() to apply the
# migrations, then stop it. We poll for `schema_version` to converge
# rather than parse log output (more robust against logger format
# changes).
echo "==> Running collector to apply datastore schema"
"${COLLECTOR_BIN}" --config="${COLLECTOR_CONFIG}" \
    > "${COLLECTOR_LOG}" 2>&1 &
COLLECTOR_PID=$!

# The migration is synchronous inside NewDatastore(); waiting until a
# schema_version row exists confirms the migration committed.
export PGPASSWORD="${E2E_DB_PASSWORD}"
PSQL_BIN="${PSQL:-psql}"

deadline=$(( $(date +%s) + 60 ))
while :; do
    if ! kill -0 "${COLLECTOR_PID}" 2>/dev/null; then
        echo "ERROR: collector exited before schema applied; tail of log:" >&2
        tail -n 50 "${COLLECTOR_LOG}" >&2 || true
        exit 1
    fi

    # Has the schema_version table been populated yet?
    version="$("${PSQL_BIN}" \
        -h "${E2E_DB_HOST}" -p "${E2E_DB_PORT}" \
        -U "${E2E_DB_USER}" -d "${E2E_DB_NAME}" \
        -tAc "SELECT COALESCE(MAX(version), 0) FROM schema_version" \
        2>/dev/null || echo "")"

    if [ -n "${version}" ] && [ "${version}" != "0" ]; then
        echo "==> Collector schema version: ${version}"
        break
    fi

    if [ "$(date +%s)" -ge "${deadline}" ]; then
        echo "ERROR: timed out waiting for collector schema migration" >&2
        tail -n 50 "${COLLECTOR_LOG}" >&2 || true
        kill -TERM "${COLLECTOR_PID}" 2>/dev/null || true
        exit 1
    fi

    sleep 1
done

# SIGTERM the collector and wait for it to flush; collectors install a
# proper signal handler that closes the pool cleanly.
echo "==> Stopping collector (PID ${COLLECTOR_PID})"
kill -TERM "${COLLECTOR_PID}" 2>/dev/null || true

# Bounded wait: collector shutdown is fast (close two pools), so 10s
# is more than enough.
for _ in $(seq 1 10); do
    if ! kill -0 "${COLLECTOR_PID}" 2>/dev/null; then
        break
    fi
    sleep 1
done

# Force-kill if still alive after the grace period.
if kill -0 "${COLLECTOR_PID}" 2>/dev/null; then
    echo "WARN: collector did not exit on SIGTERM; sending SIGKILL" >&2
    kill -KILL "${COLLECTOR_PID}" 2>/dev/null || true
fi

# Reap the process so its exit status is harvested and the script
# does not leave a zombie. `wait` after a signal returns 143/137
# which we deliberately ignore.
wait "${COLLECTOR_PID}" 2>/dev/null || true

echo "==> Datastore schema applied"
