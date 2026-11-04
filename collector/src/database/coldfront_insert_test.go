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
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/collector/src/coldfront"
	"github.com/pgedge/ai-workbench/collector/src/probes"
	"github.com/pgedge/ai-workbench/pkg/rollback"
)

// awkwardText exercises the quoting the insert helper relies on: a
// quote, a statement terminator and a comment, backslashes and
// characters outside ASCII.
const awkwardText = `it's'; DROP TABLE connections; -- \ back\\slash ünïcødé 🚀`

// typedSample is a value of one PostgreSQL column type, as a probe would
// hand it to StoreMetrics, with the text PostgreSQL shows for it once
// stored.
type typedSample struct {
	value any
	text  string
}

// samples gives a value for every column type in the specification.
var samples = map[string]typedSample{
	"integer":          {int32(42), "42"},
	"bigint":           {int64(9007199254740993), "9007199254740993"}, // not a float64
	"real":             {float32(1.5), "1.5"},
	"double precision": {0.1, "0.1"},
	"boolean":          {true, "true"},
	"timestamp with time zone": {
		time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.FixedZone("x", -5*3600)),
		"2026-01-02 08:04:05.123456+00",
	},
	"text":                   {awkwardText, awkwardText},
	"character varying(255)": {awkwardText, awkwardText},
	"text[]":                 {[]string{"a'b", "c,d", `e\f`, "ü"}, `{a'b,"c,d","e\\f",ü}`},
	"jsonb":                  {`{"k": "v'; DROP", "n": 1}`, `{"k": "v'; DROP", "n": 1}`},
	"interval": {
		pgtype.Interval{Months: 1, Days: 2, Microseconds: 3_500_000, Valid: true},
		"1 mon 2 days 00:00:03.5",
	},
	"numeric":  {pgtype.Numeric{Int: bigInt("123456789012345678901"), Valid: true}, "123456789012345678901"},
	"oid":      {uint32(4000000001), "4000000001"},
	"xid":      {uint32(4000000000), "4000000000"},
	"inet":     {netip.MustParsePrefix("10.1.2.3/32"), "10.1.2.3"},
	`"char"`:   {'c', "c"},
	"name":     {"node one", "node one"},
	"smallint": {int16(7), "7"},
}

// bigInt parses a decimal integer for a pgtype.Numeric sample.
func bigInt(s string) *big.Int {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic("bad integer " + s)
	}
	return n
}

