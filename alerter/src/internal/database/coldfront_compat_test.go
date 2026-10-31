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
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/pkg/anomaly"
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

// bindParam matches a bound parameter reference.
var bindParam = regexp.MustCompile(`\$\d+`)

// coldFrontForbidden lists SQL shapes that DuckDB cannot run, each with
// the reason reported when one appears. Every one was tried live against
// ColdFront with pgx bound parameters.
var coldFrontForbidden = []struct {
	pattern *regexp.Regexp
	reason  string
}{
	{regexp.MustCompile(`(?i)\bANY\s*\(`),
		"ANY(...) over an array fails with an UNKNOWN type in DuckDB, even with a literal or cast array"},
	{regexp.MustCompile(`(?i)\bunnest\s*\(`),
		"unnest of an array parameter fails with an UNKNOWN type in DuckDB"},
	{regexp.MustCompile(`(?i)::\s*[a-z0-9_ ]+\[\]`),
		"array casts fail in DuckDB; bind scalars or one text value instead"},
	{regexp.MustCompile(`(?i)\bpg_lsn\b`),
		"DuckDB has no pg_lsn type; use anomaly.LSNDiffBytesSQL on the text LSN"},
	{regexp.MustCompile(`(?i)\bmake_interval\s*\(`),
		"DuckDB has no make_interval(); use INTERVAL '1 day' * $1::int"},
	{regexp.MustCompile(`(?i)\bhost\s*\(`),
		"DuckDB has no host(); inet columns are text on ColdFront"},
	{regexp.MustCompile(`(?i)::\s*inet\b`),
		"DuckDB has no inet type; inet columns are text on ColdFront"},
	{regexp.MustCompile(`(?i)\binfinity\b`),
		"DuckDB may subtract infinite timestamps in a range join and fail; leave open bounds NULL"},
}

// floatCast matches a cast to double precision directly after a closing
// parenthesis.
var floatCast = regexp.MustCompile(`(?i)^\s*::\s*(float8|float|double precision)\b`)

// avgCall matches the start of an avg() aggregate call.
var avgCall = regexp.MustCompile(`(?i)\bavg\s*\(`)

// coldFrontShapeViolations returns a description of every rule sql
// breaks, or nil when it breaks none.
func coldFrontShapeViolations(sql string) []string {
	var problems []string

	for _, loc := range bindParam.FindAllStringIndex(sql, -1) {
		if !strings.HasPrefix(sql[loc[1]:], "::") {
			problems = append(problems, sql[loc[0]:loc[1]]+
				" has no cast; DuckDB types a bare parameter as UNKNOWN, so write "+
				sql[loc[0]:loc[1]]+"::int (or ::text, ...)")
		}
	}

	for _, f := range coldFrontForbidden {
		if m := f.pattern.FindString(sql); m != "" {
			problems = append(problems, strings.TrimSpace(m)+": "+f.reason)
		}
	}

	// avg() over an integer column is numeric to PostgreSQL but DOUBLE
	// to DuckDB, so pgx fails to scan it; the call must be cast where it
	// stands. EXTRACT() has the same problem, but is legitimately used
	// uncast inside predicates and larger expressions, so its output is
	// policed by result type in TestMetricsSQLResultTypes instead.
	for _, loc := range avgCall.FindAllStringIndex(sql, -1) {
		end := matchingParen(sql, loc[1]-1)
		if end < 0 || !floatCast.MatchString(sql[end+1:]) {
			problems = append(problems,
				"avg(...) must be cast to float directly: avg(x)::float")
		}
	}
	return problems
}

