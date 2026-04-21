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

# -- Arguments ------------------------------------------------------

CONTAINER="${1:-wt-datastore}"
DATABASE="${2:-ai_workbench}"

# -- Colors ---------------------------------------------------------

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

# -- Helper: run SQL via docker exec --------------------------------

run_sql() {
    docker exec "$CONTAINER" \
        psql -U postgres -d "$DATABASE" -X -A -t -q -c "$1" 2>/dev/null
}

# Run a SQL script (multi-statement, from stdin) via docker exec.
# Captures stdout (query output); stderr (notices, errors) goes to
# the terminal so that errors are visible if the script aborts.
run_sql_script() {
    docker exec -i "$CONTAINER" \
        psql -U postgres -d "$DATABASE" -X -A -t -q
}

# -- Pre-flight check -----------------------------------------------

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

# -- Step 1: Find the maximum collected_at across all metrics --------

echo ""
header "Step 1: Finding the most recent timestamp in metrics..."

# Build a UNION ALL across all parent tables in the metrics schema.
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

# -- Step 2: Calculate the offset -----------------------------------

echo ""
header "Step 2: Calculating time offset..."

OFFSET=$(run_sql "
    SELECT (now() - interval '2 minutes') - '$MAX_TS'::timestamptz;
")

info "Offset to apply: $OFFSET"

# Check whether the offset is negligible (less than 60 seconds).
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

# -- Step 3: Rebase partitioned metrics tables -----------------------
#
# Strategy: run a single PL/pgSQL DO block that processes every
# partitioned table in the metrics schema.  For each parent table:
#
#   1. Copy all data into a temp table, shifting collected_at and
#      every other timestamp column by the offset.
#   2. Detach and drop all child partitions.
#   3. Create new weekly partitions covering the shifted date range.
#      Weekly boundaries use Monday 00:00 UTC, matching the collector
#      (see collector/src/probes/base.go EnsurePartition).
#   4. INSERT the shifted data from the temp table into the parent;
#      PostgreSQL routes each row to the correct new partition.
#   5. Drop the temp table.
#
# The entire operation runs in one database round-trip.

echo ""
header "Step 3: Rebasing partitioned metrics tables..."

# The offset is passed into the DO block via a session variable so
# that both the metrics and public steps use the same value (after
# Step 3, the metrics timestamps have already been shifted, so
# recalculating would yield a near-zero offset).

METRICS_RESULT=$(run_sql_script <<EOSQL
SET app.rebase_offset = '${OFFSET}';

CREATE TEMP TABLE _rebase_result (tbl_count INT, row_count BIGINT);

DO \$\$
DECLARE
    v_offset       INTERVAL;
    v_parent       TEXT;
    v_child        TEXT;
    v_col          TEXT;
    v_cols         TEXT;
    v_ts_cols      TEXT[];
    v_select_parts TEXT;
    v_range_lo     DATE;
    v_range_hi     DATE;
    v_week         DATE;
    v_week_end     DATE;
    v_part_name    TEXT;
    v_tbl_count    INT := 0;
    v_row_count    BIGINT := 0;
    v_tbl_rows     BIGINT;
BEGIN
    v_offset := current_setting('app.rebase_offset')::interval;

    -- Process each partitioned parent table.
    FOR v_parent IN
        SELECT c.relname
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'metrics' AND c.relkind = 'p'
        ORDER BY c.relname
    LOOP
        -- Collect all column names in ordinal order.
        SELECT string_agg(column_name, ', ' ORDER BY ordinal_position)
        INTO v_cols
        FROM information_schema.columns
        WHERE table_schema = 'metrics' AND table_name = v_parent;

        -- Collect timestamp column names for this table.
        SELECT array_agg(column_name ORDER BY ordinal_position)
        INTO v_ts_cols
        FROM information_schema.columns
        WHERE table_schema = 'metrics'
          AND table_name = v_parent
          AND data_type IN (
              'timestamp with time zone',
              'timestamp without time zone'
          );

        IF v_ts_cols IS NULL OR v_cols IS NULL THEN
            CONTINUE;
        END IF;

        -- Build the SELECT list: shift every timestamp column, pass
        -- others through unchanged.
        v_select_parts := '';
        FOR v_col IN
            SELECT column_name
            FROM information_schema.columns
            WHERE table_schema = 'metrics' AND table_name = v_parent
            ORDER BY ordinal_position
        LOOP
            IF v_select_parts <> '' THEN
                v_select_parts := v_select_parts || ', ';
            END IF;

            IF v_col = ANY(v_ts_cols) THEN
                v_select_parts := v_select_parts
                    || format('%I + %L::interval AS %I', v_col, v_offset, v_col);
            ELSE
                v_select_parts := v_select_parts || format('%I', v_col);
            END IF;
        END LOOP;

        -- 1. Copy shifted data into a temporary table.
        EXECUTE format(
            'CREATE TEMP TABLE _rebase_tmp AS SELECT %s FROM metrics.%I',
            v_select_parts, v_parent
        );

        GET DIAGNOSTICS v_tbl_rows = ROW_COUNT;

        -- 2. Detach and drop every child partition.
        FOR v_child IN
            SELECT c.relname
            FROM pg_inherits i
            JOIN pg_class c ON c.oid = i.inhrelid
            JOIN pg_class p ON p.oid = i.inhparent
            JOIN pg_namespace n ON n.oid = p.relnamespace
            WHERE n.nspname = 'metrics' AND p.relname = v_parent
            ORDER BY c.relname
        LOOP
            EXECUTE format(
                'ALTER TABLE metrics.%I DETACH PARTITION metrics.%I',
                v_parent, v_child
            );
            EXECUTE format('DROP TABLE metrics.%I', v_child);
        END LOOP;

        -- 3. Determine the date range of shifted data and create
        --    weekly partitions (Monday boundaries, matching the
        --    collector's EnsurePartition logic).
        EXECUTE 'SELECT date_trunc(''week'', MIN(collected_at))::date, '
             || '(date_trunc(''week'', MAX(collected_at)) + interval ''7 days'')::date '
             || 'FROM _rebase_tmp'
        INTO v_range_lo, v_range_hi;

        IF v_range_lo IS NOT NULL AND v_range_hi IS NOT NULL THEN
            v_week := v_range_lo;
            WHILE v_week < v_range_hi LOOP
                v_week_end := v_week + 7;
                v_part_name := v_parent || '_' || to_char(v_week, 'YYYYMMDD');

                EXECUTE format(
                    'CREATE TABLE IF NOT EXISTS metrics.%I '
                    'PARTITION OF metrics.%I '
                    'FOR VALUES FROM (%L) TO (%L)',
                    v_part_name, v_parent,
                    v_week::timestamp, v_week_end::timestamp
                );

                v_week := v_week_end;
            END LOOP;

            -- 4. Insert shifted data; PG routes to correct partitions.
            EXECUTE format(
                'INSERT INTO metrics.%I (%s) SELECT %s FROM _rebase_tmp',
                v_parent, v_cols, v_cols
            );
        END IF;

        -- 5. Drop temp table explicitly for the next iteration.
        DROP TABLE IF EXISTS _rebase_tmp;

        v_tbl_count := v_tbl_count + 1;
        v_row_count := v_row_count + v_tbl_rows;
    END LOOP;

    INSERT INTO _rebase_result VALUES (v_tbl_count, v_row_count);
END;
\$\$;

SELECT tbl_count || '|' || row_count FROM _rebase_result;
DROP TABLE _rebase_result;
EOSQL
)

# Output contains one line: "tbl_count|row_count".
METRICS_LINE=$(echo "$METRICS_RESULT" | grep '|' | tail -1 || true)
METRICS_COUNT=$(echo "$METRICS_LINE" | cut -d'|' -f1)
METRICS_ROWS=$(echo "$METRICS_LINE" | cut -d'|' -f2)
METRICS_COUNT="${METRICS_COUNT:-0}"
METRICS_ROWS="${METRICS_ROWS:-0}"

info "Rebased $METRICS_COUNT metrics tables ($METRICS_ROWS rows shifted)."

# -- Step 4: Rebase public schema tables -----------------------------
#
# Public-schema tables are not partitioned, so a simple UPDATE suffices.
# We shift every timestamp column except in schema_version (migration
# metadata).  The offset is passed via the shell variable calculated
# in Step 2, before the metrics data was modified.

echo ""
header "Step 4: Rebasing public schema tables..."

PUBLIC_RESULT=$(run_sql_script <<EOSQL
SET app.rebase_offset = '${OFFSET}';

CREATE TEMP TABLE _rebase_result (tbl_count INT, row_count BIGINT);

DO \$\$
DECLARE
    v_offset     INTERVAL;
    v_tbl        TEXT;
    v_set_clause TEXT;
    v_where      TEXT;
    v_col        TEXT;
    v_count      INT := 0;
    v_rows       BIGINT := 0;
    v_tbl_rows   BIGINT;
BEGIN
    v_offset := current_setting('app.rebase_offset')::interval;

    FOR v_tbl IN
        SELECT DISTINCT t.table_name
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
        ORDER BY t.table_name
    LOOP
        -- Build SET and WHERE clauses for all timestamp columns.
        v_set_clause := '';
        v_where := '';

        FOR v_col IN
            SELECT column_name
            FROM information_schema.columns
            WHERE table_schema = 'public'
              AND table_name   = v_tbl
              AND data_type IN (
                  'timestamp with time zone',
                  'timestamp without time zone'
              )
            ORDER BY ordinal_position
        LOOP
            IF v_set_clause <> '' THEN
                v_set_clause := v_set_clause || ', ';
                v_where := v_where || ' OR ';
            END IF;
            v_set_clause := v_set_clause
                || format('%I = %I + %L::interval', v_col, v_col, v_offset);
            v_where := v_where
                || format('%I IS NOT NULL', v_col);
        END LOOP;

        IF v_set_clause = '' THEN
            CONTINUE;
        END IF;

        EXECUTE format(
            'UPDATE public.%I SET %s WHERE %s',
            v_tbl, v_set_clause, v_where
        );

        GET DIAGNOSTICS v_tbl_rows = ROW_COUNT;

        IF v_tbl_rows > 0 THEN
            v_count := v_count + 1;
            v_rows  := v_rows + v_tbl_rows;
        END IF;
    END LOOP;

    INSERT INTO _rebase_result VALUES (v_count, v_rows);
END;
\$\$;

SELECT tbl_count || '|' || row_count FROM _rebase_result;
DROP TABLE _rebase_result;
EOSQL
)

PUBLIC_LINE=$(echo "$PUBLIC_RESULT" | grep '|' | tail -1 || true)
PUBLIC_COUNT=$(echo "$PUBLIC_LINE" | cut -d'|' -f1)
PUBLIC_ROWS=$(echo "$PUBLIC_LINE" | cut -d'|' -f2)
PUBLIC_COUNT="${PUBLIC_COUNT:-0}"
PUBLIC_ROWS="${PUBLIC_ROWS:-0}"

info "Rebased $PUBLIC_COUNT public tables ($PUBLIC_ROWS rows shifted)."

# -- Summary ---------------------------------------------------------

TOTAL_ROWS=$((METRICS_ROWS + PUBLIC_ROWS))

echo ""
header "Done"
echo ""
info "Shifted all timestamps by: $OFFSET"
info "Total rows updated: $TOTAL_ROWS"
detail "Metrics tables: $METRICS_COUNT ($METRICS_ROWS rows)"
detail "Public tables:  $PUBLIC_COUNT ($PUBLIC_ROWS rows)"
echo ""
