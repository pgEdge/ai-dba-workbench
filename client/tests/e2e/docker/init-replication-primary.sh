#!/usr/bin/env bash
#
# init-replication-primary.sh
#
# Primary initialisation script for the replication cluster.
# Mounted into /docker-entrypoint-initdb.d/ and executed automatically
# by the official PostgreSQL entrypoint after cluster initialisation.
#
# Creates:
#   - replicator role with REPLICATION privilege
#   - ragdb database with pgvector extension, documents table, seed data
#   - pg_hba.conf entries for replication access from the Docker network
#
set -euo pipefail

REPLICATOR_PASSWORD="${REPLICATOR_PASSWORD:-replicator}"

echo "[replication-init] Creating replicator role..."
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
    CREATE ROLE replicator WITH REPLICATION LOGIN ENCRYPTED PASSWORD '${REPLICATOR_PASSWORD}';
EOSQL

echo "[replication-init] Configuring pg_hba.conf for replication..."
{
    echo ""
    echo "# Replication access from the Docker e2e-network"
    echo "host  replication  replicator  0.0.0.0/0  md5"
    echo "host  all          all         0.0.0.0/0  md5"
} >> "${PGDATA}/pg_hba.conf"

pg_ctl reload -D "${PGDATA}"
echo "[replication-init] pg_hba.conf reloaded."

echo "[replication-init] Creating ragdb database..."
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres <<-EOSQL
    CREATE DATABASE ragdb;
EOSQL

echo "[replication-init] Setting up ragdb schema and seed data..."
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname ragdb <<-EOSQL
    CREATE EXTENSION IF NOT EXISTS vector;

    CREATE TABLE documents (
        id       SERIAL PRIMARY KEY,
        content  TEXT NOT NULL,
        embedding vector(768),
        metadata JSONB
    );

    COMMENT ON TABLE documents IS 'RAG document store for E2E replication tests';

    INSERT INTO documents (content, metadata) VALUES
        ('PostgreSQL is a powerful open-source relational database',
         '{"source": "docs"}'),
        ('pgvector enables vector similarity search in PostgreSQL',
         '{"source": "docs"}'),
        ('RAG combines retrieval with generation for AI applications',
         '{"source": "docs"}');
EOSQL

echo "[replication-init] Primary initialisation complete."
echo "[replication-init]   replicator role  : created"
echo "[replication-init]   ragdb database   : created with 3 seed documents"
echo "[replication-init]   pg_hba.conf      : updated for Docker network replication"
