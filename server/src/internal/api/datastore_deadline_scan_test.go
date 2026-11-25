/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixedDeadlineAllowlist names the context.WithTimeout calls in the
// scanned packages that may keep a literal duration, as
// "<dir>/<file>" mapped to the number of such calls. Each one bounds
// work that is not a datastore request, so Datastore.QueryTimeout must
// not raise it.
var fixedDeadlineAllowlist = map[string]int{
	// NewDatastore's startup ctx, used before the datastore kind is known.
	"database/connection_queries.go": 1,
}

// TestDatastoreDeadlinesUseQueryTimeout keeps datastore request
// deadlines going through Datastore.WithQueryTimeout (issue #891). A
// context.WithTimeout whose duration is a numeric literal, or one of
// the database package's request budgets (ShortQueryTimeout,
// LongQueryTimeout), would bypass the ColdFront and query_timeout
// floor; deadlines for monitored servers, the LLM or the purge use
// named constants of their own and are not affected.
func TestDatastoreDeadlinesUseQueryTimeout(t *testing.T) {
	t.Parallel()

	found := map[string]int{}
	for _, dir := range []string{".", "../database"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("reading %s: %v", dir, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			key := filepath.Base(mustAbs(t, dir)) + "/" + name
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatalf("parsing %s: %v", path, err)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || !isContextWithTimeout(call) || len(call.Args) != 2 {
					return true
				}
				if bypassesQueryTimeout(call.Args[1]) {
					found[key]++
					if found[key] > fixedDeadlineAllowlist[key] {
						t.Errorf("%s: context.WithTimeout with a fixed datastore deadline; "+
							"use Datastore.WithQueryTimeout, or a named constant if the "+
							"work is not a datastore request", fset.Position(call.Pos()))
					}
				}
				return true
			})
		}
	}

	for key, want := range fixedDeadlineAllowlist {
		if found[key] != want {
			t.Errorf("%s: %d allowlisted fixed deadlines found, want %d; update fixedDeadlineAllowlist",
				key, found[key], want)
		}
	}
}

func mustAbs(t *testing.T, dir string) string {
	t.Helper()
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("resolving %s: %v", dir, err)
	}
	return abs
}

func isContextWithTimeout(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "WithTimeout" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "context"
}

// bypassesQueryTimeout reports whether a duration expression contains a
// numeric literal or names ShortQueryTimeout or LongQueryTimeout.
func bypassesQueryTimeout(expr ast.Expr) bool {
	bypass := false
	ast.Inspect(expr, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.BasicLit:
			if v.Kind == token.INT || v.Kind == token.FLOAT {
				bypass = true
			}
		case *ast.Ident:
			if v.Name == "ShortQueryTimeout" || v.Name == "LongQueryTimeout" {
				bypass = true
			}
		}
		return !bypass
	})
	return bypass
}
