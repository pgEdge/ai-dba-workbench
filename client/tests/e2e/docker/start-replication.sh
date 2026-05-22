#!/usr/bin/env bash
#
# start-replication.sh
#
# Starts three PostgreSQL instances inside the postgres container
# for on-demand replication testing. Called via docker exec by
# ReplicationHelper.start() in E2E tests.
#
# Instances started:
#   5440  ragdb primary  (replication primary, with seed data)
#   5441  ragdb standby1 (hot standby via pg_basebackup)
#   5442  ragdb standby2 (hot standby via pg_basebackup)
#
# Idempotent: re-running when clusters already exist just starts them.
#

set -euo pipefail

# ---------------------------------------------------------------------------
# Portable privilege-drop helper.  Tries gosu (Debian/official postgres
# image), su-exec (Alpine), runuser (util-linux), then POSIX su.
# Usage:  _pg <cmd> [args...]
#         PGPASSWORD=secret _pg pg_basebackup ...
# ---------------------------------------------------------------------------
if command -v gosu > /dev/null 2>&1; then
    _pg() { gosu postgres "$@"; }
elif command -v su-exec > /dev/null 2>&1; then
    _pg() { su-exec postgres "$@"; }
elif command -v runuser > /dev/null 2>&1; then
    _pg() { runuser -u postgres -- "$@"; }
else
    _pg() {
        local cmd
        cmd="$(printf '%q ' "$@")"
        if [ -n "${PGPASSWORD:-}" ]; then
            su postgres -s /bin/bash -c "export PGPASSWORD=$(printf '%q' "${PGPASSWORD}"); ${cmd}"
        else
            su postgres -s /bin/bash -c "${cmd}"
        fi
    }
fi

PRIMARY_DATA="/data/repl-primary"
STANDBY1_DATA="/data/repl-standby1"
STANDBY2_DATA="/data/repl-standby2"
LOG_DIR="/var/log/postgresql"
REPLICATOR_PASSWORD="${REPLICATOR_PASSWORD:-replicator}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-postgres}"
mkdir -p "${LOG_DIR}" "${PRIMARY_DATA}" "${STANDBY1_DATA}" "${STANDBY2_DATA}"
chown postgres:postgres "${LOG_DIR}" "${PRIMARY_DATA}" "${STANDBY1_DATA}" "${STANDBY2_DATA}"
chmod 0700 "${STANDBY1_DATA}" "${STANDBY2_DATA}"

# -----------------------------------------------------------------------
# Replication primary (port 5440)
# -----------------------------------------------------------------------
if [ ! -f "${PRIMARY_DATA}/PG_VERSION" ]; then
    echo "[replication] Initializing primary (port 5440)..."

    _pg initdb -D "${PRIMARY_DATA}" -U postgres \
        --auth-local=trust --auth-host=md5

    cat >> "${PRIMARY_DATA}/postgresql.conf" <<'EOF'
listen_addresses = '*'
wal_level = replica
max_wal_senders = 10
max_replication_slots = 10
hot_standby = on
logging_collector = off
EOF

    {
        echo "# Loopback trust for initial setup (within container)"
        echo "host  all          all         127.0.0.1/32  trust"
        echo "host  all          all         ::1/128       trust"
        echo "# Loopback trust for streaming replication (no password in primary_conninfo)"
        echo "host  replication  replicator  127.0.0.1/32  trust"
        echo "host  replication  replicator  ::1/128       trust"
        echo "# External access requires password"
        echo "host  replication  replicator  0.0.0.0/0    md5"
        echo "host  all          all         0.0.0.0/0    md5"
    } >> "${PRIMARY_DATA}/pg_hba.conf"

    _pg pg_ctl -D "${PRIMARY_DATA}" -o "-p 5440" \
        -l "${LOG_DIR}/repl-primary.log" -w start

    psql -p 5440 -U postgres <<-EOSQL
        ALTER USER postgres WITH PASSWORD '${POSTGRES_PASSWORD}';
        CREATE ROLE replicator WITH REPLICATION LOGIN ENCRYPTED PASSWORD '${REPLICATOR_PASSWORD}';
        CREATE DATABASE ragdb;
EOSQL

    psql -p 5440 -U postgres -d ragdb <<-EOSQL
        CREATE TABLE documents (
            id        SERIAL PRIMARY KEY,
            content   TEXT NOT NULL,
            metadata  JSONB
        );

        COMMENT ON TABLE documents IS 'RAG document store for E2E replication tests';

        INSERT INTO documents (content, metadata) VALUES
            ('PostgreSQL is a powerful open-source relational database', '{"source": "docs"}'),
            ('pgvector enables vector similarity search in PostgreSQL', '{"source": "docs"}'),
            ('RAG combines retrieval with generation for AI applications', '{"source": "docs"}');