// matchingParen returns the index of the parenthesis closing the one at
// open, or -1 when it is unbalanced.
func matchingParen(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// TestMetricsSQLColdFrontShapes fails when any alerter query that reads
// metrics.* reintroduces a shape DuckDB cannot run on a ColdFront
// datastore: an uncast bound parameter, an array parameter or ANY(),
// an uncast avg(), a PostgreSQL-only type or function, or an infinite
// timestamp literal. GitHub issue #755.
func TestMetricsSQLColdFrontShapes(t *testing.T) {
	stmts := metricsSQLStatements()
	if len(stmts) < len(metricRegistry) {
		t.Fatalf("collected %d statements for %d registry entries",
			len(stmts), len(metricRegistry))
	}
	for _, s := range stmts {
		for _, p := range coldFrontShapeViolations(s.sql) {
			t.Errorf("%s: %s", s.name, p)
		}
	}
}

// TestColdFrontShapeViolations checks the rules themselves against the
// shapes that failed live, and against the replacements that worked, so
// the guard cannot quietly stop matching.
func TestColdFrontShapeViolations(t *testing.T) {
	bad := map[string]string{
		"untyped parameter arithmetic": `SELECT 1 FROM metrics.t WHERE collected_at > NOW() - INTERVAL '1 day' * $1`,
		"untyped parameter comparison": `SELECT 1 FROM metrics.t WHERE connection_id = $2`,
		"ANY over a parameter":         `SELECT 1 FROM metrics.t WHERE connection_id = ANY($1::int[])`,
		"ANY over a literal":           `SELECT 1 FROM metrics.t WHERE connection_id = ANY('{2,3}'::int[])`,
		"unnest":                       `SELECT 1 FROM metrics.t WHERE connection_id IN (SELECT unnest($1::int[]))`,
		"uncast avg":                   `SELECT avg(numbackends) FROM metrics.t`,
		"avg cast too late":            `SELECT avg(numbackends) + 1 FROM metrics.t`,
		"unbalanced avg":               `SELECT avg(numbackends FROM metrics.t`,
		"pg_lsn":                       `SELECT (sent_lsn::pg_lsn - replay_lsn::pg_lsn) FROM metrics.t`,
		"make_interval":                `SELECT 1 FROM metrics.t WHERE collected_at > NOW() - make_interval(days => $1::int)`,
		"host":                         `SELECT host(client_addr) FROM metrics.t`,
		"inet cast":                    `SELECT client_addr::inet FROM metrics.t`,
		"infinite upper bound":         `SELECT COALESCE(LEAD(collected_at) OVER w, 'infinity'::timestamptz) FROM metrics.t`,
		"infinite lower bound":         `SELECT '-infinity'::timestamptz FROM metrics.t`,
	}
	for name, sql := range bad {
		if len(coldFrontShapeViolations(sql)) == 0 {
			t.Errorf("%s: no violation reported for %q", name, sql)
		}
	}

	good := map[string]string{
		"cast parameter arithmetic": `SELECT 1 FROM metrics.t WHERE collected_at > NOW() - INTERVAL '1 day' * $1::int`,
		"cast parameter comparison": `SELECT 1 FROM metrics.t WHERE connection_id = $1::int AND name = $2::text`,
		"integer literal list":      `SELECT 1 FROM metrics.t WHERE connection_id IN (2, 3)`,
		"cast avg":                  `SELECT avg(numbackends)::float, AVG(x) :: float8 FROM metrics.t`,
		"array_agg subscript":       `SELECT (array_agg(x ORDER BY y DESC))[1] FROM metrics.t`,
		"uncast EXTRACT":            `SELECT MAX(EXTRACT(EPOCH FROM (collected_at - xact_start)))::float FROM metrics.t`,
		"NULL-open range join":      `SELECT 1 FROM metrics.t a JOIN r m ON (m.valid_from IS NULL OR a.collected_at >= m.valid_from) AND (m.valid_to IS NULL OR a.collected_at < m.valid_to)`,
	}
	for name, sql := range good {
		if p := coldFrontShapeViolations(sql); len(p) != 0 {
			t.Errorf("%s: unexpected violations %v for %q", name, p, sql)
		}
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
