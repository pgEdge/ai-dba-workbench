#!/usr/bin/env bash
#
# start-spock-replication.sh
#
# Sets up a THREE-node NATIVE SPOCK (multi-master) replication cluster across
# the three E2E spock nodes (separate containers):
#
#   pg-spock-node1  <->  pg-spock-node2  <->  pg-spock-node3   (full mesh)
#
# All three containers start with wal_level=logical, track_commit_timestamp
# and spock + pg_stat_statements preloaded (see docker-compose.yml). This
# script, run INSIDE the node1 container (which resolves pg-spock-node2 /
# pg-spock-node3 over the e2e docker network), performs the whole cluster
# bring-up:
#
#   1. Wait for all three nodes to accept connections.
#   2. On each node: create the monitoring extensions (spock,
#      pg_stat_statements, and — best-effort, non-fatal — system_stats) and
#      the seed `documents` table.
#   3. On each node: spock.node_create (guarded).
#   4. On each node: spock.repset_add_all_tables('default', {public}).
#   5. Full mesh: on each node create two subscriptions to the other two
#      (six spock.sub_create calls total, guarded).
#   6. Health gate: assert every node has two 'replicating' subscriptions,
#      then round-trip a sentinel row from node1 to node2 AND node3 and remove
#      it again.
#
# Idempotent: re-running reattaches to an already-configured cluster.
#

set -euo pipefail

PG_PASSWORD="${POSTGRES_PASSWORD:-postgres}"
export PGPASSWORD="${PG_PASSWORD}"

DB="ragdb"

# Host used to reach each node from *inside* node1. node1 is local; node2/node3
# are reached over the e2e network by their compose-service hostnames.
NODE1_HOST="127.0.0.1"
NODE2_HOST="${SPOCK_NODE2_HOST:-pg-spock-node2}"
NODE3_HOST="${SPOCK_NODE3_HOST:-pg-spock-node3}"

# Advertised DSN hostnames — the addresses OTHER nodes use to reach each node.
# These must be network-resolvable service names, never 127.0.0.1.
NODE1_ADVERTISE="${SPOCK_NODE1_HOST:-pg-spock-node1}"
NODE2_ADVERTISE="${SPOCK_NODE2_HOST:-pg-spock-node2}"
NODE3_ADVERTISE="${SPOCK_NODE3_HOST:-pg-spock-node3}"

HEALTH_TIMEOUT_SECONDS=180

# Run psql against a specific host as the postgres superuser on ragdb, aborting
# on the first SQL error.
psql_host() {
    local host="$1"
    shift
    psql -h "${host}" -p 5432 -U postgres -d "${DB}" -v ON_ERROR_STOP=1 "$@"
}

dsn_for() {
    # $1 = advertised host -> a spock DSN string with credentials.
    echo "host=$1 port=5432 dbname=${DB} user=postgres password=${PG_PASSWORD}"
}

# ---------------------------------------------------------------------------
# 1. Wait for all three nodes to accept connections.
# ---------------------------------------------------------------------------
echo "[spock] Waiting for all three nodes to accept connections..."
for host in "${NODE1_HOST}" "${NODE2_HOST}" "${NODE3_HOST}"; do
    for _ in $(seq 1 60); do
        if pg_isready -h "${host}" -p 5432 -U postgres > /dev/null 2>&1; then
            break
        fi
        sleep 2
    done
done

# ---------------------------------------------------------------------------
# 2. Per-node extensions and seed schema.
# ---------------------------------------------------------------------------
setup_node_schema() {
    local host="$1"
    echo "[spock] Preparing node schema on ${host}..."
    psql_host "${host}" <<-'EOSQL'
        -- Silence benign "already exists, skipping" NOTICEs from the idempotent
        -- IF NOT EXISTS DDL below (psql writes NOTICEs to stderr, which the
        -- helper logs), so clean re-runs do not look noisy.
        SET client_min_messages = warning;
        CREATE EXTENSION IF NOT EXISTS spock;
        CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
        CREATE TABLE IF NOT EXISTS documents (
            id       BIGINT PRIMARY KEY,
            content  TEXT NOT NULL,
            email    TEXT,
            metadata JSONB
        );
        CREATE UNIQUE INDEX IF NOT EXISTS documents_email_uk ON documents(email);
        COMMENT ON TABLE documents IS 'RAG document store for E2E Spock replication tests';
EOSQL

    # system_stats may not ship in the spock image; keep it strictly optional so
    # a missing extension logs a warning instead of aborting the bring-up.
    if psql -h "${host}" -p 5432 -U postgres -d "${DB}" -v ON_ERROR_STOP=1 \
        -c "SET client_min_messages = warning; CREATE EXTENSION IF NOT EXISTS system_stats;" \
        > /dev/null 2>&1; then
        echo "[spock] system_stats: created/present on ${host}."
    else
        echo "[spock] system_stats: WARNING unavailable on ${host}; continuing without it."
    fi
}