// exerciseInsertHelper drives workbench_coldfront.insert_rows, through
// StoreMetrics and directly, against the stub views: every column type
// of every metrics table, a row of NULLs, awkward text, a NaN, and the
// relations the helper must refuse.
func exerciseInsertHelper(t *testing.T, conn *pgxpool.Conn) {
	t.Helper()
	ctx := context.Background()

	probes.SetColdFrontMetrics(true)
	t.Cleanup(func() { probes.SetColdFrontMetrics(false) })
	if _, err := conn.Exec(ctx, "SET TimeZone = 'UTC'"); err != nil {
		t.Fatal(err)
	}

	for _, spec := range coldfront.Tables() {
		columns := make([]string, len(spec.Columns))
		full := make([]any, len(spec.Columns))
		nulls := make([]any, len(spec.Columns))
		for i, c := range spec.Columns {
			s, ok := samples[c.PGType]
			if !ok {
				t.Fatalf("no sample for type %s of metrics.%s.%s", c.PGType, spec.Name, c.Name)
			}
			columns[i] = c.Name
			full[i] = s.value
		}
		if err := probes.StoreMetrics(ctx, conn, spec.Name, columns, [][]any{full, nulls}); err != nil {
			t.Fatalf("StoreMetrics into metrics.%s: %v", spec.Name, err)
		}

		for _, c := range spec.Columns {
			col := pgx.Identifier{c.Name}.Sanitize()
			rel := pgx.Identifier{"metrics", spec.Name}.Sanitize()
			var got *string
			var nullRows int
			if err := conn.QueryRow(ctx,
				"SELECT (SELECT "+col+"::text FROM "+rel+" WHERE connection_id IS NOT NULL LIMIT 1), "+
					"(SELECT count(*) FROM "+rel+" WHERE connection_id IS NULL AND "+col+" IS NULL)").
				Scan(&got, &nullRows); err != nil {
				t.Fatalf("metrics.%s.%s: %v", spec.Name, c.Name, err)
			}
			if want := samples[c.PGType].text; got == nil || *got != want {
				t.Errorf("metrics.%s.%s (%s) stored %v, want %q", spec.Name, c.Name, c.PGType, got, want)
			}
			if nullRows != 1 {
				t.Errorf("metrics.%s.%s: %d NULL rows, want 1", spec.Name, c.Name, nullRows)
			}
		}
	}

	// A NaN cannot travel as JSON and is stored as NULL.
	if err := probes.StoreMetrics(ctx, conn, "pg_connectivity",
		[]string{"connection_id", "collected_at", "response_time_ms"},
		[][]any{{int32(77), time.Now(), math.NaN()}}); err != nil {
		t.Fatalf("StoreMetrics with a NaN: %v", err)
	}
	var nanStored *float64
	if err := conn.QueryRow(ctx,
		"SELECT response_time_ms FROM metrics.pg_connectivity WHERE connection_id = 77").Scan(&nanStored); err != nil {
		t.Fatal(err)
	}
	if nanStored != nil {
		t.Errorf("NaN stored as %v, want NULL", *nanStored)
	}

	// The helper refuses anything but a registered metrics view, before
	// it looks at the relation, so a hostile name is never interpolated.
	for _, table := range []string{"connections", "pg_stat_activity; DROP TABLE connections", "no_such_table"} {
		_, err := conn.Exec(ctx, "SELECT workbench_coldfront.insert_rows($1, $2::jsonb)",
			table, `[{"id": 1}]`)
		if err == nil || !strings.Contains(err.Error(), "is not a ColdFront metrics table") {
			t.Errorf("helper accepted %q: %v", table, err)
		}
	}
	_, err := conn.Exec(ctx, "SELECT workbench_coldfront.insert_rows('pg_connectivity', '{}'::jsonb)")
	if err == nil || !strings.Contains(err.Error(), "must be a JSON array") {
		t.Errorf("helper accepted a non-array: %v", err)
	}
	if _, err := conn.Exec(ctx, "SELECT workbench_coldfront.insert_rows('pg_connectivity', '[]'::jsonb)"); err != nil {
		t.Errorf("helper refused an empty batch: %v", err)
	}

	var connections int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM pg_class WHERE oid = 'public.connections'::regclass").Scan(&connections); err != nil || connections != 1 {
		t.Errorf("connections table missing after the quoting tests: %v", err)
	}

	exerciseArrayRendering(t, conn)
	exerciseRejectedRows(t, conn)
}

// exerciseRejectedRows makes the stub refuse one pg_settings row, as
// DuckDB might refuse a value, and checks that the error the collector
// reports carries no value and that StoreMetrics drops only that row.
func exerciseRejectedRows(t *testing.T, conn *pgxpool.Conn) {
	t.Helper()
	ctx := context.Background()

	if _, err := conn.Exec(ctx, `
        CREATE FUNCTION coldfront_stub.refuse_boom() RETURNS trigger
        LANGUAGE plpgsql AS $f$
        BEGIN
            RAISE EXCEPTION 'value rejected' USING ERRCODE = 'invalid_parameter_value';
        END
        $f$;
        CREATE TRIGGER refuse_boom BEFORE INSERT ON coldfront_stub.pg_settings
            FOR EACH ROW WHEN (NEW.name = 'boom') EXECUTE FUNCTION coldfront_stub.refuse_boom();
    `); err != nil {
		t.Fatal(err)
	}

	const secret = "SECRET-cred-123"
	_, err := conn.Exec(ctx, "SELECT "+coldfront.InsertRowsFunction+"('pg_settings', $1::jsonb)",
		`[{"connection_id": 900, "name": "boom", "setting": "`+secret+`"}]`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("refused row: error = %v, want a PgError", err)
	}
	if pgErr.Code != "22023" || pgErr.Message != "value rejected" {
		t.Errorf("refused row: SQLSTATE %s, message %q", pgErr.Code, pgErr.Message)
	}
	// The error the collector reports, which is all it logs, carries no
	// value. The CONTEXT (Where) does carry the INSERT and its values into
	// the datastore log: catching the error to strip it would need an
	// EXCEPTION block, which pg_duckdb refuses as a subtransaction. That
	// residual exposure is documented rather than tested away.
	if strings.Contains(err.Error(), secret) {
		t.Errorf("refused row: the reported error exposes a value: %q", err.Error())
	}
	if !strings.Contains(pgErr.Where, "INSERT") {
		t.Errorf("refused row: the error context no longer carries the statement (%q); update coldfront-metrics.md if the residual exposure is gone", pgErr.Where)
	}

	columns := []string{"connection_id", "name", "setting", "collected_at"}
	at := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	if err := probes.StoreMetrics(ctx, conn, "pg_settings", columns, [][]any{
		{int32(901), "work_mem", "4MB", at},
		{int32(902), "boom", secret, at},
		{int32(903), "shared_buffers", "128MB", at},
	}); err != nil {
		t.Fatalf("StoreMetrics with one refused row: %v", err)
	}
	var stored []int32
	if err := conn.QueryRow(ctx, `
        SELECT COALESCE(array_agg(connection_id ORDER BY connection_id), '{}')
        FROM metrics.pg_settings WHERE connection_id BETWEEN 900 AND 903
    `).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stored, []int32{901, 903}) {
		t.Errorf("stored connections %v, want [901 903]", stored)
	}
}

