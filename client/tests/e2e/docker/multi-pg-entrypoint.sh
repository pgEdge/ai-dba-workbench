#!/usr/bin/env bash
#
# multi-pg-entrypoint.sh
#
# Starts four PostgreSQL instances inside a single container:
#
#   5432  ai_workbench   (main cluster — managed by official Docker entrypoint)
#   5440  ragdb primary  (replication primary)
#   5441  ragdb standby1 (hot standby)
#   5442  ragdb standby2 (hot standby)
#
# The main cluster is started by the official postgres docker-entrypoint.sh,
# which handles initdb, pg_hba.conf bootstrapping, and init scripts.
# The three replication instances are managed directly with pg_ctl.
#

set -euo pipefail

LOG_DIR="/var/log/postgresql"
PRIMARY_DATA="/data/repl-primary"
STANDBY1_DATA="/data/repl-standby1"
STANDBY2_DATA="/data/repl-standby2"
REPLICATOR_PASSWORD="${REPLICATOR_PASSWORD:-replicator}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-postgres}"

mkdir -p "${LOG_DIR}" "${PRIMARY_DATA}" "${STANDBY1_DATA}" "${STANDBY2_DATA}"
chown postgres:postgres "${LOG_DIR}" "${PRIMARY_DATA}" "${STANDBY1_DATA}" "${STANDBY2_DATA}"

# -----------------------------------------------------------------------
# Start main postgres (port 5432, ai_workbench) via official entrypoint.
# docker-entrypoint.sh ends with "exec postgres ...", which replaces the
# child process. Running it with bash in the background keeps the PID
# stable across the exec chain for use with wait().
# -----------------------------------------------------------------------
echo "[multi-pg] Starting main PostgreSQL cluster on port 5432..."
bash /usr/local/bin/docker-entrypoint.sh postgres \
    -c "listen_addresses=*" \
    > "${LOG_DIR}/main.log" 2>&1 &
MAIN_PID=$!

echo "[multi-pg] Waiting for main PostgreSQL (port 5432)..."
until pg_isready -h 127.0.0.1 -p 5432 -U postgres >/dev/null 2>&1; do
    sleep 1
done
echo "[multi-pg] Main PostgreSQL is ready."

# -----------------------------------------------------------------------
# Initialize replication primary (port 5440, ragdb).
# -----------------------------------------------------------------------
if [ ! -f "${PRIMARY_DATA}/PG_VERSION" ]; then
    echo "[multi-pg] Initializing replication primary (port 5440)..."

    gosu postgres initdb -D "${PRIMARY_DATA}" -U postgres \
        --auth-local=trust --auth-host=md5

    cat >> "${PRIMARY_DATA}/postgresql.conf" <<'EOF'
listen_addresses = '*'
wal_level = replica
max_wal_senders = 10
max_replication_slots = 10
hot_standby = on
EOF

    {
        echo "host  replication  replicator  0.0.0.0/0  md5"
        echo "host  all          all         0.0.0.0/0  md5"
    } >> "${PRIMARY_DATA}/pg_hba.conf"

    gosu postgres pg_ctl -D "${PRIMARY_DATA}" -o "-p 5440" \
        -l "${LOG_DIR}/repl-primary.log" -w start

    PGPASSWORD="${POSTGRES_PASSWORD}" psql -h 127.0.0.1 -p 5440 -U postgres <<-EOSQL
        ALTER USER postgres WITH PASSWORD '${POSTGRES_PASSWORD}';
        CREATE ROLE replicator WITH REPLICATION LOGIN ENCRYPTED PASSWORD '${REPLICATOR_PASSWORD}';
        CREATE DATABASE ragdb;
