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
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/pkg/anomaly"
	"github.com/pgedge/ai-workbench/pkg/coldfrontsql"
)

// The tests in this file keep every alerter query that reads metrics.*
// runnable on a pgEdge ColdFront datastore as well as on plain
// PostgreSQL. On ColdFront each metrics.* relation is a view over an
// Iceberg table, and pg_duckdb hands any query that reads one to DuckDB,
// which rejects several shapes PostgreSQL accepts. See GitHub issue #755
// and .claude/golang-expert/coldfront-queries.md.

// metricsSQLStatement is one alerter SQL text that reads metrics.*, with
// the arguments it is bound with at run time.
type metricsSQLStatement struct {
	name string
	sql  string
	args []any
}

// metricsSQLStatements returns every alerter query that reads metrics.*:
// each registry entry's latest and historical query, and the readers in
// queries.go. A new reader of metrics.* outside the registry belongs here
// too, which TestMetricsSQLStatementsListEveryReader enforces for this
// package.
func metricsSQLStatements() []metricsSQLStatement {
	var out []metricsSQLStatement
	for _, name := range registryNames() {
		cfg := metricRegistry[name]
		out = append(out, metricsSQLStatement{name + " (latest)", cfg.latestSQL, nil})
		if cfg.historicalSQL != "" {
			out = append(out, metricsSQLStatement{name + " (historical)",
				cfg.historicalSQL, []any{7, noHistoricalUpperBound}})
		}
	}
	return append(out,
		metricsSQLStatement{"connectionsWithExtensionSQL",
			connectionsWithExtensionSQL, []any{"spock"}},
		metricsSQLStatement{"clusterPeersSQL", clusterPeersSQL, []any{1}},
	)
}

// TestMetricsSQLColdFrontShapes fails when any alerter query that reads
// metrics.* reintroduces a shape DuckDB cannot run on a ColdFront
// datastore: an uncast bound parameter, an array parameter or ANY(),
// an uncast avg(), a PostgreSQL-only type or function, or an infinite
// timestamp literal. The rules live in pkg/coldfrontsql, whose own tests
// pin them; pkg/anomaly runs the same check over its historical queries.
// GitHub issue #755.
func TestMetricsSQLColdFrontShapes(t *testing.T) {
	stmts := metricsSQLStatements()
	if len(stmts) < len(metricRegistry) {
		t.Fatalf("collected %d statements for %d registry entries",
			len(stmts), len(metricRegistry))
	}
	for _, s := range stmts {
		for _, p := range coldfrontsql.Violations(s.sql) {
			t.Errorf("%s: %s", s.name, p)
		}
	}
}

// infiniteBind reports whether arg is a pgtype timestamp or date value
// carrying +infinity or -infinity, which pgx would send to the database
// as an infinite bound. A pointer is followed; a nil one is NULL.
func infiniteBind(arg any) bool {
	if v := reflect.ValueOf(arg); v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return false
		}
		arg = v.Elem().Interface()
	}
	switch v := arg.(type) {
	case pgtype.Timestamptz:
		return isInfinite(v.Valid, v.InfinityModifier)
	case pgtype.Timestamp:
		return isInfinite(v.Valid, v.InfinityModifier)
	case pgtype.Date:
		return isInfinite(v.Valid, v.InfinityModifier)
	}
	return false
}

// isInfinite reports whether a non-NULL pgtype value is +/-infinity.
func isInfinite(valid bool, m pgtype.InfinityModifier) bool {
	return valid && m != pgtype.Finite
}

// TestInfiniteBind pins the detector TestMetricsSQLArgsFinite relies on,
// so it cannot quietly stop matching.
func TestInfiniteBind(t *testing.T) {
	inf := pgtype.Timestamptz{InfinityModifier: pgtype.Infinity, Valid: true}
	ts := pgtype.Timestamp{InfinityModifier: pgtype.NegativeInfinity, Valid: true}
	date := pgtype.Date{InfinityModifier: pgtype.Infinity, Valid: true}
	for _, arg := range []any{inf, &inf, ts, &ts, date, &date} {
		if !infiniteBind(arg) {
			t.Errorf("infiniteBind(%#v) = false, want true", arg)
		}
	}
	var nilTS *pgtype.Timestamptz
	var nilPlain *pgtype.Timestamp
	var nilDate *pgtype.Date
	for _, arg := range []any{pgtype.Timestamptz{}, pgtype.Timestamp{},
		pgtype.Date{}, nilTS, nilPlain, nilDate, new(int), 7, "spock", nil,
		pgtype.Timestamptz{InfinityModifier: pgtype.Infinity}} {
		if infiniteBind(arg) {
			t.Errorf("infiniteBind(%#v) = true, want false", arg)
		}
	}
}