// TestColdFrontHelperSchemaOwnership checks that the helper schema is
// created locked down, and that the collector refuses to start when the
// schema, or a function in it, belongs to another role.
func TestColdFrontHelperSchemaOwnership(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	defer pool.Close()
	defer conn.Release()

	role := fmt.Sprintf("wb_intruder_%d", time.Now().UnixNano())
	ident := pgx.Identifier{role}.Sanitize()
	if _, err := conn.Exec(ctx, "DROP SCHEMA IF EXISTS workbench_coldfront CASCADE; CREATE ROLE "+ident); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := pool.Exec(ctx, "DROP SCHEMA IF EXISTS workbench_coldfront CASCADE; DROP OWNED BY "+ident+"; DROP ROLE "+ident); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}()

	// Created by the collector: no other role may use the schema or run
	// its functions.
	if err := EnsureColdFrontInsertHelper(ctx, conn); err != nil {
		t.Fatalf("EnsureColdFrontInsertHelper: %v", err)
	}
	var usage, execute bool
	if err := conn.QueryRow(ctx, `
        SELECT has_schema_privilege($1, 'workbench_coldfront', 'USAGE'),
               has_function_privilege($1, 'workbench_coldfront.insert_rows(text, jsonb)', 'EXECUTE')
    `, role).Scan(&usage, &execute); err != nil {
		t.Fatal(err)
	}
	if usage || execute {
		t.Errorf("another role has USAGE %v on the schema and EXECUTE %v on insert_rows", usage, execute)
	}
	// A second start replaces the functions in place.
	if err := EnsureColdFrontInsertHelper(ctx, conn); err != nil {
		t.Fatalf("second EnsureColdFrontInsertHelper: %v", err)
	}

	// A function another role planted in the schema.
	if _, err := conn.Exec(ctx, `
        GRANT USAGE, CREATE ON SCHEMA workbench_coldfront TO `+ident+`;
        SET ROLE `+ident+`;
        CREATE FUNCTION workbench_coldfront.literal(p_value varchar) RETURNS text
            LANGUAGE sql AS $f$ SELECT 'planted' $f$;
        RESET ROLE;
    `); err != nil {
		t.Fatal(err)
	}
	err := EnsureColdFrontInsertHelper(ctx, conn)
	if err == nil || !strings.Contains(err.Error(), "functions owned by other roles") || !strings.Contains(err.Error(), role) {
		t.Errorf("planted function: error = %v", err)
	}

	// A schema another role created first.
	if _, err := conn.Exec(ctx, "DROP SCHEMA workbench_coldfront CASCADE; CREATE SCHEMA workbench_coldfront AUTHORIZATION "+ident); err != nil {
		t.Fatal(err)
	}
	err = EnsureColdFrontInsertHelper(ctx, conn)
	if err == nil || !strings.Contains(err.Error(), "owned by role "+role) {
		t.Errorf("foreign schema: error = %v", err)
	}
	var functions int
	if err := conn.QueryRow(ctx, `
        SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
        WHERE n.nspname = 'workbench_coldfront'
    `).Scan(&functions); err != nil {
		t.Fatal(err)
	}
	if functions != 0 {
		t.Errorf("%d functions were created in a schema another role owns", functions)
	}
}

