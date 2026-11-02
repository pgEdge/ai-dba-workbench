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
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/pkg/anomaly"
)

// The tests in this file keep metrics SQL in the shapes portable_sql.go
// describes, which run on a pgEdge ColdFront datastore as well as on
// plain PostgreSQL (issue #755). Each shape below failed against a live
// ColdFront datastore; see .claude/golang-expert/metrics-queries.md.

// metricsReadRef matches SQL that reads a relation of the metrics schema,
// named or chosen at run time.
var metricsReadRef = regexp.MustCompile(
	`(?i)\b(from|join)\s+(metrics\.|"metrics"\.)|metrics\.%(\[[0-9]+\])?s|(^|\s)metrics\.$`)

// bareHost matches host() over client_addr without the inet cast.
var bareHost = regexp.MustCompile(`(?i)\bhost\s*\(\s*client_addr\s*\)`)

// coldFrontBadShapes are the SQL shapes a ColdFront datastore cannot plan
// or describes with a type PostgreSQL does not expect, each with the
// portable shape to use instead.
var coldFrontBadShapes = []struct {
	re   *regexp.Regexp
	hint string
}{
	{regexp.MustCompile(`(?i)\bANY\s*\(\s*\$(\d+|%d)`),
		"an array parameter cannot be typed; bind the list with IntListArg and expand it with IntArrayExpr"},
	{regexp.MustCompile(`(?i)\bIN\s*\(\s*\$(\d+|%d)(::(int|int4|int8|integer|bigint|smallint))?\s*,`),
		"an IN list of untyped or integer parameters cannot be typed; use IntArrayExpr for integers, or $n::text for each text value"},
	{bareHost,
		"client_addr is text on ColdFront and host() takes an inet; write host(client_addr::inet)"},
	{regexp.MustCompile(`\$(\d+|%d)(::[a-z ]+)?\s*[-+*/]\s`), paramArithmeticHint},
	{regexp.MustCompile(`(?i)[-*/]\s*\$(\d+|%d)\b`), paramArithmeticHint},
	{regexp.MustCompile(`(?i)\bpg_lsn\b`),
		"DuckDB has no pg_lsn type; work in bigint byte positions or compute the difference in Go"},
	{regexp.MustCompile(`(?i)'[-+]?infinity'`),
		"DuckDB may subtract an infinite bound and fail; to leave a bound open, bind NULL " +
			"and write col <= COALESCE($n::timestamptz, col)"},
	{regexp.MustCompile(`(?i)\$(\d+|%d)(::[a-z_ ]+?)?\s+IS\s+(NOT\s+)?NULL\b`),
		"a NULL test on a parameter cannot be planned, cast or not; write " +
			"col <= COALESCE($n::timestamptz, col) or test a column instead"},
}

// paramArithmeticHint is the advice for arithmetic on a parameter.
const paramArithmeticHint = "arithmetic on a parameter cannot be planned; compute the value " +
	"in Go and bind it, or multiply an interval by an integer-typed parameter " +
	"(INTERVAL '1 day' * $n::int)"

// intervalTimesIntParam matches the one parameter arithmetic a ColdFront
// datastore plans: an interval literal multiplied by a parameter cast to
// an integer type, in either order. Every other parameter arithmetic
// fails, a float8-cast multiplier and a cast parameter plus or minus
// anything included.
var intervalTimesIntParam = regexp.MustCompile(
	`(?i)INTERVAL\s+'[^']*'\s*\*\s*\$(\d+|%d)::(int|integer|int4|int8|bigint)\b|` +
		`\$(\d+|%d)::(int|integer|int4|int8|bigint)\s*\*\s*INTERVAL\s+'[^']*'`)

// arrayLiteral matches a '{...}' literal and the cast that follows it,
// if any. DuckDB cannot cast such a literal's text to an array, though a
// JSON object literal cast to json or jsonb is fine.
var arrayLiteral = regexp.MustCompile(`(?i)'\{[^']*\}'(\s*::\s*[a-z_]+)?`)

