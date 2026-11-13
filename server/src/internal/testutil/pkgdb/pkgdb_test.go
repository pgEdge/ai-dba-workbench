/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package pkgdb

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
	f.dsn = os.Getenv(EnvVar)
	if f.fn != nil {
		f.fn(f.dsn)
	}
	return f.code
}

// requireTestDatabase skips the test unless a test database is
// configured, and returns its URL.
func requireTestDatabase(t *testing.T) string {
	t.Helper()
	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	dsn := os.Getenv(EnvVar)
	if dsn == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping database test")
	}
	return dsn
}

// databaseExists reports whether the named database exists.
func databaseExists(t *testing.T, dsn, name string) bool {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	var exists bool
	if err := conn.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", name).
		Scan(&exists); err != nil {
		t.Fatalf("look up %s: %v", name, err)
	}
	return exists
}

// currentDatabase returns the name of the database dsn connects to.
func currentDatabase(t *testing.T, dsn string) string {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	var name string
	if err := conn.QueryRow(ctx, "SELECT current_database()").Scan(&name); err != nil {
		t.Fatalf("current_database: %v", err)
	}
	return name
}

func TestRunWithoutDatabase(t *testing.T) {
	tests := []struct {
		name    string
		skip    string
		dsn     string
		wantLog string
	}{
		{"skip set", "1", "postgres://postgres@127.0.0.1:1/x", ""},
		{"variable unset", "", "", ""},
		{"unparsable", "", "postgres://%zz", "cannot parse"},
		{"unreachable", "", "postgres://postgres@127.0.0.1:1/x?connect_timeout=2", "cannot connect"},
		{"a re-executed child", "", "postgres://postgres@127.0.0.1:1/x_pkg_unit?connect_timeout=2", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SKIP_DB_TESTS", tt.skip)
			t.Setenv(EnvVar, tt.dsn)
			var log bytes.Buffer
			m := &fakeM{code: 7}
			if got := run(m, "unit", &log); got != 7 {
				t.Errorf("run returned %d, want the tests' 7", got)
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

func TestRunRejectsBadTag(t *testing.T) {
	for _, tag := range []string{"", "API", "a-b", "1abc", "a_b"} {
		var log bytes.Buffer
		m := &fakeM{}
		if got := run(m, tag, &log); got != 1 {
			t.Errorf("tag %q: run returned %d, want 1", tag, got)
		}
		if m.ran {
			t.Errorf("tag %q: the tests ran", tag)
		}
	}
}

func TestRunCreatesAndDropsPackageDatabase(t *testing.T) {
	base := requireTestDatabase(t)
	t.Setenv("TEST_AI_WORKBENCH_KEEP_DB", "")
	want := currentDatabase(t, base) + Marker + "pkgdbrun"

	var log bytes.Buffer
	m := &fakeM{code: 3, fn: func(dsn string) {
		if got := currentDatabase(t, dsn); got != want {
			t.Errorf("tests connected to %q, want %q", got, want)
		}
	}}
	if got := run(m, "pkgdbrun", &log); got != 3 {
		t.Errorf("run returned %d, want the tests' 3", got)
	}
	if !m.ran {
		t.Fatal("the tests did not run")
	}
	if os.Getenv(EnvVar) != base {
		t.Errorf("%s not restored after the run", EnvVar)
	}
	if databaseExists(t, base, want) {
		t.Errorf("%s survived the run", want)
	}
	if log.Len() != 0 {
		t.Errorf("unexpected log output %q", log.String())
	}
}

func TestRunReplacesStaleDatabaseAndKeeps(t *testing.T) {
	base := requireTestDatabase(t)
	t.Setenv("TEST_AI_WORKBENCH_KEEP_DB", "true")
	name := currentDatabase(t, base) + Marker + "pkgdbkeep"

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	// Cleanups run last-in first-out: the drop runs before the close.
	t.Cleanup(func() { _ = conn.Close(ctx) })
	drop := func() {
		var stmt string
		if err := conn.QueryRow(ctx,
			"SELECT format('DROP DATABASE IF EXISTS %I WITH (FORCE)', $1::text)", name).
			Scan(&stmt); err != nil {
			t.Fatalf("build drop: %v", err)
		}
		if _, err := conn.Exec(ctx, stmt); err != nil {
			t.Fatalf("drop %s: %v", name, err)
		}
	}
	t.Cleanup(drop)

	// A stale copy holding a table must be replaced by an empty database.
	var log bytes.Buffer
	stale := &fakeM{fn: func(dsn string) {
		c, err := pgx.Connect(ctx, dsn)
		if err != nil {
			t.Fatalf("connect: %v", err)
		}
		defer func() { _ = c.Close(ctx) }()
		if _, err := c.Exec(ctx, "CREATE TABLE stale_marker (id int)"); err != nil {
			t.Fatalf("create marker: %v", err)
		}
	}}
	if got := run(stale, "pkgdbkeep", &log); got != 0 {
		t.Fatalf("first run returned %d: %s", got, log.String())
	}
	if !strings.Contains(log.String(), "keeping test database "+name) {
		t.Errorf("log %q does not report the kept database", log.String())
	}
	if !databaseExists(t, base, name) {
		t.Fatalf("%s was dropped despite TEST_AI_WORKBENCH_KEEP_DB", name)
	}

	fresh := &fakeM{fn: func(dsn string) {
		c, err := pgx.Connect(ctx, dsn)
		if err != nil {
			t.Fatalf("connect: %v", err)
		}
		defer func() { _ = c.Close(ctx) }()
		var reg *string
		if err := c.QueryRow(ctx, "SELECT to_regclass('stale_marker')::text").Scan(&reg); err != nil {
			t.Fatalf("look up marker: %v", err)
		}
		if reg != nil {
			t.Error("the stale database was not replaced")
		}
	}}
	if got := run(fresh, "pkgdbkeep", &log); got != 0 {
		t.Fatalf("second run returned %d: %s", got, log.String())
	}
}

func TestRunRejectsOverlongName(t *testing.T) {
	requireTestDatabase(t)
	var log bytes.Buffer
	m := &fakeM{}
	if got := run(m, strings.Repeat("x", MaxIdentifierLength), &log); got != 1 {
		t.Errorf("run returned %d, want 1", got)
	}
	if m.ran {
		t.Error("the tests ran against an overlong name")
	}
	if !strings.Contains(log.String(), "is longer than") {
		t.Errorf("log %q does not explain the refusal", log.String())
	}
}

func TestRunDropsDbnameParameter(t *testing.T) {
	base := requireTestDatabase(t)
	t.Setenv("TEST_AI_WORKBENCH_KEEP_DB", "")
	if !strings.HasPrefix(base, "postgres") {
		t.Skip("needs a URL-form test database")
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	t.Setenv(EnvVar, base+sep+"dbname=")
	var log bytes.Buffer
	m := &fakeM{}
	got := run(m, "pkgdburl", &log)
	if got != 0 || !m.ran {
		t.Fatalf("run returned %d (ran %v): %s", got, m.ran, log.String())
	}
	if strings.Contains(m.dsn, "dbname=") {
		t.Errorf("rewritten URL %q still carries dbname", m.dsn)
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
		{"url", "postgresql://postgres@127.0.0.1:5432/ai_workbench", "ai_workbench_pkg_api",
			"postgresql://postgres@127.0.0.1:5432/ai_workbench_pkg_api", ""},
		{"url without path", "postgres://postgres@127.0.0.1", "postgres_pkg_api",
			"postgres://postgres@127.0.0.1/postgres_pkg_api", ""},
		{"url keeps other parameters", "postgres://u@127.0.0.1/db?sslmode=disable",
			"db_pkg_x", "postgres://u@127.0.0.1/db_pkg_x?sslmode=disable", ""},
		{"url drops dbname parameter", "postgres://u@127.0.0.1/db?dbname=other&sslmode=disable",
			"db_pkg_x", "postgres://u@127.0.0.1/db_pkg_x?sslmode=disable", ""},
		{"keyword", "host=127.0.0.1 user=postgres dbname=ai_workbench", "ai_workbench_pkg_api",
			"host=127.0.0.1 user=postgres dbname=ai_workbench dbname='ai_workbench_pkg_api'", ""},
		{"keyword quoting", "host=127.0.0.1", `it's\x`,
			`host=127.0.0.1 dbname='it\'s\\x'`, ""},
		{"bad url", "postgres://%zz", "x", "", "parse TEST_AI_WORKBENCH_SERVER"},
		{"bad keyword", "host", "x", "", "parse the package database URL"},
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

func TestDropDatabaseReportsFailures(t *testing.T) {
	ctx := context.Background()
	unreachable, err := pgx.ParseConfig("postgres://postgres@127.0.0.1:1/x?connect_timeout=2")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var log bytes.Buffer
	dropDatabase(ctx, unreachable, "SELECT 1", "gone", &log)
	if !strings.Contains(log.String(), "drop gone") {
		t.Errorf("log %q does not report the failed connection", log.String())
	}

	base := requireTestDatabase(t)
	cfg, err := pgx.ParseConfig(base)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	log.Reset()
	dropDatabase(ctx, cfg, "SELECT * FROM no_such_table_for_pkgdb", "gone", &log)
	if !strings.Contains(log.String(), "drop gone") {
		t.Errorf("log %q does not report the failed drop", log.String())
	}
}

func TestCreateReportsFailures(t *testing.T) {
	base := requireTestDatabase(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	_ = conn.Close(ctx)
	if _, _, err := create(ctx, conn, "pkgdbclosed"); err == nil ||
		!strings.Contains(err.Error(), "name the package database") {
		t.Errorf("err = %v, want a naming failure", err)
	}
}

func TestRunWrapsRun(t *testing.T) {
	t.Setenv("SKIP_DB_TESTS", "1")
	m := &fakeM{code: 5}
	if got := Run(m, "unit"); got != 5 || !m.ran {
		t.Errorf("Run returned %d (ran %v), want the tests' 5", got, m.ran)
	}
}