// TestColdFrontLiteralIsInlinable checks that literal(), which runs once
// per value, is replaced on upgrade by a form PostgreSQL inlines, and
// that the inlined body stays safe under a hostile search_path.
func TestColdFrontLiteralIsInlinable(t *testing.T) {
	ctx := context.Background()
	pool, conn := getTestConnection(t)
	defer pool.Close()
	defer conn.Release()

	// The helper as an earlier collector created it, with a SET clause.
	if _, err := conn.Exec(ctx, `
        DROP SCHEMA IF EXISTS workbench_coldfront CASCADE;
        DROP SCHEMA IF EXISTS wb_hostile CASCADE;
        CREATE SCHEMA workbench_coldfront;
        CREATE FUNCTION workbench_coldfront.literal(p_value text) RETURNS text
            LANGUAGE sql IMMUTABLE SET search_path = pg_catalog, pg_temp
            AS $f$ SELECT CASE WHEN p_value IS NULL THEN 'NULL'
                               ELSE '''' || replace(p_value, '''', '''''') || '''' END $f$;
    `); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := pool.Exec(ctx, "DROP SCHEMA IF EXISTS workbench_coldfront CASCADE; DROP SCHEMA IF EXISTS wb_hostile CASCADE"); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}()

	if err := EnsureColdFrontInsertHelper(ctx, conn); err != nil {
		t.Fatalf("EnsureColdFrontInsertHelper: %v", err)
	}
	checkLiteralAttributes(t, conn)
	checkLiteralInlined(t, conn)
	checkLiteralUnderHostileSearchPath(t, conn)
}

// checkLiteralAttributes checks that literal() has none of the
// attributes that keep PostgreSQL from inlining it.
func checkLiteralAttributes(t *testing.T, conn *pgxpool.Conn) {
	t.Helper()
	var (
		config  []string
		secDef  bool
		volatil string
	)
	if err := conn.QueryRow(context.Background(), `
        SELECT proconfig, prosecdef, provolatile::text FROM pg_proc
        WHERE oid = 'workbench_coldfront.literal(text)'::regprocedure
    `).Scan(&config, &secDef, &volatil); err != nil {
		t.Fatal(err)
	}
	if len(config) != 0 || secDef || volatil != "i" {
		t.Errorf("literal(): proconfig %v, SECURITY DEFINER %v, volatility %s; want no SET clause, SECURITY INVOKER, IMMUTABLE",
			config, secDef, volatil)
	}
}

// checkLiteralInlined checks that a plan calling literal() computes its
// body's expression instead of the function. It reads from a multi-row
// VALUES list, since constant folding would hide the call either way.
func checkLiteralInlined(t *testing.T, conn *pgxpool.Conn) {
	t.Helper()
	rows, err := conn.Query(context.Background(), `
        EXPLAIN (VERBOSE, COSTS OFF)
        SELECT workbench_coldfront.literal(v.x) FROM (VALUES ('a'::text), (NULL)) AS v(x)
    `)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(plan, "\n"); strings.Contains(joined, "literal(") || !strings.Contains(joined, "replace(") {
		t.Errorf("literal() is not inlined:\n%s", joined)
	}
}

