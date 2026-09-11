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
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// rollbackScanRoots lists the module source trees, relative to the
// repository root, that the convention check covers.
var rollbackScanRoots = []string{
	filepath.Join("server", "src"),
	filepath.Join("collector", "src"),
	filepath.Join("alerter", "src"),
}

// findRepoRoot walks up from the working directory looking for the
// repository root, identified by the top-level Makefile sitting next to
// the sub-project directories.
func findRepoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "Makefile")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "collector")); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// isBackgroundCall reports whether the expression is a call to
// context.Background().
func isBackgroundCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Background" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "context"
}

// TestRollbackCallsUseNonCancelableContext enforces the convention
// settled in issue #381 across every Go module in the tree: a
// transaction rollback must never be handed a cancelable context,
// because pgx v5 fails the rollback outright once that context is
// canceled and then discards the pooled connection while its
// transaction is still open (jackc/pgx#2470).
//
// The rationale lives in .claude/golang-expert/transaction-rollback.md
// and in the contributor guide; this test is the automated half of the
// same rule, so a new transaction cannot quietly reintroduce the bug.
func TestRollbackCallsUseNonCancelableContext(t *testing.T) {
	root := findRepoRoot(t)
	if root == "" {
		t.Skip("repository root not found; skipping rollback convention scan")
	}

	var offenders []string
	fset := token.NewFileSet()

	for _, rel := range rollbackScanRoots {
		dir := filepath.Join(root, rel)
		if _, err := os.Stat(dir); err != nil {
			continue
		}

		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "vendor" || d.Name() == "node_modules" {
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

			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Rollback" || len(call.Args) != 1 {
					return true
				}
				if isBackgroundCall(call.Args[0]) {
					return true
				}
				loc := fset.Position(call.Pos())
				relPath, relErr := filepath.Rel(root, loc.Filename)
				if relErr != nil {
					relPath = loc.Filename
				}
				offenders = append(offenders,
					relPath+":"+strconv.Itoa(loc.Line))
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("scanning %s: %v", dir, err)
		}
	}

	if len(offenders) > 0 {
		t.Errorf("rollback calls must pass a non-cancelable context "+
			"(context.Background()); offending sites:\n  %s",
			strings.Join(offenders, "\n  "))
	}
}