setup_node_schema "${NODE1_HOST}"
setup_node_schema "${NODE2_HOST}"
setup_node_schema "${NODE3_HOST}"

# ---------------------------------------------------------------------------
# 3. Create the spock node objects (guarded).
# ---------------------------------------------------------------------------
create_node() {
    local host="$1" node_name="$2" advertise="$3"
    if psql_host "${host}" -tAc \
        "SELECT 1 FROM spock.node WHERE node_name = '${node_name}'" \
        | grep -q 1; then
        echo "[spock] Node ${node_name} already exists on ${host}."
        return
    fi
    echo "[spock] Creating node ${node_name} on ${host}..."
    psql_host "${host}" -c \
        "SELECT spock.node_create(node_name := '${node_name}', dsn := '$(dsn_for "${advertise}")');"
}

create_node "${NODE1_HOST}" "n1" "${NODE1_ADVERTISE}"
create_node "${NODE2_HOST}" "n2" "${NODE2_ADVERTISE}"
create_node "${NODE3_HOST}" "n3" "${NODE3_ADVERTISE}"

# ---------------------------------------------------------------------------
# 4. Add all public tables to the default replication set on each node.
# repset_add_all_tables skips tables already present, so this is idempotent;
# tolerate a benign error on re-run rather than aborting.
# ---------------------------------------------------------------------------
add_all_tables() {
    local host="$1"
    echo "[spock] Adding public tables to the 'default' repset on ${host}..."
    if ! psql_host "${host}" -c \
        "SET client_min_messages = warning; SELECT spock.repset_add_all_tables('default', ARRAY['public']);" \
        > /dev/null 2>&1; then
        echo "[spock] repset_add_all_tables on ${host}: nothing new to add (idempotent)."
    fi
}

add_all_tables "${NODE1_HOST}"
add_all_tables "${NODE2_HOST}"
add_all_tables "${NODE3_HOST}"

# ---------------------------------------------------------------------------
# 5. Full mesh subscriptions (six total, guarded).
# ---------------------------------------------------------------------------
create_sub() {
    local host="$1" sub_name="$2" provider_advertise="$3"
    if psql_host "${host}" -tAc \
        "SELECT 1 FROM spock.subscription WHERE sub_name = '${sub_name}'" \
        | grep -q 1; then
        echo "[spock] Subscription ${sub_name} already exists on ${host}."
        return
    fi
    echo "[spock] Creating subscription ${sub_name} on ${host} (provider ${provider_advertise})..."
    psql_host "${host}" -c \
        "SELECT spock.sub_create(
            subscription_name := '${sub_name}',
            provider_dsn := '$(dsn_for "${provider_advertise}")',
            replication_sets := ARRAY['default','default_insert_only','ddl_sql'],
            synchronize_structure := false,
            synchronize_data := false,
            forward_origins := '{}'::text[],
            enabled := true
        );"
}

# node1 subscribes to node2 and node3
create_sub "${NODE1_HOST}" "sub_n1_from_n2" "${NODE2_ADVERTISE}"
create_sub "${NODE1_HOST}" "sub_n1_from_n3" "${NODE3_ADVERTISE}"
# node2 subscribes to node1 and node3
create_sub "${NODE2_HOST}" "sub_n2_from_n1" "${NODE1_ADVERTISE}"
create_sub "${NODE2_HOST}" "sub_n2_from_n3" "${NODE3_ADVERTISE}"
# node3 subscribes to node1 and node2
create_sub "${NODE3_HOST}" "sub_n3_from_n1" "${NODE1_ADVERTISE}"
create_sub "${NODE3_HOST}" "sub_n3_from_n2" "${NODE2_ADVERTISE}"