// TestMetricsSQLArgsFinite fails when any alerter query that reads
// metrics.* is bound with an infinite timestamp or date. Plain
// PostgreSQL accepts one, so only this test catches it; DuckDB on a
// ColdFront datastore may plan a range join that subtracts the bound and
// fails with "Cannot subtract infinite timestamps" (rule 6 in
// .claude/golang-expert/coldfront-queries.md). GitHub issue #755.
func TestMetricsSQLArgsFinite(t *testing.T) {
	for _, s := range metricsSQLStatements() {
		for i, arg := range s.args {
			if infiniteBind(arg) {
				t.Errorf("%s: $%d is bound to an infinite value %#v; bind a "+
					"finite far-future timestamp instead", s.name, i+1, arg)
			}
		}
	}
}

// TestNoHistoricalUpperBoundIsFinite pins the baseline calculator's
// open upper bound to 9999-12-31 00:00:00 UTC: finite, so DuckDB on a
// ColdFront datastore never subtracts an infinite timestamp, and not
// NULL, under which each historical query's plain
// collected_at <= $2::timestamptz bound would match no row. GitHub
// issues #755 and #776.
func TestNoHistoricalUpperBoundIsFinite(t *testing.T) {
	want := time.Date(9999, time.December, 31, 0, 0, 0, 0, time.UTC)
	if !noHistoricalUpperBound.Valid ||
		noHistoricalUpperBound.InfinityModifier != pgtype.Finite ||
		!noHistoricalUpperBound.Time.Equal(want) {
		t.Errorf("noHistoricalUpperBound = %#v, want finite %s",
			noHistoricalUpperBound, want)
	}
}

// metricsReader matches SQL that reads a metrics.* relation. It is case
// sensitive because the package writes SQL keywords in capitals, and
// prose such as "from metrics.pg_node_role" in a comment is not a query.
var metricsReader = regexp.MustCompile(`\b(FROM|JOIN)\s+metrics\.`)

// TestMetricsSQLStatementsListEveryReader fails when a non-test source
// file in this package reads metrics.* in a SQL literal that
// metricsSQLStatements does not list, so a new reader cannot escape the
// ColdFront guards above. Registry readers are listed wholesale; any
// other file may only hold the named readers.
func TestMetricsSQLStatementsListEveryReader(t *testing.T) {
	listed := map[string]bool{}
	for _, s := range metricsSQLStatements() {
		listed[s.name] = true
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	// Readers per file outside the registry; the count is the number of
	// FROM or JOIN metrics.* clauses its listed constants hold.
	allowed := map[string]int{
		"queries.go": strings.Count(connectionsWithExtensionSQL, "FROM metrics.") +
			strings.Count(clusterPeersSQL, "FROM metrics."),
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") ||
			name == "metric_registry.go" {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		got := len(metricsReader.FindAll(src, -1))
		if got != allowed[name] {
			t.Errorf("%s reads metrics.* %d times, %d of them through statements "+
				"metricsSQLStatements lists; list the new reader there so the "+
				"ColdFront guards cover it", name, got, allowed[name])
		}
	}
	for _, want := range []string{"connectionsWithExtensionSQL", "clusterPeersSQL"} {
		if !listed[want] {
			t.Errorf("metricsSQLStatements does not list %s", want)
		}
	}
}

// TestLSNDiffBytesSQLMatchesPgLSN checks on plain PostgreSQL that the
// portable expression gives exactly what pg_lsn subtraction gives, which
// is what the lag_bytes queries computed before GitHub issue #755.
func TestLSNDiffBytesSQLMatchesPgLSN(t *testing.T) {
	pool := newColdFrontCompatPool(t)

	pairs := [][2]string{
		{"0/0", "0/0"},
		{"0/3000148", "0/3000000"},
		{"16/B374D848", "16/A0000000"},
		{"1/0", "0/FFFFFFFF"},
		{"0/FFFFFFFF", "1/0"},
		{"a/bc", "0/1"},
		{"7FFFFFFF/FFFFFFFF", "7FFFFFFF/0"},
		{"FFFFFFFF/FFFFFFFF", "FFFFFFFF/FFFFFFFE"},
		{"12345678/9ABCDEF0", "12345677/FFFFFFFF"},
	}
	for _, p := range pairs {
		var got, want float64
		//nosemgrep: go_sql_rule-concat-sqli -- fixed SQL fragment over literal column names; the LSNs are bound as $1 and $2
		err := pool.QueryRow(context.Background(),
			`SELECT `+anomaly.LSNDiffBytesSQL("r.sent_lsn", "r.replay_lsn")+`,
			        (r.sent_lsn::pg_lsn - r.replay_lsn::pg_lsn)::float
			   FROM (SELECT $1::text AS sent_lsn, $2::text AS replay_lsn) r`,
			p[0], p[1]).Scan(&got, &want)
		if err != nil {
			t.Fatalf("%s - %s: %v", p[0], p[1], err)
		}
		if got != want {
			t.Errorf("%s - %s = %v, pg_lsn gives %v", p[0], p[1], got, want)
		}
	}
}

// collectorMetricsDDL matches each CREATE TABLE for a metrics.* table in
// the collector's schema source, and collectorMetricsAlter each later
// ADD COLUMN on one.
var (
	collectorMetricsDDL = regexp.MustCompile(
		`(?s)CREATE TABLE IF NOT EXISTS metrics\.\w+ \(.*?\) PARTITION BY RANGE \(collected_at\);`)
	collectorMetricsAlter = regexp.MustCompile(
		`(?s)ALTER TABLE metrics\.\w+\s+ADD COLUMN IF NOT EXISTS [^;]*;`)
)

// newColdFrontCompatPool connects to the integration-test database and
// creates a connections table and every metrics.* table from the
// collector's own DDL, so the alerter's queries plan against the columns
// and types the collector really creates. The schema is dropped when the
// test ends.
func newColdFrontCompatPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	connStr := requireLocalTestDSN(t, "the ColdFront compatibility tests")
	source, err := os.ReadFile(collectorProbeSeedPath)
	if err != nil {
		t.Fatalf("collector schema not readable beside the alerter: %v", err)
	}
	creates := collectorMetricsDDL.FindAllString(string(source), -1)
	if len(creates) == 0 {
		t.Fatalf("%s no longer creates metrics tables in the expected form; "+
			"update collectorMetricsDDL", collectorProbeSeedPath)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}

	teardown := []string{
		`DROP SCHEMA IF EXISTS metrics CASCADE`,
		`DROP TABLE IF EXISTS connections CASCADE`,
	}
	stmts := append(append([]string{}, teardown...),
		`CREATE TABLE connections (
		    id SERIAL PRIMARY KEY,
		    name VARCHAR(255) NOT NULL,
		    cluster_id INTEGER,
		    deleted_at TIMESTAMPTZ
		)`,
		`CREATE SCHEMA metrics`)
	stmts = append(stmts, creates...)
	stmts = append(stmts, collectorMetricsAlter.FindAllString(string(source), -1)...)
	for _, stmt := range stmts {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			pool.Close()
			t.Fatalf("failed to create the collector schema: %v\n%s", err, stmt)
		}
	}

	t.Cleanup(func() {
		for _, stmt := range teardown {
			if _, err := pool.Exec(context.Background(), stmt); err != nil {
				t.Logf("ColdFront compatibility teardown failed: %v", err)
			}
		}
		pool.Close()
	})
	return pool
}

