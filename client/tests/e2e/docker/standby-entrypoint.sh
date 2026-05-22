#!/usr/bin/env bash
#
# standby-entrypoint.sh
#
# Custom entrypoint for PostgreSQL streaming replication standbys.
# Replaces the standard PostgreSQL entrypoint because standbys must
# bootstrap via pg_basebackup rather than initdb.
#
# Required environment variables:
#   PRIMARY_HOST         Hostname of the primary (default: postgres)
#   PRIMARY_PORT         Port of the primary    (default: 5432)
#   REPLICATOR_USER      Replication user       (default: replicator)
#   REPLICATOR_PASSWORD  Replication password   (default: replicator)
#   PGDATA               Data directory path
#
set -euo pipefail

PRIMARY_HOST="${PRIMARY_HOST:-postgres}"
PRIMARY_PORT="${PRIMARY_PORT:-5432}"
REPLICATOR_USER="${REPLICATOR_USER:-replicator}"
REPLICATOR_PASSWORD="${REPLICATOR_PASSWORD:-replicator}"

if [ -z "${PGDATA:-}" ]; then
    echo "[standby] ERROR: PGDATA must be set." >&2
    exit 1
fi

# -------------------------------------------------------------------
# Wait for the primary to accept connections.
# -------------------------------------------------------------------
wait_for_primary() {
    local max_attempts=60
    local attempt=0

    echo "[standby] Waiting for primary at ${PRIMARY_HOST}:${PRIMARY_PORT}..."
    while [ "$attempt" -lt "$max_attempts" ]; do
        if PGPASSWORD="${REPLICATOR_PASSWORD}" pg_isready \
                -h "${PRIMARY_HOST}" \
                -p "${PRIMARY_PORT}" \
                -U "${REPLICATOR_USER}" \
                -q 2>/dev/null; then
            echo "[standby] Primary is accepting connections."
            return 0
        fi
        attempt=$((attempt + 1))
        echo "[standby]   attempt ${attempt}/${max_attempts} — retrying in 2 s..."
        sleep 2
    done

    echo "[standby] ERROR: primary did not become ready within $((max_attempts * 2)) seconds." >&2
    exit 1
}

# -------------------------------------------------------------------
# Clone the primary with pg_basebackup.
# -R creates standby.signal and writes primary_conninfo automatically.
# -------------------------------------------------------------------
run_basebackup() {
    echo "[standby] Running pg_basebackup from ${PRIMARY_HOST}:${PRIMARY_PORT}..."
    PGPASSWORD="${REPLICATOR_PASSWORD}" pg_basebackup \
        -h "${PRIMARY_HOST}" \
        -p "${PRIMARY_PORT}" \
        -U "${REPLICATOR_USER}" \
        -D "${PGDATA}" \
        -Fp -Xs -P -R
    echo "[standby] pg_basebackup complete."
}

# -------------------------------------------------------------------
# Bootstrap: skip if a data directory already exists (restart case).
# -------------------------------------------------------------------
if [ -f "${PGDATA}/PG_VERSION" ]; then
    echo "[standby] Existing data directory found — skipping pg_basebackup."
else
    mkdir -p "${PGDATA}"
    if [ "$(ls -A "${PGDATA}" 2>/dev/null)" ]; then
        echo "[standby] WARNING: non-empty PGDATA without PG_VERSION — cleaning..."
        rm -rf "${PGDATA:?}"/*
    fi
    wait_for_primary
    run_basebackup
fi

# Ensure correct ownership for the postgres user.
if id postgres >/dev/null 2>&1; then
    chown -R postgres:postgres "${PGDATA}"
fi

echo "[standby] Starting PostgreSQL in hot standby mode..."

# Hand off to the postgres process as PID 1 so Docker signals work.
PG_ARGS=(-D "${PGDATA}" -c "listen_addresses=*" -c "hot_standby=on")

if [ "$(id -u)" = "0" ]; then
    if command -v gosu >/dev/null 2>&1; then
        exec gosu postgres postgres "${PG_ARGS[@]}"
    elif command -v su-exec >/dev/null 2>&1; then
        exec su-exec postgres postgres "${PG_ARGS[@]}"
    else
        exec postgres "${PG_ARGS[@]}"
    fi
else
    exec postgres "${PG_ARGS[@]}"
fi