// qualifiedTable matches a schema-qualified name after FROM or JOIN,
// capturing the word before the keyword and the schema.
var qualifiedTable = regexp.MustCompile(`(?i)(?:(\w+)\s+)?\b(?:from|join)\s+"?([a-z_][a-z0-9_]*)"?\s*\.\s*["a-z_%]`)

// notARelation are the words before a FROM that is not a relation's:
// IS DISTINCT FROM and EXTRACT(field FROM ...) take a column.
var notARelation = map[string]bool{"distinct": true, "epoch": true, "year": true, "month": true,
	"day": true, "hour": true, "minute": true, "second": true}

// untypedJSONValue matches a string literal passed as a JSON builder's
// value: a key literal, a comma and a second literal with no cast.
var untypedJSONValue = regexp.MustCompile(`'[a-z_]+'\s*,\s*'[^']*'(\s*[,)\n]|\s*$)`)

// reviewedQualifiedTableUnits are the units that hold a metrics read and
// a schema-qualified ordinary table in different statements, never in
// one, each reviewed.
var reviewedQualifiedTableUnits = map[string]string{
	"internal/tools/datastore_query_builder.go:fromClause": "returns metrics.<probe> for a metric " +
		"source and public.<table> for a configuration source, never both in one statement",
}

// uncastCalls are the calls whose PostgreSQL type (numeric) differs from
// DuckDB's (DOUBLE), so each must be followed by a cast that changes it.
var uncastCalls = regexp.MustCompile(`(?i)\b(avg|extract)\s*\(`)

// castAfter matches a type-changing cast at the start of the text that
// follows a call.
var castAfter = regexp.MustCompile(`(?i)^\s*::\s*(float8|double precision|bigint)\b`)

// coldFrontShapeProblems returns a description of every non-portable
// shape in sql, which reads the metrics schema.
func coldFrontShapeProblems(sql string) []string {
	var problems []string
	masked := intervalTimesIntParam.ReplaceAllString(sql, "interval_multiple")
	for _, bad := range coldFrontBadShapes {
		if m := bad.re.FindString(masked); m != "" {
			problems = append(problems, strconv.Quote(m)+": "+bad.hint)
		}
	}
	for _, m := range arrayLiteral.FindAllStringSubmatch(sql, -1) {
		cast := strings.ToLower(strings.TrimLeft(m[1], " \t\n:"))
		if cast != "json" && cast != "jsonb" {
			problems = append(problems, strconv.Quote(m[0])+": DuckDB cannot cast a '{...}' "+
				"literal to an array; write ARRAY[...], or bind integers with IntListArg and IntArrayExpr")
		}
	}
	for _, m := range qualifiedTable.FindAllStringSubmatch(sql, -1) {
		if !strings.EqualFold(m[2], "metrics") && !notARelation[strings.ToLower(m[1])] {
			problems = append(problems, strconv.Quote(strings.TrimSpace(m[0]))+": ColdFront plans the "+
				"whole statement in DuckDB, which cannot resolve a schema-qualified PostgreSQL table; "+
				"name it unqualified, as the other metrics queries do")
		}
	}
	if strings.Contains(sql, "_build_object") {
		if m := untypedJSONValue.FindString(sql); m != "" {
			problems = append(problems, strconv.Quote(strings.TrimSpace(m))+": DuckDB cannot type a "+
				"string literal as a JSON builder's value; cast it to text")
		}
	}
	for _, loc := range uncastCalls.FindAllStringIndex(sql, -1) {
		end := matchingParen(sql, loc[1]-1)
		if end < 0 {
			// The call is closed in another fragment; nothing to check.
			continue
		}
		call := sql[loc[0] : end+1]
		if !castAfter.MatchString(sql[end+1:]) {
			problems = append(problems, strconv.Quote(call)+": numeric to PostgreSQL but DOUBLE on "+
				"ColdFront; cast it to float8 (EpochSeconds, AggregateExpr)")
		}
	}
	return problems
}

