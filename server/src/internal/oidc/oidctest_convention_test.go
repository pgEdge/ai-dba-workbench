/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package oidc

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// oidctestImportPath is the package that must never be imported from
// production code.
const oidctestImportPath = "github.com/pgedge/ai-workbench/server/internal/oidc/oidctest"

// TestOIDCTestPackageIsNotImportedByProductionCode turns the doc comment
// on package oidctest into a build-time failure, in the style of the
// pkg/rollback convention test in internal/database. The fake identity
// provider is a normal (non-test) package so that both internal/oidc and
// internal/api can use it, which means nothing but this test stops it
// being linked into the server binary: it imports testing, registers
// cleanup on a *testing.T, and mints ID tokens with an in-process key.
//
// The module root is found relative to this file, so the check needs no
// knowledge of the repository layout and runs in the module's own CI
// workflow.
func TestOIDCTestPackageIsNotImportedByProductionCode(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving the module root: %v", err)
	}

	// anchorFile must be seen by the walk. Without it, a test that fails
	// only on a non-empty offender list would stay green whilst
	// enforcing nothing, should the module root ever stop resolving.
	const anchorFile = "internal/oidc/provider.go"

	var offenders []string
	sawAnchor := false
	fset := token.NewFileSet()

	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "vendor" || entry.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}

		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if filepath.ToSlash(relative) == anchorFile {
			sawAnchor = true
		}
		for _, imported := range file.Imports {
			value, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				continue
			}
			if value == oidctestImportPath {
				offenders = append(offenders,
					relative+":"+strconv.Itoa(fset.Position(imported.Pos()).Line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scanning the module: %v", err)
	}

	if !sawAnchor {
		t.Fatalf("the walk never reached %s, so this test enforced nothing; "+
			"check that the module root still resolves from this file", anchorFile)
	}

	if len(offenders) > 0 {
		t.Errorf("%s may only be imported from _test.go files; offending sites:\n  %s",
			oidctestImportPath, strings.Join(offenders, "\n  "))
	}
}
