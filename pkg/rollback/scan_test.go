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
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeTree writes each file in files (path relative to root) and
// returns root.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, src := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const offendingSource = `package x

import "context"

func f(ctx context.Context, tx interface {
	Rollback(context.Context) error
	Exec(context.Context, string, ...any) (int, error)
}, name string) {
	defer tx.Rollback(context.Background())      // line 9
	_ = tx.Rollback(context.WithoutCancel(ctx))  // line 10
	_, _ = tx.Exec(ctx, "rollback to savepoint "+name) // line 11
	_, _ = tx.Exec(ctx, ("ROLLBACK TO SAVEPOINT " + name)) // line 12
	_, _ = tx.Exec(ctx, "  Rollback") // line 13
	_, _ = tx.Exec(ctx, "SAVEPOINT "+name)
	_, _ = tx.Exec(ctx, name+"ROLLBACK")
	_, _ = tx.Exec(ctx, "x"+"ROLLBACK")
	_, _ = tx.Exec(ctx, "a"-"ROLLBACK")
	_, _ = tx.Exec(ctx, 42)
	_, _ = tx.Exec(ctx, name)
}
`

const cleanSource = `package y

import (
	"context"
	"database/sql"

	"github.com/pgedge/ai-workbench/pkg/rollback"
)

func g(ctx context.Context, tx interface{ Rollback(context.Context) error }, sqltx *sql.Tx) {
	defer rollback.Tx(ctx, tx)
	_ = sqltx.Rollback()
	_ = rollback.ToSavepoint(ctx, nil, "sp")
	Exec("ROLLBACK") // a plain function call, not a method
}

func Exec(string) {}
`

func TestScanFlagsOffenders(t *testing.T) {
	root := writeTree(t, map[string]string{
		filepath.Join("a", "bad.go"):                    offendingSource,
		filepath.Join("b", "good.go"):                   cleanSource,
		filepath.Join("a", "bad_test.go"):               offendingSource,
		filepath.Join("vendor", "v", "bad.go"):          offendingSource,
		filepath.Join("node_modules", "n", "bad.go"):    offendingSource,
		filepath.Join("pkg", "rollback", "rollback.go"): offendingSource,
		filepath.Join("pkg", "rollbackx", "notself.go"): cleanSource,
		filepath.Join("a", "README.md"):                 "ROLLBACK",
	})

	got, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	want := []string{
		filepath.Join("a", "bad.go") + ":9",
		filepath.Join("a", "bad.go") + ":10",
		filepath.Join("a", "bad.go") + ":11",
		filepath.Join("a", "bad.go") + ":12",
		filepath.Join("a", "bad.go") + ":13",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Scan = %v, want %v", got, want)
	}
}

func TestScanReportsParseErrors(t *testing.T) {
	root := writeTree(t, map[string]string{"broken.go": "package \n func {"})
	if _, err := Scan(root); err == nil {
		t.Error("Scan returned nil error for an unparsable file")
	}
}

func TestScanReportsMissingRoot(t *testing.T) {
	if _, err := Scan(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("Scan returned nil error for a missing root")
	}
}

// TestNoDirectRollbacksInModule scans the pkg module (the parent of
// this package) so that a shared package cannot bypass the convention.
// The server, collector and alerter each carry the same check over
// their own module root.
func TestNoDirectRollbacksInModule(t *testing.T) {
	offenders, err := Scan(filepath.Join("..", "."))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(offenders) > 0 {
		t.Errorf("rollbacks must go through pkg/rollback; offending sites:\n  %s",
			strings.Join(offenders, "\n  "))
	}
}
