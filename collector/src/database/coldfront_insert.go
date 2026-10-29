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
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/pgedge/ai-workbench/collector/src/coldfront"
	"github.com/pgedge/ai-workbench/pkg/rollback"
	"github.com/pgedge/ai-workbench/pkg/sqlmarker"
)

// coldFrontHelperFunctionsTemplate creates the functions the collector
// writes metrics through under ColdFront, in the workbench_coldfront
// schema (coldfront.HelperSchema). @@MARKER@@ is replaced with
// sqlmarker.Comment, which tags the INSERT the helper runs as
// Workbench-internal like every other collector write.
//
// ColdFront rewrites a cold-tier INSERT that has bound parameters into
// format(<template>, $1, $2, ...) passed to duckdb.raw_query, so one
// statement can carry at most 99 parameters, and a probe batch of 100
// rows needs thousands. Reading the rows inside DuckDB does not work
// either: DuckDB has no jsonb_populate_recordset, and cannot unnest
// several VARCHAR[] lists. A literal multi-row INSERT works, so the rows
// travel as one jsonb parameter, insert_rows_sql renders that literal
// INSERT on the server, and insert_rows EXECUTEs it, where ColdFront's
// hook turns it into one Iceberg commit. The renderer is separate so the
// tests can assert on the SQL it builds.
//
// Only a view ColdFront has registered in the metrics schema is
// accepted. The column list and types come from the catalog, in
// attribute order, never from the JSON keys. Identifiers are quoted with
// %I and every value with literal(), which renders NULL or a standard
// string literal (quotes doubled, backslashes left alone) that is then
// cast back to the column's own type. The functions pin
// standard_conforming_strings, DateStyle, IntervalStyle and
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
//
// insert_rows catches a failure of the INSERT and raises it again with
// the same SQLSTATE but without the statement: the CONTEXT of an error
// in an EXECUTE carries the whole statement, and with it every value of
// the batch, some of which (pg_settings, pg_hba_file_rules) can be
// credentials, into the datastore's log.
const coldFrontHelperFunctionsTemplate = `
CREATE OR REPLACE FUNCTION workbench_coldfront.literal(p_value text)
RETURNS text
LANGUAGE sql
IMMUTABLE
SET search_path = pg_catalog, pg_temp
AS $fn$
    SELECT CASE WHEN p_value IS NULL THEN 'NULL'
                ELSE '''' || replace(p_value, '''', '''''') || '''' END
$fn$;

COMMENT ON FUNCTION workbench_coldfront.literal(text) IS
    'Renders a value as NULL or a standard SQL string literal, doubling single quotes only, for the literal INSERT insert_rows runs; backslashes stay literal, as both PostgreSQL with standard_conforming_strings and DuckDB read them.';

CREATE OR REPLACE FUNCTION workbench_coldfront.insert_rows_sql(p_table text, p_rows jsonb)
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
        RAISE EXCEPTION 'workbench_coldfront insert: p_rows must be a JSON array';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM coldfront.tiered_views
                   WHERE schema_name = 'metrics' AND relname = p_table) THEN
        RAISE EXCEPTION 'workbench_coldfront insert: metrics.% is not a ColdFront metrics table', p_table;
    END IF;
    IF jsonb_array_length(p_rows) = 0 THEN
        RETURN NULL;
    END IF;

    v_rel := format('%I.%I', 'metrics', p_table);

    SELECT string_agg(format('%I', a.attname), ', ' ORDER BY a.attnum),
           string_agg(CASE WHEN t.typcategory = 'A' THEN
                          format('CASE WHEN r.%1$I IS NULL THEN %2$L ELSE ''ARRAY['' || '
                                 'COALESCE((SELECT string_agg(workbench_coldfront.literal(u.e::text), '', '' ORDER BY u.o) '
                                 'FROM unnest(r.%1$I) WITH ORDINALITY AS u(e, o)), '''') || %3$L END',
                                 a.attname, 'NULL::' || ft.name, ']::' || ft.name)
                      ELSE
                          format('workbench_coldfront.literal(r.%I::text) || %L', a.attname, '::' || ft.name)
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
            RAISE EXCEPTION 'workbench_coldfront insert: %.% holds a multi-dimensional array', v_rel, v_col;
        END IF;
    END LOOP;

    EXECUTE format(
        'SELECT string_agg(''('' || %s || '')'', '', '') FROM jsonb_populate_recordset(NULL::%s, $1) AS r',
        v_exprs, v_rel)
      INTO v_vals
      USING p_rows;

    RETURN format('INSERT @@MARKER@@ INTO %s (%s) VALUES %s', v_rel, v_cols, v_vals);
END
$fn$;

COMMENT ON FUNCTION workbench_coldfront.insert_rows_sql(text, jsonb) IS
    'Renders the literal multi-row INSERT that insert_rows runs for a JSON array of rows, or NULL for an empty array. Refuses any relation that is not a ColdFront view in the metrics schema.';

CREATE OR REPLACE FUNCTION workbench_coldfront.insert_rows(p_table text, p_rows jsonb)
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
    v_sql := workbench_coldfront.insert_rows_sql(p_table, p_rows);
    IF v_sql IS NULL THEN
        RETURN;
    END IF;
    BEGIN
        EXECUTE v_sql;
    EXCEPTION WHEN OTHERS THEN
        RAISE EXCEPTION 'workbench_coldfront insert into metrics.% failed: %', p_table, left(SQLERRM, 200)
            USING ERRCODE = SQLSTATE;
    END;
END
$fn$;

COMMENT ON FUNCTION workbench_coldfront.insert_rows(text, jsonb) IS
    'Inserts a JSON array of rows into a ColdFront metrics view as one literal multi-row INSERT, because ColdFront caps a cold-tier INSERT at 99 bound parameters. Used only by the collector when use_coldfront_for_metrics is enabled; refuses any relation that is not a ColdFront view in the metrics schema.';

REVOKE ALL ON FUNCTION workbench_coldfront.literal(text),
                       workbench_coldfront.insert_rows_sql(text, jsonb),
                       workbench_coldfront.insert_rows(text, jsonb)
    FROM PUBLIC;
`