// coldFrontSafeResultTypes are the result column types the alerter's
// metrics queries may return. numeric is deliberately absent: avg() over
// an integer and EXTRACT() are numeric to PostgreSQL, which labels the
// column so, while DuckDB sends DOUBLE bytes under that label, and pgx
// then fails to scan the value ("numeric incomplete"). Every value column
// is scanned into float64 anyway, so a float8 cast loses nothing.
var coldFrontSafeResultTypes = map[uint32]string{
	pgtype.Int4OID:        "integer",
	pgtype.Int8OID:        "bigint",
	pgtype.Float8OID:      "double precision",
	pgtype.TextOID:        "text",
	pgtype.VarcharOID:     "character varying",
	pgtype.TimestamptzOID: "timestamp with time zone",
}

// TestMetricsSQLResultTypes prepares every alerter query that reads
// metrics.* against the collector's schema on plain PostgreSQL, runs it
// with its real arguments, and fails when a result column has a type
// outside coldFrontSafeResultTypes: in particular an uncast avg() or
// EXTRACT() that reaches the select list as numeric. GitHub issue #755.
func TestMetricsSQLResultTypes(t *testing.T) {
	pool := newColdFrontCompatPool(t)
	ctx := context.Background()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer conn.Release()

	for _, s := range metricsSQLStatements() {
		desc, err := conn.Conn().Prepare(ctx, "", s.sql)
		if err != nil {
			t.Errorf("%s: does not plan against the collector schema: %v", s.name, err)
			continue
		}
		for _, f := range desc.Fields {
			if _, ok := coldFrontSafeResultTypes[f.DataTypeOID]; !ok {
				t.Errorf("%s: column %s has type OID %d; cast it to float, "+
					"integer, text or timestamptz", s.name, f.Name, f.DataTypeOID)
			}
		}
		rows, err := conn.Query(ctx, s.sql, s.args...)
		if err != nil {
			t.Errorf("%s: does not run: %v", s.name, err)
			continue
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Errorf("%s: does not run: %v", s.name, err)
		}
	}
}