EOSQL

    PGPASSWORD="${POSTGRES_PASSWORD}" psql -h 127.0.0.1 -p 5440 -U postgres -d ragdb <<-EOSQL
        CREATE EXTENSION IF NOT EXISTS vector;

        CREATE TABLE documents (
            id        SERIAL PRIMARY KEY,
            content   TEXT NOT NULL,
            embedding vector(768),
            metadata  JSONB
        );

        COMMENT ON TABLE documents IS 'RAG document store for E2E replication tests';

        INSERT INTO documents (content, metadata) VALUES
            ('PostgreSQL is a powerful open-source relational database', '{"source": "docs"}'),
            ('pgvector enables vector similarity search in PostgreSQL', '{"source": "docs"}'),
            ('RAG combines retrieval with generation for AI applications', '{"source": "docs"}');
EOSQL

    echo "[multi-pg] Replication primary ready."
else
    echo "[multi-pg] Replication primary data exists — starting..."
    gosu postgres pg_ctl -D "${PRIMARY_DATA}" -o "-p 5440" \
        -l "${LOG_DIR}/repl-primary.log" -w start
fi

# -----------------------------------------------------------------------
# Initialize standby1 (port 5441).
# -----------------------------------------------------------------------
if [ ! -f "${STANDBY1_DATA}/PG_VERSION" ]; then
    echo "[multi-pg] Initializing standby1 (port 5441) via pg_basebackup..."
    PGPASSWORD="${REPLICATOR_PASSWORD}" gosu postgres pg_basebackup \
        -h 127.0.0.1 -p 5440 -U replicator \
        -D "${STANDBY1_DATA}" -Fp -Xs -P -R
    echo "port = 5441" >> "${STANDBY1_DATA}/postgresql.conf"
    gosu postgres pg_ctl -D "${STANDBY1_DATA}" -o "-p 5441" \
        -l "${LOG_DIR}/repl-standby1.log" -w start
    echo "[multi-pg] Standby1 ready."
else
    echo "[multi-pg] Standby1 data exists — starting..."
    gosu postgres pg_ctl -D "${STANDBY1_DATA}" -o "-p 5441" \
        -l "${LOG_DIR}/repl-standby1.log" -w start
fi

# -----------------------------------------------------------------------
# Initialize standby2 (port 5442).
# -----------------------------------------------------------------------
if [ ! -f "${STANDBY2_DATA}/PG_VERSION" ]; then
    echo "[multi-pg] Initializing standby2 (port 5442) via pg_basebackup..."
    PGPASSWORD="${REPLICATOR_PASSWORD}" gosu postgres pg_basebackup \
        -h 127.0.0.1 -p 5440 -U replicator \
        -D "${STANDBY2_DATA}" -Fp -Xs -P -R
    echo "port = 5442" >> "${STANDBY2_DATA}/postgresql.conf"
    gosu postgres pg_ctl -D "${STANDBY2_DATA}" -o "-p 5442" \
        -l "${LOG_DIR}/repl-standby2.log" -w start
    echo "[multi-pg] Standby2 ready."
else
    echo "[multi-pg] Standby2 data exists — starting..."
    gosu postgres pg_ctl -D "${STANDBY2_DATA}" -o "-p 5442" \
        -l "${LOG_DIR}/repl-standby2.log" -w start
fi

echo ""
echo "[multi-pg] ============================================"
echo "[multi-pg]  All PostgreSQL instances running:"
echo "[multi-pg]    5432  ai_workbench  (main)"
echo "[multi-pg]    5440  ragdb primary"
echo "[multi-pg]    5441  ragdb standby1"
echo "[multi-pg]    5442  ragdb standby2"
echo "[multi-pg] ============================================"
echo ""

# Tail replication logs so they appear in docker logs output.
# The main cluster logs are already going to stdout via the main process.
tail -F \
    "${LOG_DIR}/repl-primary.log" \
    "${LOG_DIR}/repl-standby1.log" \
    "${LOG_DIR}/repl-standby2.log" &

# Keep the container alive by waiting for the main postgres process.
wait "${MAIN_PID}"