// runtimeINList matches an IN list whose items are formatted at run time.
var runtimeINList = regexp.MustCompile(`(?i)\bIN\s*\(\s*%s\s*\)`)

// typedTextPlaceholder matches a placeholder formatted with a text cast.
var typedTextPlaceholder = regexp.MustCompile(`^\$%d::text$`)

// unitShapeProblems returns the problems of a unit's SQL that need more
// than one literal to judge: an IN list formatted at run time must be
// built from $n::text placeholders, the only list items a ColdFront
// datastore can type.
func unitShapeProblems(literals []string) []string {
	inList, typed := "", false
	for _, lit := range literals {
		if m := runtimeINList.FindString(lit); m != "" {
			inList = m
		}
		typed = typed || typedTextPlaceholder.MatchString(lit)
	}
	if inList != "" && !typed {
		return []string{strconv.Quote(inList) + ": build the list from \"$%d::text\" placeholders, " +
			"or bind integers with IntListArg and IntArrayExpr"}
	}
	return nil
}

// matchingParen returns the index of the parenthesis closing the one at
// open, or -1 when sql ends first.
func matchingParen(sql string, open int) int {
	depth := 0
	for i := open; i < len(sql); i++ {
		switch sql[i] {
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

func TestColdFrontShapeProblems(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		bad  bool
	}{
		{"array parameter", "WHERE connection_id = ANY($1)", true},
		{"cast array parameter", "WHERE connection_id = ANY($2::int[])", true},
		{"formatted array parameter", "WHERE connection_id = ANY($%d)", true},
		{"text list", "WHERE connection_id = ANY(" + IntArrayExpr("$1") + ")", false},
		{"untyped IN list", "WHERE name IN ($2, $3)", true},
		{"integer IN list", "WHERE connection_id IN ($1::int, $2::int)", true},
		{"text IN list", "WHERE name IN ($2::text, $3::text)", false},
		{"single typed IN", "WHERE connection_id IN ($1::integer)", false},
		{"subquery IN", "WHERE dbid IN (SELECT datid FROM db_names)", false},
		{"bare host", "COALESCE(host(client_addr), 'local')", true},
		{"inet host", "COALESCE(host(client_addr::inet), 'local')", false},
		{"parameter minus interval", "collected_at >= $3 - INTERVAL '1 hour'", true},
		{"interval times parameter", "INTERVAL '1 day' * $1", true},
		{"interval times int parameter", "NOW() - INTERVAL '1 day' * $1::int", false},
		{"bigint parameter times interval", "NOW() - $1::bigint * INTERVAL '1 day'", false},
		{"interval times float parameter", "INTERVAL '1 day' * $1::float8", true},
		{"int parameter minus one", "connection_id > $2::int - 1", true},
		{"cast timestamp minus interval", "collected_at > $2::timestamptz - INTERVAL '1 hour'", true},
		{"pg_lsn", "(sent_lsn::pg_lsn - replay_lsn::pg_lsn)::float", true},
		{"infinity literal", "collected_at <= 'infinity'::timestamptz", true},
		{"minus infinity literal", "THEN '-infinity'::timestamptz", true},
		{"array literal", "connection_id = ANY('{1,2}'::int[])", true},
		{"untyped array literal", "state = ANY('{active,idle}')", true},
		{"array constructor", "connection_id = ANY(ARRAY[1, 2])", false},
		{"parameter is null", "($2 IS NULL OR collected_at <= $2)", true},
		{"cast parameter is not null", "$2::timestamptz IS NOT NULL", true},
		{"formatted parameter is null", "$%d::text IS NULL", true},
		{"column is null", "m.valid_from IS NULL OR a.collected_at >= m.valid_from", false},
		{"coalesced open bound", "collected_at <= COALESCE($2::timestamptz, collected_at)", false},
		{"not distinct from parameter", "database_name IS NOT DISTINCT FROM $4::text", false},
		{"jsonb object literal", "COALESCE(details, '{}'::jsonb)", false},
		{"json object literal", `'{"a": 1}'::json ->> 'a'`, false},
		{"now minus parameter", "collected_at > now() - $2::interval", true},
		{"column plus parameter", "bucket_time + $1::interval", false},
		{"comparison", "collected_at >= $3 AND collected_at <= $4", false},
		{"uncast avg", "SELECT avg(xact_commit) FROM metrics.x", true},
		{"cast avg", "SELECT avg(xact_commit)::float8 FROM metrics.x", false},
		{"uncast epoch", "MIN(EXTRACT(EPOCH FROM a - b)) AS s", true},
		{"float8 epoch", "MIN(EXTRACT(EPOCH FROM a - b)::float8) AS s", false},
		{"bigint epoch", "GREATEST(EXTRACT(EPOCH FROM (a - b))::BIGINT, 0)", false},
		{"open call", "SELECT avg(", false},
		{"qualified table", "FROM public.connections c LEFT JOIN metrics.pg_connectivity l", true},
		{"qualified join", `JOIN "public"."alerts" a ON true`, true},
		{"unqualified table", "FROM connections c LEFT JOIN metrics.pg_connectivity l", false},
		{"metrics table", `FROM metrics."pg_stat_database" t`, false},
		{"distinct from a column", "x IS DISTINCT FROM changes.prev_setting", false},
		{"extract from a column", "EXTRACT(EPOCH FROM p1.collected_at - p2.collected_at)::float8", false},
		{"untyped JSON value", "jsonb_build_object('name', n, 'change_type', 'modified'\n)", true},
		{"typed JSON value", "jsonb_build_object('name', n, 'change_type', 'modified'::text)", false},
		{"JSON keys only", "jsonb_build_object('name', n, 'value', v)", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := coldFrontShapeProblems(tt.sql)
			if (len(got) > 0) != tt.bad {
				t.Errorf("coldFrontShapeProblems(%q) = %q, want bad = %v", tt.sql, got, tt.bad)
			}
		})
	}
	listTests := []struct {
		name     string
		literals []string
		bad      bool
	}{
		{"untyped run-time list", []string{"$%d", "name IN (%s)"}, true},
		{"typed run-time list", []string{"$%d::text", "name NOT IN (%s)"}, false},
		{"no list", []string{"name = $1"}, false},
	}
	for _, tt := range listTests {
		if got := unitShapeProblems(tt.literals); (len(got) > 0) != tt.bad {
			t.Errorf("%s: unitShapeProblems = %q, want bad = %v", tt.name, got, tt.bad)
		}
	}
	if matchingParen("((x)", 0) != -1 {
		t.Error("matchingParen should report an unclosed parenthesis")
	}
}

// metricsSQLUnit is the SQL in one function, or one package-level
// constant or variable, of the server source.
type metricsSQLUnit struct {
	unit     string
	literals []string
	lines    []string
	// reads is true when one of the literals reads the metrics schema.
	reads bool
}

// metricsSQLUnits returns every unit of the server's non-test source that
// holds a string literal, with all of its string literals, since a
// statement is often assembled from fragments in the function that runs
// it, and whether one of them reads the metrics schema.
func metricsSQLUnits(t *testing.T, root string) []metricsSQLUnit {
	t.Helper()
	var units []metricsSQLUnit
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "vendor" || d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		collect := func(unit string, node ast.Node) {
			u := metricsSQLUnit{unit: rel + ":" + unit}
			ast.Inspect(node, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("%s: unquote %s: %v", rel, lit.Value, err)
				}
				u.reads = u.reads || metricsReadRef.MatchString(s)
				u.literals = append(u.literals, s)
				u.lines = append(u.lines, rel+":"+strconv.Itoa(fset.Position(lit.Pos()).Line))
				return true
			})
			if len(u.literals) > 0 {
				units = append(units, u)
			}
		}
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if decl.Body != nil {
					collect(funcName(decl), decl.Body)
				}
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					if vs, ok := spec.(*ast.ValueSpec); ok && len(vs.Names) > 0 {
						collect(vs.Names[0].Name, vs)
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk source tree: %v", err)
	}
	return units
}

