/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package metrics

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/testutil/coldfrontcheck"
)

// statementDatabaseClauseAt is the clause statementDatabaseClause writes
// with its placeholders starting at $first.
func statementDatabaseClauseAt(first string, second string, third string) string {
	return "(dbid::bigint = ANY(string_to_array($" + first +
		", ',')::bigint[]) OR (NOT dbid::bigint = ANY(string_to_array($" +
		second + ", ',')::bigint[]) AND database_name = $" + third + "))"
}

// testStatementDatabase is a filter for alpha, which the name lookup
// resolves from dbid 100, on a server whose lookup also knows dbid 300.
func testStatementDatabase() *StatementDatabaseFilter {
	return &StatementDatabaseFilter{
		Name:          "alpha",
		DBIDs:         []int64{100},
		ResolvedDBIDs: []int64{100, 300},
	}
}

func TestInt64ListArg(t *testing.T) {
	if got := Int64ListArg(nil); got != "" {
		t.Errorf("Int64ListArg(nil) = %q, want empty", got)
	}
	if got := Int64ListArg([]int64{1, 4294967295}); got != "1,4294967295" {
		t.Errorf("Int64ListArg = %q, want %q", got, "1,4294967295")
	}
	if got := BigIntArrayExpr("$7"); got != "string_to_array($7, ',')::bigint[]" {
		t.Errorf("BigIntArrayExpr = %q", got)
	}
}

func TestStatementDatabaseClause(t *testing.T) {
	clause, args := statementDatabaseClause(testStatementDatabase(), 6)
	if want := statementDatabaseClauseAt("6", "7", "8"); clause != want {
		t.Errorf("clause = %q, want %q", clause, want)
	}
	if len(args) != 3 || args[0] != "100" || args[1] != "100,300" || args[2] != "alpha" {
		t.Errorf("args = %#v", args)
	}
	// The clause is spliced into metrics SQL, so it must keep to the
	// shapes a ColdFront datastore can run.
	for _, p := range coldfrontcheck.ShapeProblems(
		"SELECT 1 FROM metrics.pg_stat_statements WHERE " + clause) {
		t.Error(p)
	}
}

