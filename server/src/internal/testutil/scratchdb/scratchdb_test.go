/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package scratchdb

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// fakeTB records what a helper reports instead of failing the real
// test, so the failure branches can be exercised. Fatalf and Skip end
// the calling goroutine as the real ones do; run drives a helper on a
// goroutine of its own for that reason.
type fakeTB struct {
	testing.TB
	fatal    string
	skipped  string
	logs     []string
	cleanups []func()
}

func (f *fakeTB) Helper() {}

func (f *fakeTB) Fatalf(format string, args ...any) {
	f.fatal = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

func (f *fakeTB) Skip(args ...any) {
	f.skipped = fmt.Sprint(args...)
	runtime.Goexit()
}

func (f *fakeTB) Logf(format string, args ...any) {
	f.logs = append(f.logs, fmt.Sprintf(format, args...))
}

func (f *fakeTB) Cleanup(fn func()) { f.cleanups = append(f.cleanups, fn) }

// call calls fn with the fake on a separate goroutine and waits for it
// to return or stop.
func (f *fakeTB) call(fn func(tb testing.TB)) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(f)
	}()
	<-done
}

// run is call followed by the registered cleanups, last-in first-out.
func (f *fakeTB) run(fn func(tb testing.TB)) {
	f.call(fn)
	for i := len(f.cleanups) - 1; i >= 0; i-- {
		f.cleanups[i]()
	}
}

// requireTestDatabase skips the test unless a test database is
// configured, and returns its URL.
func requireTestDatabase(t *testing.T) string {
	t.Helper()
	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping scratch database test")
	}
	return connStr
}

// databaseExists reports whether a database called name exists on the
// test cluster.
func databaseExists(t *testing.T, connStr, name string) bool {
	t.Helper()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	defer pool.Close()
	var exists bool
	if err := pool.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_database WHERE datname = $1)",
		name).Scan(&exists); err != nil {
		t.Fatalf("look up %s: %v", name, err)
	}
	return exists
}

func TestNewSkips(t *testing.T) {
	tests := []struct {
		name     string
		skipDB   string
		connStr  string
		expected string
	}{
		{"SKIP_DB_TESTS set", "1", "postgres://127.0.0.1/x", "SKIP_DB_TESTS is set"},
		{"no test database", "", "", "TEST_AI_WORKBENCH_SERVER not set"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SKIP_DB_TESTS", tt.skipDB)
			t.Setenv("TEST_AI_WORKBENCH_SERVER", tt.connStr)
			f := &fakeTB{}
			f.run(func(tb testing.TB) { New(tb, "_scratchdb_skip", 1) })
			if !strings.Contains(f.skipped, tt.expected) {
				t.Errorf("skip message %q does not contain %q", f.skipped, tt.expected)
			}
		})
	}
}

func TestNewFailures(t *testing.T) {
	tests := []struct {
		name     string
		connStr  string
		suffix   string
		maxConns int32
		expected string
	}{
		{"malformed URL", "postgres://%zz", "_scratchdb_bad", 1, "parse test database URL"},
		{"unreachable server", "postgres://postgres@127.0.0.1:1/postgres?connect_timeout=2",
			"_scratchdb_bad", 1, "name the scratch database"},
		{"name too long", "", strings.Repeat("x", MaxIdentifierLength), 1, "is longer than"},
		{"no connections allowed", "", "_scratchdb_zero", 0, "connect to"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connStr := tt.connStr
			if connStr == "" {
				connStr = requireTestDatabase(t)
			}
			t.Setenv("SKIP_DB_TESTS", "")
			t.Setenv("TEST_AI_WORKBENCH_SERVER", connStr)
			f := &fakeTB{}
			f.run(func(tb testing.TB) { New(tb, tt.suffix, tt.maxConns) })
			if !strings.Contains(f.fatal, tt.expected) {
				t.Errorf("fatal message %q does not contain %q", f.fatal, tt.expected)
			}
		})
	}
}

func TestNewCreatesAndDropsDatabase(t *testing.T) {
	connStr := requireTestDatabase(t)

	f := &fakeTB{}
	var name string
	f.run(func(tb testing.TB) {
		pool := New(tb, "_scratchdb_new", 1)
		if err := pool.QueryRow(context.Background(),
			"SELECT current_database()").Scan(&name); err != nil {
			t.Errorf("query scratch database: %v", err)
		}
		if !databaseExists(t, connStr, name) {
			t.Errorf("scratch database %s does not exist", name)
		}
	})
	if f.fatal != "" {
		t.Fatalf("New failed: %s", f.fatal)
	}
	if !strings.HasSuffix(name, "_scratchdb_new") {
		t.Errorf("scratch database %q does not end with the suffix", name)
	}
	if databaseExists(t, connStr, name) {
		t.Errorf("scratch database %s survived cleanup", name)
	}
}