// coldFrontHelperFunctionsSQL is the template with the internal-query
// marker in place.
var coldFrontHelperFunctionsSQL = strings.ReplaceAll(
	coldFrontHelperFunctionsTemplate, "@@MARKER@@", sqlmarker.Comment)

// createColdFrontHelperSchemaSQL creates the helper schema if it does
// not exist. An existing schema is checked by coldFrontHelperOwnersQuery
// before anything is created in it.
const createColdFrontHelperSchemaSQL = `CREATE SCHEMA IF NOT EXISTS workbench_coldfront`

// coldFrontHelperOwnersQuery reports, for the helper schema, its owner,
// whether the collector's role owns it, and every function in it that
// another role owns.
const coldFrontHelperOwnersQuery = `
    SELECT n.nspowner::regrole::text,
           n.nspowner = (SELECT oid FROM pg_roles WHERE rolname = current_user),
           COALESCE(array_agg(format('%s(%s) owned by %s', p.proname,
                                     pg_get_function_identity_arguments(p.oid),
                                     p.proowner::regrole))
                    FILTER (WHERE p.oid IS NOT NULL AND p.proowner <> n.nspowner), '{}')
    FROM pg_namespace n
    LEFT JOIN pg_proc p ON p.pronamespace = n.oid
    WHERE n.nspname = $1
    GROUP BY n.nspowner
`

// lockDownColdFrontHelperSchemaSQL keeps every other role out of the
// helper schema and documents it.
const lockDownColdFrontHelperSchemaSQL = `
    REVOKE ALL ON SCHEMA workbench_coldfront FROM PUBLIC;
    COMMENT ON SCHEMA workbench_coldfront IS
        'pgEdge AI DBA Workbench: functions the collector writes ColdFront metrics through. Owned by the collector''s role; no other role may create in it or execute its functions.';
`

// txBeginner is what EnsureColdFrontInsertHelper needs from a datastore
// connection that is not inside a transaction.
type txBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// EnsureColdFrontInsertHelper creates or replaces the functions
// StoreMetrics writes through under ColdFront, in one transaction.
//
// They live in their own schema, workbench_coldfront, rather than in
// public, where any role with CREATE could create them first: a role
// that owned them would keep its ownership through CREATE OR REPLACE and
// could later swap in a body the collector then runs on its next write
// (the functions are SECURITY INVOKER), and a collector that is not a
// superuser could not start at all. Not in metrics, which holds the
// views only, nor in coldfront, which belongs to the extension. The
// schema and every function in it must be owned by the collector's role,
// or the collector refuses to start; PUBLIC is revoked from both.
//
// It runs at every startup rather than in a migration, so the functions
// always match the collector that calls them and the default-mode
// schema gains nothing.
func EnsureColdFrontInsertHelper(ctx context.Context, conn txBeginner) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin creating the %s schema: %w", coldfront.HelperSchema, err)
	}
	defer rollback.Tx(ctx, tx) //nolint:errcheck // no-op after commit

	if _, err := tx.Exec(ctx, createColdFrontHelperSchemaSQL); err != nil {
		return fmt.Errorf("failed to create the %s schema: %w", coldfront.HelperSchema, err)
	}

	var (
		owner   string
		ownedBy bool
		foreign []string
	)
	if err := tx.QueryRow(ctx, coldFrontHelperOwnersQuery, coldfront.HelperSchema).Scan(
		&owner, &ownedBy, &foreign); err != nil {
		return fmt.Errorf("failed to check the owner of the %s schema: %w", coldfront.HelperSchema, err)
	}
	if !ownedBy {
		return fmt.Errorf("the %s schema is owned by role %s, not by the collector's "+
			"datastore role; another role must not control the functions the collector "+
			"writes metrics through, so drop the schema or make the collector's role "+
			"its owner", coldfront.HelperSchema, owner)
	}
	if len(foreign) > 0 {
		return fmt.Errorf("the %s schema holds functions owned by other roles (%s); "+
			"drop them before starting the collector", coldfront.HelperSchema,
			strings.Join(foreign, ", "))
	}

	for _, stmt := range []string{lockDownColdFrontHelperSchemaSQL, coldFrontHelperFunctionsSQL} {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("failed to create the functions in the %s schema: %w",
				coldfront.HelperSchema, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit the %s schema: %w", coldfront.HelperSchema, err)
	}
	return nil
}
