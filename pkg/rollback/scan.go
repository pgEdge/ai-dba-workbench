/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package rollback

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
)

// sqlMethods lists the method names whose string arguments Scan
// inspects for a ROLLBACK statement. Rollback itself is handled
// separately because it is matched by arity rather than by SQL text.
var sqlMethods = map[string]bool{
	"Exec":            true,
	"ExecContext":     true,
	"Query":           true,
	"QueryContext":    true,
	"QueryRow":        true,
	"QueryRowContext": true,
	"Prepare":         true,
	"PrepareContext":  true,
}

// Scan walks every non-test Go file under root and returns one
// "path:line" entry, relative to root, for each transaction rollback
// that does not go through this package: a method call named Rollback
// taking exactly one argument (the context-free database/sql form is
// exempt), or a call to one of the SQL-issuing methods whose SQL
// argument, a string literal or a concatenation whose leftmost operand
// is a literal, begins with ROLLBACK case-insensitively. Files in a
// pkg/rollback directory are skipped, as are vendor and node_modules
// trees. Each Go module runs Scan over its own root in a test, so the
// rule is enforced by whichever CI workflow a change triggers.
func Scan(root string) ([]string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	var offenders []string
	fset := token.NewFileSet()

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "vendor" || d.Name() == "node_modules" || isSelf(path) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, pos := range scanFile(file) {
			offenders = append(offenders, rel+":"+strconv.Itoa(fset.Position(pos).Line))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return offenders, nil
}

// isSelf reports whether dir is this package's own directory, which
// legitimately issues the rollbacks the rule forbids elsewhere.
func isSelf(dir string) bool {
	return filepath.Base(dir) == "rollback" &&
		filepath.Base(filepath.Dir(dir)) == "pkg"
}

// scanFile returns the positions of the offending calls in file.
func scanFile(file *ast.File) []token.Pos {
	var offenders []token.Pos
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch {
		case sel.Sel.Name == "Rollback" && len(call.Args) == 1:
			offenders = append(offenders, call.Pos())
		case sqlMethods[sel.Sel.Name]:
			for _, arg := range call.Args {
				if isRollbackSQL(arg) {
					offenders = append(offenders, call.Pos())
					break
				}
			}
		}
		return true
	})
	return offenders
}

// isRollbackSQL reports whether expr is a string literal, or a
// concatenation whose leftmost operand is a string literal, whose text
// begins with the ROLLBACK keyword.
func isRollbackSQL(expr ast.Expr) bool {
	for {
		switch e := expr.(type) {
		case *ast.BinaryExpr:
			if e.Op != token.ADD {
				return false
			}
			expr = e.X
		case *ast.ParenExpr:
			expr = e.X
		case *ast.BasicLit:
			if e.Kind != token.STRING {
				return false
			}
			text, err := strconv.Unquote(e.Value)
			if err != nil {
				return false
			}
			return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(text)), "ROLLBACK")
		default:
			return false
		}
	}
}
