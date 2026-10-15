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
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// credentialTableRef matches SQL that names a metrics table whose rows
// can hold a credential: a setting value, an hba option or error, or
// statement and row text.
var credentialTableRef = regexp.MustCompile(
	`metrics\.(pg_settings|pg_hba_file_rules|pg_stat_activity|pg_stat_statements|` +
		`spock_exception_log|spock_resolutions|pg_stat_replication)\b`)

// probeTableRef matches SQL that names a metrics table chosen at run
// time, as in "FROM metrics.%s" or "metrics." + QuoteIdentifier(name),
// which can be any probe's table, including one holding a credential.
var probeTableRef = regexp.MustCompile(`(?i)(^|\b(from|join)\s+)metrics\.$|metrics\.%(\[[0-9]+\])?s`)

// maskingHelpers are the helpers in this package that mask a value read
// from a credentialTableRef table.
var maskingHelpers = []string{"RedactedText", "MaskedSetting", "MaskedWhole", "MaskedOptions", "MaskedProbeColumn"}

// columnFilterHelpers are the helpers that choose which columns of a
// probe's table may be returned, dropping secret columns and masking
// the probe's credential values.
var columnFilterHelpers = []string{"ReturnableColumns", "MaskedProbeColumn"}

// reviewedCredentialTableReaders are the server functions, as
// "file:Func" or "file:Type.Method" relative to server/src, or the
// package-level constants and variables as "file:name", that hold
// SQL naming a credentialTableRef table but call no masking helper, each
// reviewed as returning no credential.
var reviewedCredentialTableReaders = map[string]string{
	"internal/api/perf_summary_connection_groups.go:connectionGroupsQueryHead": "reads only usename, " +
		"datname, client address and hostname, state and collected_at from pg_stat_activity, and " +
		"returns counts per group",
	"internal/api/perf_summary_handlers.go:statementDatabaseLookupSQL": "reads only collected_at from " +
		"pg_stat_statements and datid and datname from pg_stat_activity",
	"internal/api/perf_summary_handlers.go:queryStatsSQLTemplate": "reads only identity columns, calls " +
		"and total_exec_time from pg_stat_statements, and returns sums",
	"internal/api/server_info_handlers.go:ServerInfoHandler.queryKeySettings": "reads pg_settings for the " +
		"fixed keySettings names only, none of which holds a credential",
	"internal/database/cluster_queries.go:Datastore.GetServersInCluster": "reads only MAX(collected_at) " +
		"from pg_stat_activity",
	"internal/database/estate_queries.go:Datastore.GetConnectionContext": "reads pg_settings for a fixed " +
		"list of memory and planner settings only",
	"internal/database/health.go:criticalRelations": "probes that pg_settings exists with SELECT 1 ... LIMIT 0",
	"internal/database/timeline_queries.go:buildConfigChangeCountQuery": "returns only a count of " +
		"configuration changes",
	"internal/database/timeline_queries.go:buildHBAChangeQuery": "returns only the type, database, " +
		"user_name, address and auth_method of each hba rule, never its options or error",
	"internal/database/timeline_queries.go:buildHBAChangeCountQuery": "returns only a count of hba rule " +
		"changes",
}

// reviewedProbeTableReaders are the server functions that hold SQL
// naming a probe's table chosen at run time but call no column filter
// helper, each reviewed as returning only columns that cannot hold a
// credential.
var reviewedProbeTableReaders = map[string]string{
	"internal/metrics/probe_interval.go:ObserveSampleSpacing": "reads only connection_id and collected_at",
	"internal/metrics/query.go:BuildMetricsQuery": "aggregates only the numeric columns " +
		"GetProbeMetricColumns returns (IsMetricColumn)",
	"internal/metrics/query.go:BuildDerivedMetricsQuery": "returns only numbers derived from numeric " +
		"columns; entity-key columns are used to partition and group, never returned",
	"internal/metrics/query.go:metricQueryParts.lookbackWhere": "reads only MAX(collected_at)",
	"internal/tools/datastore_query_builder.go:fromClause": "names the table only; query_datastore " +
		"returns only the columns its source lists, from which redactMetricColumns drops secret columns and " +
		"masks the rest as ProbeColumnMask says",
}

// sqlUse is a string literal in server source that names a table the
// guard checks, and the function holding it.
type sqlUse struct {
	unit    string // file:Func or file:name, relative to server/src
	pos     string // file:line of the literal
	calls   map[string]bool
	creds   bool // names a credentialTableRef table
	dynamic bool // names a probe table chosen at run time
}