# ---------------------------------------------------------------------------
# 6. Health gate.
# ---------------------------------------------------------------------------
replicating_count() {
    local host="$1"
    psql_host "${host}" -tAc \
        "SELECT count(*) FROM spock.sub_show_status() WHERE status = 'replicating'" \
        | tr -d '[:space:]'
}

echo "[spock] Waiting for every node to reach two 'replicating' subscriptions..."
deadline=$(( $(date +%s) + HEALTH_TIMEOUT_SECONDS ))
mesh_ready=0
while [ "$(date +%s)" -lt "${deadline}" ]; do
    c1="$(replicating_count "${NODE1_HOST}")"
    c2="$(replicating_count "${NODE2_HOST}")"
    c3="$(replicating_count "${NODE3_HOST}")"
    if [ "${c1}" = "2" ] && [ "${c2}" = "2" ] && [ "${c3}" = "2" ]; then
        mesh_ready=1
        break
    fi
    sleep 3
done

if [ "${mesh_ready}" != "1" ]; then
    echo "[spock] WARNING: mesh not fully replicating (n1=${c1:-?}, n2=${c2:-?}, n3=${c3:-?})."
    psql_host "${NODE1_HOST}" -c "SELECT * FROM spock.sub_show_status();" || true
    exit 1
fi

echo "[spock] All nodes report two replicating subscriptions."

# Round-trip: insert a sentinel on node1, confirm it reaches node2 AND node3,
# then delete it and confirm removal propagates everywhere.
SENTINEL_ID=999000
echo "[spock] Round-tripping sentinel row id=${SENTINEL_ID} from node1..."
psql_host "${NODE1_HOST}" -c \
    "INSERT INTO documents (id, content, email, metadata)
     VALUES (${SENTINEL_ID}, 'spock health sentinel', 'sentinel@example.com',
             '{\"source\": \"spock\"}'::jsonb)
     ON CONFLICT (id) DO NOTHING;"

sentinel_present() {
    local host="$1"
    psql_host "${host}" -tAc \
        "SELECT count(*) FROM documents WHERE id = ${SENTINEL_ID}" \
        | tr -d '[:space:]'
}

deadline=$(( $(date +%s) + HEALTH_TIMEOUT_SECONDS ))
insert_ok=0
while [ "$(date +%s)" -lt "${deadline}" ]; do
    if [ "$(sentinel_present "${NODE2_HOST}")" = "1" ] \
        && [ "$(sentinel_present "${NODE3_HOST}")" = "1" ]; then
        insert_ok=1
        break
    fi
    sleep 2
done

if [ "${insert_ok}" != "1" ]; then
    echo "[spock] WARNING: sentinel did not reach node2/node3."
    exit 1
fi
echo "[spock] Sentinel replicated to node2 and node3."

echo "[spock] Removing sentinel from node1 and awaiting propagation..."
psql_host "${NODE1_HOST}" -c \
    "DELETE FROM documents WHERE id = ${SENTINEL_ID};"

deadline=$(( $(date +%s) + HEALTH_TIMEOUT_SECONDS ))
delete_ok=0
while [ "$(date +%s)" -lt "${deadline}" ]; do
    if [ "$(sentinel_present "${NODE2_HOST}")" = "0" ] \
        && [ "$(sentinel_present "${NODE3_HOST}")" = "0" ]; then
        delete_ok=1
        break
    fi
    sleep 2
done

if [ "${delete_ok}" != "1" ]; then
    echo "[spock] WARNING: sentinel deletion did not propagate to node2/node3."
    exit 1
fi

echo "[spock] Replication healthy: 3-node full mesh, all subscriptions replicating, round-trip verified."
echo ""
echo "[spock] Cluster ready:"
echo "[spock]   ${NODE1_ADVERTISE}:5432  ragdb node n1"
echo "[spock]   ${NODE2_ADVERTISE}:5432  ragdb node n2"
echo "[spock]   ${NODE3_ADVERTISE}:5432  ragdb node n3"
exit 0