EOSQL

    echo "[replication] Primary ready."
else
    echo "[replication] Primary data exists — starting..."
    if _pg pg_ctl status -D "${PRIMARY_DATA}" | grep -q "server is running"; then
        echo "[replication] Primary already running."
    else
        _pg pg_ctl -D "${PRIMARY_DATA}" -o "-p 5440" \
            -l "${LOG_DIR}/repl-primary.log" -w start
    fi
fi

# -----------------------------------------------------------------------
# Standby 1 (port 5441)
# -----------------------------------------------------------------------
if [ ! -f "${STANDBY1_DATA}/PG_VERSION" ]; then
    echo "[replication] Initializing standby1 (port 5441) via pg_basebackup..."
    PGPASSWORD="${REPLICATOR_PASSWORD}" _pg pg_basebackup \
        -h 127.0.0.1 -p 5440 -U replicator \
        -D "${STANDBY1_DATA}" -Fp -Xs -P -R
    chmod 0700 "${STANDBY1_DATA}"
    echo "port = 5441" >> "${STANDBY1_DATA}/postgresql.conf"
    echo "host  all  all  0.0.0.0/0  md5" >> "${STANDBY1_DATA}/pg_hba.conf"
    echo "primary_conninfo = 'host=127.0.0.1 port=5440 user=replicator password=${REPLICATOR_PASSWORD}'" \
        >> "${STANDBY1_DATA}/postgresql.auto.conf"
    _pg pg_ctl -D "${STANDBY1_DATA}" -o "-p 5441" \
        -l "${LOG_DIR}/repl-standby1.log" -w start
    echo "[replication] Standby1 ready."
else
    echo "[replication] Standby1 data exists — starting..."
    if _pg pg_ctl status -D "${STANDBY1_DATA}" | grep -q "server is running"; then
        echo "[replication] Standby1 already running."
    else
        echo "primary_conninfo = 'host=127.0.0.1 port=5440 user=replicator password=${REPLICATOR_PASSWORD}'" \
            >> "${STANDBY1_DATA}/postgresql.auto.conf"
        _pg pg_ctl -D "${STANDBY1_DATA}" -o "-p 5441" \
            -l "${LOG_DIR}/repl-standby1.log" start
    fi
fi

# -----------------------------------------------------------------------
# Standby 2 (port 5442)
# -----------------------------------------------------------------------
if [ ! -f "${STANDBY2_DATA}/PG_VERSION" ]; then
    echo "[replication] Initializing standby2 (port 5442) via pg_basebackup..."
    PGPASSWORD="${REPLICATOR_PASSWORD}" _pg pg_basebackup \
        -h 127.0.0.1 -p 5440 -U replicator \
        -D "${STANDBY2_DATA}" -Fp -Xs -P -R
    chmod 0700 "${STANDBY2_DATA}"
    echo "port = 5442" >> "${STANDBY2_DATA}/postgresql.conf"
    echo "host  all  all  0.0.0.0/0  md5" >> "${STANDBY2_DATA}/pg_hba.conf"
    echo "primary_conninfo = 'host=127.0.0.1 port=5440 user=replicator password=${REPLICATOR_PASSWORD}'" \
        >> "${STANDBY2_DATA}/postgresql.auto.conf"
    _pg pg_ctl -D "${STANDBY2_DATA}" -o "-p 5442" \
        -l "${LOG_DIR}/repl-standby2.log" -w start
    echo "[replication] Standby2 ready."
else
    echo "[replication] Standby2 data exists — starting..."
    if _pg pg_ctl status -D "${STANDBY2_DATA}" | grep -q "server is running"; then
        echo "[replication] Standby2 already running."
    else
        echo "primary_conninfo = 'host=127.0.0.1 port=5440 user=replicator password=${REPLICATOR_PASSWORD}'" \
            >> "${STANDBY2_DATA}/postgresql.auto.conf"
        _pg pg_ctl -D "${STANDBY2_DATA}" -o "-p 5442" \
            -l "${LOG_DIR}/repl-standby2.log" start
    fi
fi

echo ""
echo "[replication] Cluster ready:"
echo "[replication]   5440  ragdb primary"
echo "[replication]   5441  ragdb standby1"
echo "[replication]   5442  ragdb standby2"