// TestComparisonSeriesQueryAvoidsColdFrontShapes runs the shape check
// over the anomaly comparison's statement for every baselined metric.
// TestMetricsSQLAvoidsColdFrontShapes cannot see it: comparisonSeriesQuery
// names no metrics relation itself, because the registry's historical
// query supplies the reads. Only the wrapper this package adds is checked;
// the historical query is pkg/anomaly's and is guarded there, so it is
// replaced by a stand-in before the check (issue #755).
func TestComparisonSeriesQueryAvoidsColdFrontShapes(t *testing.T) {
	defs := anomaly.BaselineMetrics()
	if len(defs) == 0 {
		t.Fatal("no baselined metrics")
	}
	for _, def := range defs {
		sql := comparisonSeriesQuery(def)
		if def.HistoricalSQL == "" || strings.Count(sql, def.HistoricalSQL) != 1 {
			t.Errorf("%s: the wrapper does not hold the historical query exactly once", def.Name)
			continue
		}
		wrapper := strings.Replace(sql, def.HistoricalSQL, "SELECT * FROM metrics.historical", 1)
		for _, p := range coldFrontShapeProblems(wrapper) {
			t.Errorf("%s: %s", def.Name, p)
		}
		for _, m := range wrapperParam.FindAllStringSubmatch(wrapper, -1) {
			if m[1] == "" {
				t.Errorf("%s: wrapper parameter %s is untyped; cast it", def.Name, m[0])
			}
		}
	}
}

