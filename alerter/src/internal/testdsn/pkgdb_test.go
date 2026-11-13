/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package testdsn

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// fakeM records whether the tests ran, and what the test database
// variable held while they did.
type fakeM struct {
	code int
	ran  bool
	dsn  string
	fn   func(dsn string)
}

func (f *fakeM) Run() int {
	f.ran = true
	f.dsn = os.Getenv(envVar)
	if f.fn != nil {
		f.fn(f.dsn)
	}
	return f.code
}

// connect opens a connection for the test and closes it at cleanup.
func connect(t *testing.T, dsn string) *pgx.Conn {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	return conn
}

// currentDatabase returns the name of the database dsn connects to.
func currentDatabase(t *testing.T, dsn string) string {
	t.Helper()
	var name string
	if err := connect(t, dsn).QueryRow(context.Background(),
		"SELECT current_database()").Scan(&name); err != nil {
		t.Fatalf("current_database: %v", err)
	}
	return name
}

// databaseExists reports whether the named database exists.
func databaseExists(t *testing.T, dsn, name string) bool {
	t.Helper()
	var exists bool
	if err := connect(t, dsn).QueryRow(context.Background(),
		"SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", name).
		Scan(&exists); err != nil {
		t.Fatalf("look up %s: %v", name, err)
	}
	return exists
}

// TestRunPackageLeavesTestsAlone covers every case in which RunPackage
// must run the tests against the configured DSN untouched, creating
// nothing, so that Require skips or refuses them exactly as it did
// before per-package databases existed.
func TestRunPackageLeavesTestsAlone(t *testing.T) {
	tests := []struct {
		name    string
		skip    string
		dsn     string
		wantLog string
	}{
		{"skip set", "1", "postgresql://postgres@127.0.0.1:5432/ai_workbench", ""},
		{"variable unset", "", "", ""},
		{"unparseable", "", "postgresql://user:pass@host:notaport/db", "parse test DSN"},
		{"remote host", "", "postgresql://postgres@db.example.com:5432/ai_workbench", "non-loopback host"},
		{"remote fallback host", "", "postgresql://postgres@127.0.0.1:5432,db.example.com:5432/ai_workbench", `non-loopback host "db.example.com"`},
		{"production-shaped database", "", "postgresql://postgres@127.0.0.1:5432/ai_workbench_prod2", "ai_workbench_prod2"},
		{"another package's database", "", "postgresql://postgres@127.0.0.1:5432/ai_workbench_pkg_engine", "ai_workbench_pkg_engine"},
		{"a re-executed child", "", "postgresql://postgres@127.0.0.1:5432/ai_workbench_pkg_unit", ""},
		{"unreachable", "", "postgresql://postgres@127.0.0.1:1/ai_workbench?connect_timeout=2", "cannot connect"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SKIP_DB_TESTS", tt.skip)
			t.Setenv(envVar, tt.dsn)
			var log bytes.Buffer
			m := &fakeM{code: 7}
			if got := runPackage(m, "unit", &log); got != 7 {
				t.Errorf("runPackage returned %d, want the tests' 7", got)
			}
			if !m.ran {
				t.Fatal("the tests did not run")
			}
			if m.dsn != tt.dsn {
				t.Errorf("tests saw %q, want the unchanged %q", m.dsn, tt.dsn)
			}
			if !strings.Contains(log.String(), tt.wantLog) {
				t.Errorf("log %q does not mention %q", log.String(), tt.wantLog)
			}
		})
	}
}

func TestRunPackageRejectsBadTag(t *testing.T) {
	for _, tag := range []string{"", "Engine", "a-b", "1abc", "a_b"} {
		var log bytes.Buffer
		m := &fakeM{}
		if got := runPackage(m, tag, &log); got != 1 {
			t.Errorf("tag %q: runPackage returned %d, want 1", tag, got)
		}
		if m.ran {
			t.Errorf("tag %q: the tests ran", tag)
		}
	}
}

func TestRunPackageWrapsRun(t *testing.T) {
	t.Setenv("SKIP_DB_TESTS", "1")
	m := &fakeM{code: 5}
	if got := RunPackage(m, "unit"); got != 5 || !m.ran {
		t.Errorf("RunPackage returned %d (ran %v), want the tests' 5", got, m.ran)
	}
}

func TestRunPackageCreatesAndDropsDatabase(t *testing.T) {
	base := Require(t, "the per-package database test")
	t.Setenv("TEST_AI_WORKBENCH_KEEP_DB", "")
	want := currentDatabase(t, base) + PackageMarker + "testdsnrun"

	var log bytes.Buffer
	m := &fakeM{code: 3, fn: func(dsn string) {
		if got := currentDatabase(t, dsn); got != want {
			t.Errorf("tests connected to %q, want %q", got, want)
		}
		// The derived database must pass the guard the tests call.
		if got := Require(t, "the derived database"); got != dsn {
			t.Errorf("Require returned %q, want %q", got, dsn)
		}
	}}
	if got := runPackage(m, "testdsnrun", &log); got != 3 {
		t.Errorf("runPackage returned %d, want the tests' 3: %s", got, log.String())
	}
	if !m.ran {
		t.Fatal("the tests did not run")
	}
	if os.Getenv(envVar) != base {
		t.Errorf("%s not restored after the run", envVar)
	}
	if databaseExists(t, base, want) {
		t.Errorf("%s survived the run", want)
	}
}