// checkLiteralUnderHostileSearchPath checks that literal(), whose
// inlined body is parsed under the caller's search_path, does not pick
// up a replace() earlier in that path.
func checkLiteralUnderHostileSearchPath(t *testing.T, conn *pgxpool.Conn) {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback.Tx(ctx, tx) //nolint:errcheck // test cleanup
	if _, err := tx.Exec(ctx, `
        CREATE SCHEMA wb_hostile;
        CREATE FUNCTION wb_hostile.replace(text, text, text) RETURNS text
            LANGUAGE sql AS $f$ SELECT 'hijacked' $f$;
        SET LOCAL search_path = wb_hostile, pg_catalog;
    `); err != nil {
		t.Fatal(err)
	}
	var got, gotNull string
	if err := tx.QueryRow(ctx,
		`SELECT workbench_coldfront.literal('it''s'), workbench_coldfront.literal(NULL)`,
	).Scan(&got, &gotNull); err != nil {
		t.Fatal(err)
	}
	if got != `'it''s'` || gotNull != "NULL" {
		t.Errorf("literal() under a hostile search_path = %q, %q; want 'it''s', NULL", got, gotNull)
	}
}

// exerciseArrayRendering checks how the helper renders array and
// backslash values. The stub views are plain PostgreSQL, which also
// accepts the '{...}' array literal and E'...' strings that ColdFront's
// DuckDB rejects, so the rendered SQL itself is asserted, and then the
// stored values are read back.
func exerciseArrayRendering(t *testing.T, conn *pgxpool.Conn) {
	t.Helper()
	ctx := context.Background()

	options := [][]any{
		{int32(501), []string{}},
		{int32(502), []any{nil, "b"}},
		{int32(503), nil},
		{int32(504), []string{"all", "it's, {x}", `back\slash`, "}"}},
	}
	var rows []map[string]any
	for _, o := range options {
		rows = append(rows, map[string]any{"connection_id": o[0], "options": o[1], "address": `C:\dir 'x'`})
	}
	doc, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}

	var rendered string
	if err := conn.QueryRow(ctx, "SELECT workbench_coldfront.insert_rows_sql('pg_hba_file_rules', $1::jsonb)",
		string(doc)).Scan(&rendered); err != nil {
		t.Fatalf("rendering the INSERT: %v", err)
	}
	for _, want := range []string{
		"ARRAY[]::text[]",
		"ARRAY[NULL, 'b']::text[]",
		"ARRAY['all', 'it''s, {x}', 'back\\slash', '}']::text[]",
		"'C:\\dir ''x'''::text",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered INSERT lacks %s:\n%s", want, rendered)
		}
	}
	// options is NULL in one row; the other array columns are NULL in
	// every row.
	if n := strings.Count(rendered, "NULL::text[]"); n != 1+2*len(options) {
		t.Errorf("rendered INSERT has %d NULL arrays, want %d", n, 1+2*len(options))
	}
	if strings.Contains(rendered, "'{") || strings.Contains(rendered, "E'") {
		t.Errorf("rendered INSERT uses an array or escape-string literal:\n%s", rendered)
	}

	columns := []string{"connection_id", "options", "address"}
	values := make([][]any, len(options))
	for i, o := range options {
		values[i] = []any{o[0], o[1], `C:\dir 'x'`}
	}
	if err := probes.StoreMetrics(ctx, conn, "pg_hba_file_rules", columns, values); err != nil {
		t.Fatalf("StoreMetrics with arrays: %v", err)
	}
	want := map[int32]*string{
		501: strPtr("{}"),
		502: strPtr("{NULL,b}"),
		503: nil,
		504: strPtr(`{all,"it's, {x}","back\\slash","}"}`),
	}
	for id, w := range want {
		var got *string
		var address string
		if err := conn.QueryRow(ctx,
			"SELECT options::text, address FROM metrics.pg_hba_file_rules WHERE connection_id = $1", id).
			Scan(&got, &address); err != nil {
			t.Fatalf("connection %d: %v", id, err)
		}
		if (got == nil) != (w == nil) || (got != nil && *got != *w) {
			t.Errorf("connection %d options = %v, want %v", id, got, w)
		}
		if address != `C:\dir 'x'` {
			t.Errorf("connection %d address = %q", id, address)
		}
	}

	// A multi-dimensional array would be flattened by ARRAY[...], so it
	// is refused.
	_, err = conn.Exec(ctx, "SELECT workbench_coldfront.insert_rows('pg_hba_file_rules', $1::jsonb)",
		`[{"connection_id": 505, "options": [["a"], ["b"]]}]`)
	if err == nil || !strings.Contains(err.Error(), "multi-dimensional array") {
		t.Errorf("helper accepted a two-dimensional array: %v", err)
	}
}
