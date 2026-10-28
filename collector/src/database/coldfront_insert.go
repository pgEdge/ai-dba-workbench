/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package database

import (
	"context"
	"fmt"
)

// createColdFrontInsertHelperSQL creates the function the collector
// writes metrics through under ColdFront.
//
// ColdFront rewrites a cold-tier INSERT that has bound parameters into
// format(<template>, $1, $2, ...) passed to duckdb.raw_query, so one
// statement can carry at most 99 parameters, and a probe batch of 100
// rows needs thousands. Reading the rows inside DuckDB does not work
// either: DuckDB has no jsonb_populate_recordset, and cannot unnest
// several VARCHAR[] lists. A literal multi-row INSERT works, so the rows
// travel as one jsonb parameter and this function builds that literal
// INSERT on the server and EXECUTEs it, where ColdFront's hook turns it
// into one Iceberg commit.
//
// The function writes only to a view ColdFront has registered in the
// metrics schema. The column list and types come from the catalog, in
// attribute order, never from the JSON keys; identifiers are quoted with
// %I and every value with quote_nullable, which renders NULL or a
// quoted literal that is then cast back to the column's own type. It
// does not read the view, which ColdFront refuses inside a function.
const createColdFrontInsertHelperSQL = `
CREATE OR REPLACE FUNCTION public.workbench_coldfront_insert(p_table text, p_rows jsonb)
RETURNS void
LANGUAGE plpgsql
SECURITY INVOKER
SET search_path = pg_catalog, pg_temp
AS $fn$
DECLARE
    v_rel   text;
    v_cols  text;
    v_exprs text;
    v_vals  text;
BEGIN
    IF p_rows IS NULL OR jsonb_typeof(p_rows) <> 'array' THEN
        RAISE EXCEPTION 'workbench_coldfront_insert: p_rows must be a JSON array';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM coldfront.tiered_views
                   WHERE schema_name = 'metrics' AND relname = p_table) THEN
        RAISE EXCEPTION 'workbench_coldfront_insert: metrics.% is not a ColdFront metrics table', p_table;
    END IF;
    IF jsonb_array_length(p_rows) = 0 THEN
        RETURN;
    END IF;

    v_rel := format('%I.%I', 'metrics', p_table);

    -- Concatenating one quote_nullable per column, rather than passing
    -- every column to a single format() call, keeps a wide table clear of
    -- the 100-argument limit on function calls.
    SELECT string_agg(format('%I', a.attname), ', ' ORDER BY a.attnum),
           string_agg(format('quote_nullable(r.%I) || %L',
                             a.attname, '::' || format_type(a.atttypid, a.atttypmod)),
                      ' || '', '' || ' ORDER BY a.attnum)
      INTO v_cols, v_exprs
      FROM pg_attribute a
     WHERE a.attrelid = v_rel::regclass AND a.attnum > 0 AND NOT a.attisdropped;

    EXECUTE format(
        'SELECT string_agg(''('' || %s || '')'', '', '') FROM jsonb_populate_recordset(NULL::%s, $1) AS r',
        v_exprs, v_rel)
      INTO v_vals
      USING p_rows;

    EXECUTE format('INSERT INTO %s (%s) VALUES %s', v_rel, v_cols, v_vals);
END
$fn$;

COMMENT ON FUNCTION public.workbench_coldfront_insert(text, jsonb) IS
    'Inserts a JSON array of rows into a ColdFront metrics view as one literal multi-row INSERT, because ColdFront caps a cold-tier INSERT at 99 bound parameters. Used only by the collector when use_coldfront_for_metrics is enabled; refuses any relation that is not a ColdFront view in the metrics schema.';
`

// EnsureColdFrontInsertHelper creates or replaces
// public.workbench_coldfront_insert, the function StoreMetrics writes
// through under ColdFront. It runs at every startup rather than in a
// migration, so the function always matches the collector that calls it
// and the default-mode schema gains nothing.
func EnsureColdFrontInsertHelper(ctx context.Context, e ddlExecer) error {
	if _, err := e.Exec(ctx, createColdFrontInsertHelperSQL); err != nil {
		return fmt.Errorf("failed to create public.workbench_coldfront_insert: %w", err)
	}
	return nil
}