func TestRunPackageReplacesStaleDatabaseAndKeeps(t *testing.T) {
	base := Require(t, "the per-package database test")
	t.Setenv("TEST_AI_WORKBENCH_KEEP_DB", "true")
	name := currentDatabase(t, base) + PackageMarker + "testdsnkeep"
	ctx := context.Background()

	admin := connect(t, base)
	t.Cleanup(func() {
		var stmt string
		if err := admin.QueryRow(ctx,
			"SELECT format('DROP DATABASE IF EXISTS %I WITH (FORCE)', $1::text)", name).
			Scan(&stmt); err != nil {
			t.Errorf("build drop: %v", err)
			return
		}
		if _, err := admin.Exec(ctx, stmt); err != nil {
			t.Errorf("drop %s: %v", name, err)
		}
	})

	var log bytes.Buffer
	stale := &fakeM{fn: func(dsn string) {
		if _, err := connect(t, dsn).Exec(ctx, "CREATE TABLE stale_marker (id int)"); err != nil {
			t.Fatalf("create marker: %v", err)
		}
	}}
	if got := runPackage(stale, "testdsnkeep", &log); got != 0 {
		t.Fatalf("first run returned %d: %s", got, log.String())
	}
	if !strings.Contains(log.String(), "keeping test database "+name) {
		t.Errorf("log %q does not report the kept database", log.String())
	}
	if !databaseExists(t, base, name) {
		t.Fatalf("%s was dropped despite TEST_AI_WORKBENCH_KEEP_DB", name)
	}

	fresh := &fakeM{fn: func(dsn string) {
		var reg *string
		if err := connect(t, dsn).QueryRow(ctx,
			"SELECT to_regclass('stale_marker')::text").Scan(&reg); err != nil {
			t.Fatalf("look up marker: %v", err)
		}
		if reg != nil {
			t.Error("the stale database was not replaced")
		}
	}}
	if got := runPackage(fresh, "testdsnkeep", &log); got != 0 {
		t.Fatalf("second run returned %d: %s", got, log.String())
	}
}

func TestRunPackageRejectsOverlongName(t *testing.T) {
	Require(t, "the per-package database test")
	var log bytes.Buffer
	m := &fakeM{}
	if got := runPackage(m, strings.Repeat("x", maxIdentifierLength), &log); got != 1 {
		t.Errorf("runPackage returned %d, want 1", got)
	}
	if m.ran {
		t.Error("the tests ran against an overlong name")
	}
	if !strings.Contains(log.String(), "is longer than") {
		t.Errorf("log %q does not explain the refusal", log.String())
	}
}

func TestCreatePackageDatabaseReportsFailures(t *testing.T) {
	base := Require(t, "the per-package database test")
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	_ = conn.Close(ctx)
	if _, _, err := createPackageDatabase(ctx, conn, "testdsnclosed"); err == nil ||
		!strings.Contains(err.Error(), "name the package database") {
		t.Errorf("err = %v, want a naming failure", err)
	}
}

func TestDropPackageDatabaseReportsFailures(t *testing.T) {
	ctx := context.Background()
	unreachable, err := pgx.ParseConfig("postgres://postgres@127.0.0.1:1/x?connect_timeout=2")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var log bytes.Buffer
	dropPackageDatabase(ctx, unreachable, "SELECT 1", "gone", &log)
	if !strings.Contains(log.String(), "drop gone") {
		t.Errorf("log %q does not report the failed connection", log.String())
	}

	base := Require(t, "the per-package database test")
	cfg, err := pgx.ParseConfig(base)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	log.Reset()
	dropPackageDatabase(ctx, cfg, "SELECT * FROM no_such_table_for_testdsn", "gone", &log)
	if !strings.Contains(log.String(), "drop gone") {
		t.Errorf("log %q does not report the failed drop", log.String())
	}
}

func TestWithDatabase(t *testing.T) {
	tests := []struct {
		name    string
		dsn     string
		db      string
		want    string
		wantErr string
	}{
		{"url", "postgresql://postgres@127.0.0.1:5432/ai_workbench", "ai_workbench_pkg_engine",
			"postgresql://postgres@127.0.0.1:5432/ai_workbench_pkg_engine", ""},
		{"ci default", "postgresql://postgres@localhost:5432/postgres", "postgres_pkg_database",
			"postgresql://postgres@localhost:5432/postgres_pkg_database", ""},
		{"url drops dbname parameter",
			"postgres://u@127.0.0.1/ai_workbench?dbname=other&sslmode=disable",
			"ai_workbench_pkg_x", "postgres://u@127.0.0.1/ai_workbench_pkg_x?sslmode=disable", ""},
		{"keyword", "host=127.0.0.1 user=postgres dbname=ai_workbench_sess2", "ai_workbench_sess2_pkg_engine",
			"host=127.0.0.1 user=postgres dbname=ai_workbench_sess2 dbname='ai_workbench_sess2_pkg_engine'", ""},
		{"derived name refused by the guard", "host=127.0.0.1", `it's`, "", "refusing"},
		{"bad url", "postgres://%zz", "x", "", "parse TEST_AI_WORKBENCH_SERVER"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := withDatabase(tt.dsn, tt.db)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