func TestNewLogsFailedDrop(t *testing.T) {
	connStr := requireTestDatabase(t)

	f := &fakeTB{}
	var name string
	f.call(func(tb testing.TB) {
		pool := New(tb, "_scratchdb_logdrop", 1)
		if err := pool.QueryRow(context.Background(),
			"SELECT current_database()").Scan(&name); err != nil {
			t.Errorf("query scratch database: %v", err)
		}
	})
	if f.fatal != "" || len(f.cleanups) != 3 {
		t.Fatalf("New failed (%s) or registered %d cleanups", f.fatal, len(f.cleanups))
	}
	// The cleanups are the admin pool's Close, the drop and the scratch
	// pool's Close. Closing both pools before the drop runs makes the
	// drop fail, which is logged rather than fatal.
	f.cleanups[2]()
	f.cleanups[0]()
	f.cleanups[1]()
	if len(f.logs) != 1 || !strings.Contains(f.logs[0], "drop "+name) {
		t.Errorf("expected the failed drop of %s to be logged, got %q", name, f.logs)
	}

	// Drop the database the failed cleanup left behind.
	pool, err := pgxpool.New(context.Background(), connStr)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	defer pool.Close()
	var leftover, dropSQL, createSQL string
	if err := pool.QueryRow(context.Background(), statementsQuery, "_scratchdb_logdrop").
		Scan(&leftover, &dropSQL, &createSQL); err != nil {
		t.Fatalf("name the leftover database: %v", err)
	}
	if _, err := pool.Exec(context.Background(), dropSQL); err != nil {
		t.Fatalf("drop %s: %v", leftover, err)
	}
}

func TestCollectorSchemaAndPartitions(t *testing.T) {
	requireTestDatabase(t)
	pool := NewCollectorSchema(t, "_scratchdb_schema", 2)
	ctx := context.Background()

	var connections *string
	if err := pool.QueryRow(ctx,
		"SELECT to_regclass('public.connections')::text").Scan(&connections); err != nil {
		t.Fatalf("look up connections: %v", err)
	}
	if connections == nil {
		t.Fatal("the collector schema has no connections table")
	}

	// pg_settings twice and a table that is not partitioned: the first
	// gets one default partition, the second is ignored.
	AttachDefaultPartitions(t, pool, []string{"pg_settings", "pg_settings", "connections"})
	var count int
	if err := pool.QueryRow(ctx, `
        SELECT count(*) FROM pg_catalog.pg_class c
          JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
         WHERE n.nspname = 'metrics' AND c.relname LIKE '%\_default'`).Scan(&count); err != nil {
		t.Fatalf("count default partitions: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 default partition, got %d", count)
	}

	t.Run("attaching twice fails", func(t *testing.T) {
		f := &fakeTB{}
		f.run(func(tb testing.TB) { AttachDefaultPartitions(tb, pool, []string{"pg_settings"}) })
		if !strings.Contains(f.fatal, "attach default partition") {
			t.Errorf("fatal message %q does not mention the attach", f.fatal)
		}
	})

	t.Run("migrating an existing schema is a no-op", func(t *testing.T) {
		f := &fakeTB{}
		f.run(func(tb testing.TB) { MigrateCollectorSchema(tb, pool) })
		if f.fatal != "" {
			t.Errorf("re-migration failed: %s", f.fatal)
		}
	})
}

func TestHelpersFailOnClosedPool(t *testing.T) {
	connStr := requireTestDatabase(t)
	pool, err := pgxpool.New(context.Background(), connStr)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	pool.Close()

	tests := []struct {
		name     string
		call     func(tb testing.TB)
		expected string
	}{
		{"migrate", func(tb testing.TB) { MigrateCollectorSchema(tb, pool) }, "acquire connection"},
		{"partitions", func(tb testing.TB) {
			AttachDefaultPartitions(tb, pool, []string{"pg_settings"})
		}, "build default partition DDL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeTB{}
			f.run(tt.call)
			if !strings.Contains(f.fatal, tt.expected) {
				t.Errorf("fatal message %q does not contain %q", f.fatal, tt.expected)
			}
		})
	}
}
