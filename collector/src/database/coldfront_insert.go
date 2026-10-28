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

// createColdFrontInsertHelperSQL creates the two functions the collector
// writes metrics through under ColdFront.
//
// ColdFront rewrites a cold-tier INSERT that has bound parameters into
// format(<template>, $1, $2, ...) passed to duckdb.raw_query, so one
// statement can carry at most 99 parameters, and a probe batch of 100
// rows needs thousands. Reading the rows inside DuckDB does not work
// either: DuckDB has no jsonb_populate_recordset, and cannot unnest
// several VARCHAR[] lists. A literal multi-row INSERT works, so the rows
// travel as one jsonb parameter, workbench_coldfront_insert_sql renders
// that literal INSERT on the server, and workbench_coldfront_insert
// EXECUTEs it, where ColdFront's hook turns it into one Iceberg commit.
// The renderer is separate so the tests can assert on the SQL it builds.
//
// Only a view ColdFront has registered in the metrics schema is
// accepted. The column list and types come from the catalog, in
// attribute order, never from the JSON keys. Identifiers are quoted with
// %I and every value with workbench_coldfront_literal, which renders NULL
// or a standard string literal (quotes doubled, backslashes left alone,
// never the E'...' form quote_literal uses for a backslash, which DuckDB
// may not read) that is then cast back to the column's own type. Both
// functions pin standard_conforming_strings, DateStyle, IntervalStyle and
// extra_float_digits, so the rendered text means the same in PostgreSQL
// and DuckDB whatever the session settings:
//
//   - A scalar is rendered as literal(value) || '::<type>'.
//   - An array is rendered as ARRAY[<literal(element)>, ...]::<type>,
//     ARRAY[]::<type> when empty and NULL::<type> when NULL, with the
//     elements taken from the typed array in order. DuckDB cannot parse
//     PostgreSQL's '{...}' array literal, which failed every write of
//     pg_settings, pg_hba_file_rules and pg_node_role live. The metrics
//     arrays are one-dimensional, so a multi-dimensional value, which
//     the ARRAY[] rendering would flatten, is refused.
//
// One expression per column is concatenated, rather than every column
// passed to a single format() call, which keeps a wide table clear of
// the 100-argument limit on function calls. Neither function reads the
// view, which ColdFront refuses inside a function.
const createColdFrontInsertHelperSQL = `
CREATE OR REPLACE FUNCTION public.workbench_coldfront_literal(p_value text)
RETURNS text
LANGUAGE sql
IMMUTABLE
SET search_path = pg_catalog, pg_temp
AS $fn$
    SELECT CASE WHEN p_value IS NULL THEN 'NULL'
                ELSE '''' || replace(p_value, '''', '''''') || '''' END
$fn$;

COMMENT ON FUNCTION public.workbench_coldfront_literal(text) IS
    'Renders a value as NULL or a standard SQL string literal, doubling single quotes only, for the literal INSERT workbench_coldfront_insert runs; backslashes stay literal, as both PostgreSQL with standard_conforming_strings and DuckDB read them.';

CREATE OR REPLACE FUNCTION public.workbench_coldfront_insert_sql(p_table text, p_rows jsonb)
RETURNS text
LANGUAGE plpgsql
STABLE
SECURITY INVOKER
SET search_path = pg_catalog, pg_temp
SET standard_conforming_strings = on
SET DateStyle = 'ISO, YMD'
SET IntervalStyle = 'postgres'
SET extra_float_digits = 1
AS $fn$
DECLARE
    v_rel    text;
    v_cols   text;
    v_exprs  text;
    v_arrays text[];
    v_col    text;
    v_multi  boolean;
    v_vals   text;
BEGIN
    IF p_rows IS NULL OR jsonb_typeof(p_rows) <> 'array' THEN
        RAISE EXCEPTION 'workbench_coldfront_insert: p_rows must be a JSON array';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM coldfront.tiered_views
                   WHERE schema_name = 'metrics' AND relname = p_table) THEN
        RAISE EXCEPTION 'workbench_coldfront_insert: metrics.% is not a ColdFront metrics table', p_table;
    END IF;
    IF jsonb_array_length(p_rows) = 0 THEN
        RETURN NULL;
    END IF;

    v_rel := format('%I.%I', 'metrics', p_table);

    SELECT string_agg(format('%I', a.attname), ', ' ORDER BY a.attnum),
           string_agg(CASE WHEN t.typcategory = 'A' THEN
                          format('CASE WHEN r.%1$I IS NULL THEN %2$L ELSE ''ARRAY['' || '
                                 'COALESCE((SELECT string_agg(public.workbench_coldfront_literal(u.e::text), '', '' ORDER BY u.o) '
                                 'FROM unnest(r.%1$I) WITH ORDINALITY AS u(e, o)), '''') || %3$L END',
                                 a.attname, 'NULL::' || ft.name, ']::' || ft.name)
                      ELSE
                          format('public.workbench_coldfront_literal(r.%I::text) || %L', a.attname, '::' || ft.name)
                      END,
                      ' || '', '' || ' ORDER BY a.attnum),
           array_agg(a.attname::text ORDER BY a.attnum) FILTER (WHERE t.typcategory = 'A')
      INTO v_cols, v_exprs, v_arrays
      FROM pg_attribute a
      JOIN pg_type t ON t.oid = a.atttypid
     CROSS JOIN LATERAL (SELECT format_type(a.atttypid, a.atttypmod) AS name) ft
     WHERE a.attrelid = v_rel::regclass AND a.attnum > 0 AND NOT a.attisdropped;

    FOREACH v_col IN ARRAY coalesce(v_arrays, '{}') LOOP
        EXECUTE format(
            'SELECT bool_or(array_ndims(r.%I) > 1) FROM jsonb_populate_recordset(NULL::%s, $1) AS r',
            v_col, v_rel)
          INTO v_multi
          USING p_rows;
        IF v_multi THEN
            RAISE EXCEPTION 'workbench_coldfront_insert: %.% holds a multi-dimensional array', v_rel, v_col;
        END IF;
    END LOOP;

    EXECUTE format(
        'SELECT string_agg(''('' || %s || '')'', '', '') FROM jsonb_populate_recordset(NULL::%s, $1) AS r',
        v_exprs, v_rel)
      INTO v_vals
      USING p_rows;

    RETURN format('INSERT INTO %s (%s) VALUES %s', v_rel, v_cols, v_vals);
END
$fn$;

COMMENT ON FUNCTION public.workbench_coldfront_insert_sql(text, jsonb) IS
    'Renders the literal multi-row INSERT that workbench_coldfront_insert runs for a JSON array of rows, or NULL for an empty array. Refuses any relation that is not a ColdFront view in the metrics schema.';

CREATE OR REPLACE FUNCTION public.workbench_coldfront_insert(p_table text, p_rows jsonb)
RETURNS void
LANGUAGE plpgsql
SECURITY INVOKER
SET search_path = pg_catalog, pg_temp
SET standard_conforming_strings = on
SET DateStyle = 'ISO, YMD'
SET IntervalStyle = 'postgres'
SET extra_float_digits = 1
AS $fn$
DECLARE
    v_sql text;
BEGIN
    v_sql := public.workbench_coldfront_insert_sql(p_table, p_rows);
    IF v_sql IS NOT NULL THEN
        EXECUTE v_sql;
    END IF;
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