// wrapperParam matches a placeholder and the cast that follows it, if
// any.
var wrapperParam = regexp.MustCompile(`\$\d+(::)?`)

// TestMetricsSQLAvoidsColdFrontShapes fails when a server function, or a
// package-level constant or variable, that reads the metrics schema
// holds SQL in a shape a pgEdge ColdFront datastore cannot run or
// describes with a type PostgreSQL does not expect (issue #755): an
// array or untyped IN-list parameter, arithmetic on a parameter, an avg
// or EXTRACT left numeric, or host() over the text client_addr. It
// cannot see a column's type, so an integer sum and an integer division
// are left to review; AggregateExpr and the rules in portable_sql.go
// cover those.
func TestMetricsSQLAvoidsColdFrontShapes(t *testing.T) {
	units := metricsSQLUnits(t, filepath.Join("..", ".."))
	readers := 0
	for _, u := range units {
		if u.reads {
			readers++
		}
	}
	if readers < 20 {
		t.Fatalf("found only %d units reading the metrics schema; the walk is broken", readers)
	}
	var problems []string
	for _, u := range units {
		if !u.reads {
			// client_addr is a column of the metrics schema alone, so a
			// fragment holding host(client_addr) is metrics SQL wherever
			// it is declared, even in a constant joined to the statement
			// elsewhere.
			for i, lit := range u.literals {
				if m := bareHost.FindString(lit); m != "" {
					problems = append(problems, u.lines[i]+" ("+u.unit+"): "+strconv.Quote(m)+
						": write host(client_addr::inet)")
				}
			}
			continue
		}
		for i, lit := range u.literals {
			for _, p := range coldFrontShapeProblems(lit) {
				if _, ok := reviewedQualifiedTableUnits[u.unit]; ok && strings.Contains(p, "schema-qualified") {
					continue
				}
				problems = append(problems, u.lines[i]+" ("+u.unit+"): "+p)
			}
		}
		for _, p := range unitShapeProblems(u.literals) {
			problems = append(problems, u.unit+": "+p)
		}
	}
	sort.Strings(problems)
	for _, p := range problems {
		t.Error(p)
	}
}
