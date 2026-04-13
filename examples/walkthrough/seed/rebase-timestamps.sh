#!/usr/bin/env bash
#-------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#-------------------------------------------------------------------------
#
# rebase-timestamps.sh -- Shift all timestamps in a walkthrough datastore
# so that the most recent data point is 2 minutes before "now".
#
# Run this AFTER the seed SQL has been loaded and BEFORE the collector
# starts gathering new data.
#
# Usage:
#   ./rebase-timestamps.sh [container] [database]
#
# Arguments:
#   container   Docker container name (default: wt-datastore)
#   database    Database name         (default: ai_workbench)

set -euo pipefail

# ── Arguments ───────────────────────────────────────────────────────

CONTAINER="${1:-wt-datastore}"
DATABASE="${2:-ai_workbench}"

# ── Colors ──────────────────────────────────────────────────────────

BOLD='\033[1m'
TEAL='\033[38;5;30m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
RED='\033[0;31m'
DIM='\033[2m'
RESET='\033[0m'

info()    { echo -e "${GREEN}$1${RESET}"; }
warn()    { echo -e "${YELLOW}$1${RESET}"; }
error()   { echo -e "${RED}$1${RESET}"; }
header()  { echo -e "${BOLD}${TEAL}$1${RESET}"; }
detail()  { echo -e "${DIM}  $1${RESET}"; }

# ── Helper: run SQL via docker exec ────────────────────────────────

run_sql() {
    docker exec "$CONTAINER" \
        psql -U postgres -d "$DATABASE" -X -A -t -q -c "$1" 2>/dev/null
}

# ── Pre-flight check ───────────────────────────────────────────────

echo ""
header "Rebase Timestamps"
echo ""
detail "Container: $CONTAINER"
detail "Database:  $DATABASE"
echo ""

if ! docker exec "$CONTAINER" pg_isready -U postgres -q 2>/dev/null; then
    error "Cannot connect to PostgreSQL in container '$CONTAINER'."
    exit 1
fi

info "Connected to datastore."

# ── Step 1: Find the maximum collected_at across all metrics ───────

echo ""
header "Step 1: Finding the most recent timestamp in metrics..."