func TestBuildMetricsQueryStatementDatabase(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	query, args, err := BuildMetricsQuery(
		"pg_stat_statements",
		[]string{"calls"},
		map[string]string{"calls": "bigint"},
		1, start, end, 60, "sum",
		MetricFilters{
			DatabaseName:      "alpha",
			DatabaseColumn:    "database_name",
			QueryID:           ptrInt64(42),
			StatementDatabase: testStatementDatabase(),
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// database_name is $5 and queryid $6, so the statement database
	// takes $7 to $9.
	if !strings.Contains(query, "queryid = $6") {
		t.Errorf("query should filter by queryid, got:\n%s", query)
	}
	if !strings.Contains(query, statementDatabaseClauseAt("7", "8", "9")) {
		t.Errorf("query should filter by statement database, got:\n%s", query)
	}
	if len(args) != 9 || args[8] != "alpha" {
		t.Fatalf("args = %#v", args)
	}
	// The filters run inside the source that keeps one copy of each
	// reading, so the copies a probing database adds are dropped after
	// them; the result must still be a shape ColdFront can run.
	if !strings.Contains(query, statementReadingsSource) {
		t.Errorf("query should keep one copy of each reading, got:\n%s", query)
	}
	for _, p := range coldfrontcheck.ShapeProblems(query) {
		t.Error(p)
	}

	without, argsWithout, err := BuildMetricsQuery(
		"pg_stat_statements",
		[]string{"calls"},
		map[string]string{"calls": "bigint"},
		1, start, end, 60, "sum",
		MetricFilters{QueryID: ptrInt64(42)},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(without, "dbid::bigint") || len(argsWithout) != 5 {
		t.Errorf("a nil StatementDatabase must add no clause, got:\n%s", without)
	}
}

// TestBuildDerivedMetricsQueryStatementDatabase proves the derived path,
// which appends its own arguments after the filter values, numbers them
// after the statement database's three.
func TestBuildDerivedMetricsQueryStatementDatabase(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	query, args, err := BuildDerivedMetricsQuery(
		"pg_stat_statements",
		[]DerivedMetric{{
			OutputName: "calls_per_sec",
			BaseColumn: "calls",
			Kind:       DerivedPerSec,
		}},
		[]string{"database_name", "queryid", "userid", "dbid", "toplevel"},
		nil, 1, start, start.Add(time.Hour), 60, "avg",
		MetricFilters{
			QueryID:           ptrInt64(42),
			StatementDatabase: testStatementDatabase(),
		}, testMaxElapsed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(query, statementDatabaseClauseAt("6", "7", "8")) {
		t.Errorf("query should filter by statement database, got:\n%s", query)
	}
	if !strings.Contains(query, "$9::float8") {
		t.Errorf("the gap bound should follow the filter as $9, got:\n%s", query)
	}
	if len(args) != 10 {
		t.Fatalf("expected 10 args, got %d: %#v", len(args), args)
	}
	// The LAG runs over one copy of each reading and is not partitioned
	// by the probing database, so a reading taken through a different
	// database from its predecessor still yields a rate.
	if !strings.Contains(query, statementReadingsSource) {
		t.Errorf("query should keep one copy of each reading, got:\n%s", query)
	}
	if want := `PARTITION BY "queryid", "userid", "dbid", "toplevel" ORDER BY`; !strings.Contains(query, want) {
		t.Errorf("query should partition by %q, got:\n%s", want, query)
	}
}

// statementReadingsSource is the start of the source sampleSource
// returns for pg_stat_statements.
const statementReadingsSource = "SELECT DISTINCT ON (queryid, userid, dbid, toplevel, collected_at) *"

// TestSampleSourceOtherProbes proves every other probe reads its table
// directly and keeps all of its entity-key columns.
func TestSampleSourceOtherProbes(t *testing.T) {
	if got := sampleSource("pg_stat_database", "x = 1"); strings.Contains(got, "DISTINCT") ||
		!strings.HasPrefix(got, `metrics."pg_stat_database"`) || !strings.HasSuffix(got, "WHERE x = 1") {
		t.Errorf("sampleSource = %q", got)
	}
	cols := []string{"database_name", "datname"}
	if got := sampleEntityColumns("pg_stat_database", cols); len(got) != 2 {
		t.Errorf("sampleEntityColumns = %v, want %v", got, cols)
	}
	got := sampleEntityColumns("pg_stat_statements", []string{"database_name", "queryid"})
	if len(got) != 1 || got[0] != "queryid" {
		t.Errorf("sampleEntityColumns(pg_stat_statements) = %v, want [queryid]", got)
	}
}

func TestNewStatementDatabaseFilter(t *testing.T) {
	f := NewStatementDatabaseFilter("alpha",
		map[int64]string{300: "gamma", 101: "alpha", 100: "alpha"})
	if f.Name != "alpha" || !slices.Equal(f.DBIDs, []int64{100, 101}) ||
		!slices.Equal(f.ResolvedDBIDs, []int64{100, 101, 300}) {
		t.Errorf("filter = %#v", f)
	}
	if f := NewStatementDatabaseFilter("delta", nil); f.DBIDs == nil || len(f.DBIDs) != 0 {
		t.Errorf("an unknown name must give an empty, non-nil dbid list, got %#v", f)
	}
}

func TestBuildLatestRowsQueryStatementDatabase(t *testing.T) {
	query, args := buildLatestRowsQuery(
		"pg_stat_statements",
		[]string{"database_name", "calls"},
		map[string]string{"database_name": "text", "calls": "bigint"},
		[]int{4},
		MetricFilters{
			QueryID:           ptrInt64(42),
			StatementDatabase: testStatementDatabase(),
		},
		"collected_at", "desc", 10,
	)
	// The connections are $1 and queryid $2, so the statement database
	// takes $3 to $5 and the limit $6.
	if !strings.Contains(query, statementDatabaseClauseAt("3", "4", "5")) {
		t.Errorf("query should filter by statement database, got:\n%s", query)
	}
	if !strings.Contains(query, "LIMIT $6") {
		t.Errorf("the limit should follow the filter as $6, got:\n%s", query)
	}
	if len(args) != 6 || args[4] != "alpha" || args[5] != 10 {
		t.Fatalf("args = %#v", args)
	}
}
