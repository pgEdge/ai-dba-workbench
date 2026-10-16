#!/usr/bin/env bash
#
# start-logical-replication.sh
#
# Sets up NATIVE PostgreSQL logical replication between the two E2E logical
# nodes (separate containers):
#
#   pg-logical-node1  (publisher)   -> pg-logical-node2  (subscriber)
#
# Both containers start with wal_level=logical and pg_stat_statements
# preloaded (see docker-compose.yml). This script creates the monitoring
# extensions, the seed `documents` table on both nodes, a publication on
# node1, and a subscription on node2, then waits for the initial data sync.
#
# Idempotent: re-running is a no-op once the publication/subscription exist.
#
# Intended to run INSIDE the node2 container (via `docker exec`), which can
# resolve `pg-logical-node1` over the e2e docker network.
#

set -euo pipefail

PG_PASSWORD="${POSTGRES_PASSWORD:-postgres}"
export PGPASSWORD="${PG_PASSWORD}"

NODE1_HOST="${LOGICAL_NODE1_HOST:-pg-logical-node1}"
NODE2_HOST="127.0.0.1"
DB="ragdb"
PUB="ai_logical_pub"
SUB="ai_logical_sub"

psql_n1() { psql -h "${NODE1_HOST}" -p 5432 -U postgres -d "${DB}" -v ON_ERROR_STOP=1 "$@"; }
psql_n2() { psql -h "${NODE2_HOST}" -p 5432 -U postgres -d "${DB}" -v ON_ERROR_STOP=1 "$@"; }

# ---------------------------------------------------------------------------
# Wait for both nodes to accept connections.
# ---------------------------------------------------------------------------
for host in "${NODE1_HOST}" "${NODE2_HOST}"; do
    for _ in $(seq 1 60); do
        if pg_isready -h "${host}" -p 5432 -U postgres > /dev/null 2>&1; then
            break
        fi
        sleep 2
    done
done

# ---------------------------------------------------------------------------
# Publisher (node1): extensions, seed table + rows, publication.
# ---------------------------------------------------------------------------
echo "[logical] Preparing publisher (${NODE1_HOST})..."
psql_n1 <<-'EOSQL'
    -- Silence benign "already exists, skipping" NOTICEs from the idempotent
    -- IF NOT EXISTS DDL below. psql writes NOTICEs to stderr and the helper
    -- logs any stderr, so WARNING keeps clean re-runs from looking noisy.
    SET client_min_messages = warning;
    CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
    CREATE EXTENSION IF NOT EXISTS system_stats;
    CREATE TABLE IF NOT EXISTS documents (
        id       SERIAL PRIMARY KEY,
        content  TEXT NOT NULL,
        metadata JSONB
    );
    COMMENT ON TABLE documents IS 'RAG document store for E2E logical replication tests';
    INSERT INTO documents (content, metadata)
    SELECT 'PostgreSQL logical replication row ' || g, '{"source": "logical"}'::jsonb
    FROM generate_series(1, 3) AS g
    WHERE NOT EXISTS (SELECT 1 FROM documents);
EOSQL

if ! psql_n1 -tAc "SELECT 1 FROM pg_publication WHERE pubname = '${PUB}'" | grep -q 1; then
    echo "[logical] Creating publication ${PUB} on node1..."
    psql_n1 -c "CREATE PUBLICATION ${PUB} FOR ALL TABLES;"
else
    echo "[logical] Publication ${PUB} already exists."
fi

# ---------------------------------------------------------------------------
# Subscriber (node2): extensions, matching schema (logical replication does
# NOT create tables), subscription.
# ---------------------------------------------------------------------------
echo "[logical] Preparing subscriber (${NODE2_HOST})..."
psql_n2 <<-'EOSQL'
    -- Silence benign "already exists, skipping" NOTICEs (see node1 block).
    SET client_min_messages = warning;
    CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
    CREATE EXTENSION IF NOT EXISTS system_stats;
    CREATE TABLE IF NOT EXISTS documents (
        id       SERIAL PRIMARY KEY,
        content  TEXT NOT NULL,
        metadata JSONB
    );
EOSQL

if ! psql_n2 -tAc "SELECT 1 FROM pg_subscription WHERE subname = '${SUB}'" | grep -q 1; then
    echo "[logical] Creating subscription ${SUB} on node2..."
    psql_n2 -c "CREATE SUBSCRIPTION ${SUB} \
        CONNECTION 'host=${NODE1_HOST} port=5432 dbname=${DB} user=postgres password=${PG_PASSWORD}' \
        PUBLICATION ${PUB};"
else
    echo "[logical] Subscription ${SUB} already exists."
fi

# ---------------------------------------------------------------------------
# Wait for the initial data sync (node2 row count catches up to node1).
# ---------------------------------------------------------------------------
echo "[logical] Waiting for initial data sync..."
n1=0
n2=0
for _ in $(seq 1 30); do
    n1="$(psql_n1 -tAc 'SELECT count(*) FROM documents' | tr -d '[:space:]')"
    n2="$(psql_n2 -tAc 'SELECT count(*) FROM documents' | tr -d '[:space:]')"
    if [ "${n1}" = "${n2}" ] && [ "${n1}" -gt 0 ]; then
        echo "[logical] Replication healthy: node1=${n1} rows, node2=${n2} rows."
        echo ""
        echo "[logical] Cluster ready:"
        echo "[logical]   ${NODE1_HOST}:5432  ragdb publisher"
        echo "[logical]   pg-logical-node2:5432  ragdb subscriber"
        exit 0
    fi
    sleep 2
done

echo "[logical] WARNING: data not fully synced (node1=${n1}, node2=${n2})."