# Build a UNION ALL query across all parent tables in the metrics
# schema that have a collected_at column.
PARENT_TABLES=$(run_sql "
    SELECT c.relname
    FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = 'metrics'
      AND c.relkind = 'p'
    ORDER BY c.relname;
")

if [[ -z "$PARENT_TABLES" ]]; then
    warn "No partitioned tables found in the metrics schema."
    warn "Is the seed data loaded?"
    exit 1
fi

# Build the UNION ALL query to find the global max collected_at
UNION_PARTS=""
while IFS= read -r tbl; do
    [[ -z "$tbl" ]] && continue
    if [[ -n "$UNION_PARTS" ]]; then
        UNION_PARTS="$UNION_PARTS UNION ALL "
    fi
    UNION_PARTS="${UNION_PARTS}SELECT MAX(collected_at) AS ts FROM metrics.${tbl}"
done <<< "$PARENT_TABLES"

MAX_TS=$(run_sql "SELECT MAX(ts) FROM ($UNION_PARTS) sub;")

if [[ -z "$MAX_TS" || "$MAX_TS" == "" ]]; then
    warn "No timestamp data found in metrics tables."
    warn "Is the seed data loaded?"
    exit 1
fi

info "Most recent metric timestamp: $MAX_TS"

# ── Step 2: Calculate the offset ───────────────────────────────────

echo ""
header "Step 2: Calculating time offset..."

OFFSET=$(run_sql "
    SELECT (now() - interval '2 minutes') - '$MAX_TS'::timestamptz;
")

info "Offset to apply: $OFFSET"

# Check whether the offset is negligible (less than 60 seconds)
IS_NEGLIGIBLE=$(run_sql "
    SELECT CASE
        WHEN ABS(EXTRACT(EPOCH FROM '$OFFSET'::interval)) < 60
        THEN 'yes' ELSE 'no'
    END;
")

if [[ "$IS_NEGLIGIBLE" == "yes" ]]; then
    echo ""
    info "Timestamps are already current (offset < 60 seconds). Nothing to do."
    echo ""
    exit 0
fi

# ── Step 3: Rebase partitioned metrics tables ──────────────────────

echo ""
header "Step 3: Rebasing partitioned metrics tables..."

# Strategy for partitioned tables:
#
# PostgreSQL enforces partition bounds on child tables, so we cannot
# UPDATE the partition key (collected_at) in place.  Shifting data by
# a few days could also make two formerly non-overlapping children
# require overlapping bounds.
#
# The safe approach:
#   1. Detach ALL children from the parent.
#   2. Update timestamp columns in each detached child.
#   3. Compute the full date range across all shifted children.
#   4. Create new weekly partitions on the (now empty) parent.
#   5. INSERT the shifted data into the parent; PostgreSQL routes
#      each row to the correct new partition.
#   6. Drop the old detached children.
#
# The collector will create further weekly partitions as new data
# arrives, so this is fully compatible with ongoing collection.

# Get all timestamp columns for each metrics parent table
METRICS_TS_COLS=$(run_sql "
    SELECT table_name, column_name
    FROM information_schema.columns
    WHERE table_schema = 'metrics'
      AND data_type IN (
          'timestamp with time zone',
          'timestamp without time zone'
      )
      AND table_name IN (
          SELECT c.relname
          FROM pg_class c
          JOIN pg_namespace n ON n.oid = c.relnamespace
          WHERE n.nspname = 'metrics'
            AND c.relkind = 'p'
      )
    ORDER BY table_name, column_name;
")

# Get the column list for each parent table (all columns, for INSERT)
get_columns() {
    local tbl="$1"
    run_sql "
        SELECT string_agg(column_name, ', ' ORDER BY ordinal_position)
        FROM information_schema.columns
        WHERE table_schema = 'metrics'
          AND table_name   = '$tbl';
    "
}

METRICS_COUNT=0
METRICS_ROWS=0

while IFS= read -r parent_table; do
    [[ -z "$parent_table" ]] && continue

    # Find child partitions for this parent
    CHILDREN=$(run_sql "
        SELECT c.relname
        FROM pg_inherits i
        JOIN pg_class c ON c.oid = i.inhrelid
        JOIN pg_class p ON p.oid = i.inhparent
        JOIN pg_namespace n ON n.oid = p.relnamespace
        WHERE n.nspname = 'metrics'
          AND p.relname = '$parent_table'
        ORDER BY c.relname;
    ")

    if [[ -z "$CHILDREN" ]]; then
        continue
    fi

    # Gather the SET clause for timestamp columns
    TS_SET=""
    while IFS='|' read -r tname cname; do
        [[ "$tname" != "$parent_table" ]] && continue
        if [[ -n "$TS_SET" ]]; then
            TS_SET="$TS_SET, "
        fi
        TS_SET="${TS_SET}${cname} = ${cname} + '$OFFSET'::interval"
    done <<< "$METRICS_TS_COLS"

    if [[ -z "$TS_SET" ]]; then
        continue
    fi

    detail "Rebasing metrics.$parent_table ..."

    # Phase 1: Detach all children and update their timestamps
    CHILD_LIST=()
    TABLE_ROWS=0
    while IFS= read -r child; do
        [[ -z "$child" ]] && continue
        CHILD_LIST+=("$child")

        run_sql "ALTER TABLE metrics.$parent_table DETACH PARTITION metrics.$child;" >/dev/null

        ROW_COUNT=$(run_sql "
            UPDATE metrics.$child SET $TS_SET;
            SELECT COUNT(*) FROM metrics.$child;
        " | tail -1)

        TABLE_ROWS=$((TABLE_ROWS + ROW_COUNT))
    done <<< "$CHILDREN"

    METRICS_ROWS=$((METRICS_ROWS + TABLE_ROWS))

    if [[ ${#CHILD_LIST[@]} -eq 0 ]]; then
        continue
    fi

    # Phase 2: Find the date range of all shifted data and create
    # new weekly partitions.  Build a UNION to scan all detached
    # children for their min/max collected_at.
    RANGE_UNION=""
    for child in "${CHILD_LIST[@]}"; do
        if [[ -n "$RANGE_UNION" ]]; then
            RANGE_UNION="$RANGE_UNION UNION ALL "
        fi
        RANGE_UNION="${RANGE_UNION}SELECT MIN(collected_at) AS lo, MAX(collected_at) AS hi FROM metrics.$child"
    done

    DATE_RANGE=$(run_sql "
        SELECT
            date_trunc('week', MIN(lo))::date,
            (date_trunc('week', MAX(hi)) + interval '7 days')::date
        FROM ($RANGE_UNION) sub;
    ")

    RANGE_START=$(echo "$DATE_RANGE" | cut -d'|' -f1)
    RANGE_END=$(echo "$DATE_RANGE" | cut -d'|' -f2)

    if [[ -z "$RANGE_START" || -z "$RANGE_END" ]]; then
        warn "    No data in $parent_table children -- skipping."
        continue
    fi

    # Generate weekly partitions covering [RANGE_START, RANGE_END)
    WEEKS=$(run_sql "
        SELECT d::date
        FROM generate_series(
            '$RANGE_START'::date,
            '$RANGE_END'::date - 1,
            '7 days'::interval
        ) AS d;
    ")

    while IFS= read -r week_start; do
        [[ -z "$week_start" ]] && continue
        week_end=$(run_sql "SELECT ('$week_start'::date + 7)::date;")
        part_name="${parent_table}_$(echo "$week_start" | tr -d '-')"

        # Create the partition only if it does not already exist
        run_sql "
            CREATE TABLE IF NOT EXISTS metrics.$part_name
            PARTITION OF metrics.$parent_table
            FOR VALUES FROM ('$week_start') TO ('$week_end');
        " >/dev/null
    done <<< "$WEEKS"

    # Phase 3: INSERT shifted data from old children into parent.
    # Get the column list from the parent table definition.
    COLS=$(get_columns "$parent_table")

    for child in "${CHILD_LIST[@]}"; do
        run_sql "INSERT INTO metrics.$parent_table ($COLS) SELECT $COLS FROM metrics.$child;" >/dev/null
    done

    # Phase 4: Drop the old detached children.
    for child in "${CHILD_LIST[@]}"; do
        run_sql "DROP TABLE IF EXISTS metrics.$child;" >/dev/null
    done

    METRICS_COUNT=$((METRICS_COUNT + 1))

done <<< "$PARENT_TABLES"

info "Rebased $METRICS_COUNT metrics tables ($METRICS_ROWS rows updated)."

# ── Step 4: Rebase public schema tables ────────────────────────────

echo ""
header "Step 4: Rebasing public schema tables..."

# Tables and their timestamp columns that should be shifted.
# We skip:
#   - schema_version.applied_at (migration metadata)
#   - metric_definitions (no timestamps)
#   - alerter_settings.updated_at (singleton config, not event data)
#
# We include tables that have event/time-series semantics:
#   alerts, alert_acknowledgments, alert_rules, alert_thresholds,
#   anomaly_candidates, anomaly_embeddings, blackouts,
#   blackout_schedules, correlation_groups, metric_baselines,
#   notification_history, notification_reminder_state,
#   notification_channels, notification_channel_overrides,
#   connection_notification_channels, email_recipients,
#   conversations, chat_memories, probe_availability,
#   probe_configs, connections, clusters, cluster_groups,
#   cluster_node_relationships

PUBLIC_COUNT=0
PUBLIC_ROWS=0

# Dynamically discover all public tables with timestamp columns,
# excluding schema_version (migration metadata).
PUBLIC_TS_DATA=$(run_sql "
    SELECT t.table_name,
           string_agg(c.column_name, ',')
    FROM information_schema.tables t
    JOIN information_schema.columns c
         ON c.table_schema = t.table_schema
        AND c.table_name   = t.table_name
    WHERE t.table_schema = 'public'
      AND t.table_type   = 'BASE TABLE'
      AND t.table_name  != 'schema_version'
      AND c.data_type IN (
          'timestamp with time zone',
          'timestamp without time zone'
      )
    GROUP BY t.table_name
    ORDER BY t.table_name;
")

while IFS='|' read -r tbl cols; do
    [[ -z "$tbl" ]] && continue

    # Build SET clause
    SET_CLAUSE=""
    IFS=',' read -ra COL_ARRAY <<< "$cols"
    for col in "${COL_ARRAY[@]}"; do
        col=$(echo "$col" | xargs)  # trim whitespace
        [[ -z "$col" ]] && continue
        if [[ -n "$SET_CLAUSE" ]]; then
            SET_CLAUSE="$SET_CLAUSE, "
        fi
        SET_CLAUSE="${SET_CLAUSE}${col} = ${col} + '$OFFSET'::interval"
    done

    if [[ -z "$SET_CLAUSE" ]]; then
        continue
    fi

    # Only update rows where at least one timestamp column is not null
    WHERE_PARTS=""
    for col in "${COL_ARRAY[@]}"; do
        col=$(echo "$col" | xargs)
        [[ -z "$col" ]] && continue
        if [[ -n "$WHERE_PARTS" ]]; then
            WHERE_PARTS="$WHERE_PARTS OR "
        fi
        WHERE_PARTS="${WHERE_PARTS}${col} IS NOT NULL"
    done

    ROW_COUNT=$(run_sql "
        WITH updated AS (
            UPDATE public.$tbl SET $SET_CLAUSE WHERE $WHERE_PARTS RETURNING 1
        )
        SELECT COUNT(*) FROM updated;
    ")

    if [[ "$ROW_COUNT" -gt 0 ]]; then
        detail "$tbl: $ROW_COUNT rows"
        PUBLIC_ROWS=$((PUBLIC_ROWS + ROW_COUNT))
        PUBLIC_COUNT=$((PUBLIC_COUNT + 1))
    fi

done <<< "$PUBLIC_TS_DATA"

info "Rebased $PUBLIC_COUNT public tables ($PUBLIC_ROWS rows updated)."

# ── Summary ─────────────────────────────────────────────────────────

TOTAL_ROWS=$((METRICS_ROWS + PUBLIC_ROWS))

echo ""
header "Done"
echo ""
info "Shifted all timestamps by: $OFFSET"
info "Total rows updated: $TOTAL_ROWS"
detail "Metrics tables: $METRICS_COUNT ($METRICS_ROWS rows)"
detail "Public tables:  $PUBLIC_COUNT ($PUBLIC_ROWS rows)"
echo ""