// helperCalls returns the names of the helpers in names that body
// calls, as metrics.Name(...) or, inside this package, Name(...).
func helperCalls(body ast.Node, names []string) map[string]bool {
	calls := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var name string
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			name = fun.Name
		case *ast.SelectorExpr:
			if x, ok := fun.X.(*ast.Ident); ok && x.Name == "metrics" {
				name = fun.Sel.Name
			}
		}
		for _, h := range names {
			if name == h {
				calls[h] = true
			}
		}
		return true
	})
	return calls
}

// funcName returns Func, or Type.Method for a method.
func funcName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	t := fn.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name + "." + fn.Name.Name
	}
	return fn.Name.Name
}

// sqlUses returns every string literal in the server's non-test source
// that names a credential table or a probe table chosen at run time,
// with the masking helpers its function calls. A literal outside any
// function belongs to the package-level constant or variable holding
// it, as file:name.
func sqlUses(t *testing.T, root string) []sqlUse {
	t.Helper()
	all := append(slices.Clone(maskingHelpers), columnFilterHelpers...)
	var uses []sqlUse
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
		check := func(unit string, body ast.Node, calls map[string]bool) {
			ast.Inspect(body, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("%s: unquote %s: %v", rel, lit.Value, err)
				}
				creds, dynamic := credentialTableRef.MatchString(s), probeTableRef.MatchString(s)
				if creds || dynamic {
					p := fset.Position(lit.Pos())
					uses = append(uses, sqlUse{unit: rel + ":" + unit, pos: rel + ":" + strconv.Itoa(p.Line),
						calls: calls, creds: creds, dynamic: dynamic})
				}
				return true
			})
		}
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if decl.Body != nil {
					check(funcName(decl), decl.Body, helperCalls(decl.Body, all))
				}
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					if vs, ok := spec.(*ast.ValueSpec); ok && len(vs.Names) > 0 {
						check(vs.Names[0].Name, vs, helperCalls(vs, all))
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk source tree: %v", err)
	}
	return uses
}

func anyCalled(calls map[string]bool, names []string) bool {
	for _, n := range names {
		if calls[n] {
			return true
		}
	}
	return false
}

// TestCredentialTableReadersAreReviewed fails when a SQL string literal
// in server code names a metrics table that can hold a credential, or a
// probe table chosen at run time, unless the function holding it calls
// a masking helper (for a probe table, ReturnableColumns or
// MaskedProbeColumn) or has been reviewed and listed. Each literal is
// checked on its own, so a function that reads such a table without a
// helper fails even when another function in the same file masks. A
// function that uses a helper must still mask every value it returns;
// review it when the guard does not.
func TestCredentialTableReadersAreReviewed(t *testing.T) {
	uses := sqlUses(t, filepath.Join("..", ".."))
	seenCreds, seenDynamic := map[string]bool{}, map[string]bool{}
	var problems []string
	for _, u := range uses {
		if u.creds {
			seenCreds[u.unit] = true
			if _, ok := reviewedCredentialTableReaders[u.unit]; !ok && !anyCalled(u.calls, maskingHelpers) {
				problems = append(problems, u.pos+" ("+u.unit+") reads a metrics table that can hold a "+
					"credential: mask what it returns with a masking helper, or review it and add it to "+
					"reviewedCredentialTableReaders")
			}
		}
		if u.dynamic {
			seenDynamic[u.unit] = true
			if _, ok := reviewedProbeTableReaders[u.unit]; !ok && !anyCalled(u.calls, columnFilterHelpers) {
				problems = append(problems, u.pos+" ("+u.unit+") reads a probe table chosen at run time: "+
					"choose its columns with ReturnableColumns or MaskedProbeColumn, or review it and add it "+
					"to reviewedProbeTableReaders")
			}
		}
	}
	if len(seenCreds) == 0 || len(seenDynamic) == 0 {
		t.Fatal("found no server source reading a credential or probe table; the walk is broken")
	}
	sort.Strings(problems)
	for _, p := range problems {
		t.Error(p)
	}
	for unit := range reviewedCredentialTableReaders {
		if !seenCreds[unit] {
			t.Errorf("%s is listed in reviewedCredentialTableReaders but no longer reads a "+
				"credential table; remove it", unit)
		}
	}
	for unit := range reviewedProbeTableReaders {
		if !seenDynamic[unit] {
			t.Errorf("%s is listed in reviewedProbeTableReaders but no longer reads a probe table "+
				"chosen at run time; remove it", unit)
		}
	}
}
