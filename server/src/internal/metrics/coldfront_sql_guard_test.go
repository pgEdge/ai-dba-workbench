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
	"github.com/pgedge/ai-workbench/server/internal/testutil/coldfrontcheck"
)

// The tests in this file keep metrics SQL in the shapes portable_sql.go
// describes, which run on a pgEdge ColdFront datastore as well as on
// plain PostgreSQL (issue #755). Each shape below failed against a live
// ColdFront datastore; see .claude/golang-expert/metrics-queries.md.

// metricsReadRef matches SQL that reads a relation of the metrics schema,
// named or chosen at run time.
var metricsReadRef = regexp.MustCompile(
	`(?i)\b(from|join)\s+(metrics\.|"metrics"\.)|metrics\.%(\[[0-9]+\])?s|(^|\s)metrics\.$`)

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
		for _, p := range coldfrontcheck.ShapeProblems(wrapper) {
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
				if m := coldfrontcheck.BareHost.FindString(lit); m != "" {
					problems = append(problems, u.lines[i]+" ("+u.unit+"): "+strconv.Quote(m)+
						": write host(client_addr::inet)")
				}
			}
			continue
		}
		for i, lit := range u.literals {
			for _, p := range coldfrontcheck.ShapeProblems(lit) {
				problems = append(problems, u.lines[i]+" ("+u.unit+"): "+p)
			}
		}
		for _, p := range coldfrontcheck.UnitProblems(u.literals) {
			problems = append(problems, u.unit+": "+p)
		}
	}
	sort.Strings(problems)
	for _, p := range problems {
		t.Error(p)
	}
}
