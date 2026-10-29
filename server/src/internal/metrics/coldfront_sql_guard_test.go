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
	{regexp.MustCompile(`\$(\d+|%d)(::[a-z ]+)?\s*[-+*/]\s`),
		"arithmetic on a parameter cannot be planned; compute the value in Go and bind it"},
	{regexp.MustCompile(`(?i)[-*/]\s*\$(\d+|%d)\b`),
		"arithmetic on a parameter cannot be planned; compute the value in Go and bind it"},
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
	for _, bad := range coldFrontBadShapes {
		if m := bad.re.FindString(sql); m != "" {
			problems = append(problems, strconv.Quote(m)+": "+bad.hint)
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
		{"now minus parameter", "collected_at > now() - $2::interval", true},
		{"column plus parameter", "bucket_time + $1::interval", false},
		{"comparison", "collected_at >= $3 AND collected_at <= $4", false},
		{"uncast avg", "SELECT avg(xact_commit) FROM metrics.x", true},
		{"cast avg", "SELECT avg(xact_commit)::float8 FROM metrics.x", false},
		{"uncast epoch", "MIN(EXTRACT(EPOCH FROM a - b)) AS s", true},
		{"float8 epoch", "MIN(EXTRACT(EPOCH FROM a - b)::float8) AS s", false},
		{"bigint epoch", "GREATEST(EXTRACT(EPOCH FROM (a - b))::BIGINT, 0)", false},
		{"open call", "SELECT avg(", false},
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
