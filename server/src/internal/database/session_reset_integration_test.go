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
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/server/internal/config"
)

// sessionResetTestDSN returns the integration test database, skipping
// the test when none is configured.
func sessionResetTestDSN(t *testing.T) string {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping session reset integration test")
	}
	return connStr
}

// newSessionResetTestPool connects a Client to the test database with
// a single-connection pool, so a reacquire after a release either gets
// the same backend back or a replacement for a destroyed one.
func newSessionResetTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	connStr := sessionResetTestDSN(t)
	client := NewClient(&config.DatabaseConfig{PoolMaxConns: 1})
	if err := client.ConnectTo(connStr); err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	t.Cleanup(client.Close)

	pool := client.GetPoolFor(connStr)
	if pool == nil {
		t.Fatal("GetPoolFor returned nil after ConnectTo")
	}
	return pool
}

// newSessionResetTestConn opens a standalone connection, outside any
// pool, for calling the reset functions directly.
func newSessionResetTestConn(t *testing.T) *pgx.Conn {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, sessionResetTestDSN(t))
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		_ = conn.Close(closeCtx)
	})
	return conn
}

// terminateBackend ends pid from a separate connection, as an
// administrator or a server restart would, and waits until the backend
// has gone so the next statement on the victim fails deterministically.
func terminateBackend(ctx context.Context, t *testing.T, pid uint32) {
	t.Helper()

	admin := newSessionResetTestConn(t)
	var terminated bool
	if err := admin.QueryRow(ctx, "SELECT pg_terminate_backend($1, 5000)", int32(pid)).Scan(&terminated); err != nil { //nolint:gosec // G115: PostgreSQL PIDs fit in int32
		t.Fatalf("pg_terminate_backend failed: %v", err)
	}
	if !terminated {
		t.Fatalf("backend %d was not terminated", pid)
	}
}

func TestSessionResetOnRelease_ClearsSessionState(t *testing.T) {
	pool := newSessionResetTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	pid := conn.Conn().PgConn().PID()

	// Leave behind every kind of session state a tool call could set.
	setup := []string{
		"SET search_path TO pg_catalog",
		"SELECT set_config('work_mem', '1234kB', false)",
		"SET default_transaction_read_only = off",
		"CREATE TEMP TABLE issue709_leak (id int)",
		"PREPARE issue709_stmt AS SELECT 1",
		"SELECT pg_advisory_lock(709709)",
		"LISTEN issue709",
		"DECLARE issue709_cursor CURSOR WITH HOLD FOR SELECT 1",
		"CREATE TEMP SEQUENCE issue709_seq",
		"SELECT nextval('issue709_seq')",
		// Last, since the role may lack privileges the rest need.
		"SET ROLE pg_read_all_settings",
	}
	for _, stmt := range setup {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			conn.Release()
			t.Fatalf("%s failed: %v", stmt, err)
		}
	}

	// Run a parameterised query so pgx caches a named prepared
	// statement, which DISCARD ALL deallocates on the server.
	const cachedQuery = "SELECT $1::int + 1"
	var got int
	if err := conn.QueryRow(ctx, cachedQuery, 1).Scan(&got); err != nil {
		conn.Release()
		t.Fatalf("cached query failed: %v", err)
	}
	conn.Release()

	// MaxConns is 1, so this waits for the AfterRelease hook to finish.
	conn, err = pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("re-Acquire failed: %v", err)
	}
	defer conn.Release()

	if newPID := conn.Conn().PgConn().PID(); newPID != pid {
		t.Fatalf("expected the reset connection to be reused (pid %d), got pid %d", pid, newPID)
	}

	// The statement pgx cached before the reset must still work, which
	// it only does if the client-side statement cache was cleared too.
	if err := conn.QueryRow(ctx, cachedQuery, 1).Scan(&got); err != nil {
		t.Fatalf("cached query after reset failed: %v", err)
	}
	if got != 2 {
		t.Errorf("cached query after reset = %d, want 2", got)
	}

	checks := []struct {
		name  string
		query string
		want  string
	}{
		{"search_path", "SELECT current_setting('search_path')", ""},
		{"work_mem", "SELECT (current_setting('work_mem') <> '1234kB')::text", "true"},
		{"read-only default restored", "SHOW default_transaction_read_only", "on"},
		{"application_name kept", "SHOW application_name", "pgEdge AI DBA Workbench"},
		{"temp table dropped", "SELECT (to_regclass('pg_temp.issue709_leak') IS NULL)::text", "true"},
		{"prepared statement deallocated", "SELECT count(*)::text FROM pg_prepared_statements WHERE name = 'issue709_stmt'", "0"},
		{"role reset", "SELECT (current_user = session_user)::text", "true"},
		{"LISTEN cleared", "SELECT count(*)::text FROM pg_listening_channels()", "0"},
		{"WITH HOLD cursor closed", "SELECT count(*)::text FROM pg_cursors WHERE name = 'issue709_cursor'", "0"},
		{"advisory lock released", "SELECT count(*)::text FROM pg_locks WHERE locktype = 'advisory' AND pid = pg_backend_pid()", "0"},
	}
	for _, c := range checks {
		var value string
		if err := conn.QueryRow(ctx, c.query).Scan(&value); err != nil {
			t.Errorf("%s: query failed: %v", c.name, err)
			continue
		}
		if c.name == "search_path" {
			// The server default may be overridden for the role or
			// database; it must at least no longer be the value set
			// above.
			if strings.TrimSpace(value) == "pg_catalog" {
				t.Errorf("search_path survived the release: %q", value)
			}
			continue
		}
		if value != c.want {
			t.Errorf("%s: got %q, want %q", c.name, value, c.want)
		}
	}

	// DISCARD ALL forgets the sequence values this session has seen, so
	// lastval() has nothing to report.
	var lastval int64
	err = conn.QueryRow(ctx, "SELECT lastval()").Scan(&lastval)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55000" {
		t.Errorf("lastval() after reset: got value %d, error %v; want SQLSTATE 55000", lastval, err)
	}
}

func TestSessionResetOnRelease_DestroysConnectionWhenResetFails(t *testing.T) {
	pool := newSessionResetTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	pid := conn.Conn().PgConn().PID()
	createdBefore := pool.Stat().NewConnsCount()

	// The pool still believes the connection is healthy, so Release
	// hands it to the hook, whose DISCARD ALL then fails.
	terminateBackend(ctx, t, pid)
	conn.Release()

	conn, err = pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("re-Acquire failed: %v", err)
	}
	defer conn.Release()

	if newPID := conn.Conn().PgConn().PID(); newPID == pid {
		t.Fatalf("connection whose reset failed was reused (pid %d)", pid)
	}
	if created := pool.Stat().NewConnsCount(); created <= createdBefore {
		t.Errorf("expected a replacement connection, NewConnsCount stayed at %d", created)
	}
	if err := conn.Ping(ctx); err != nil {
		t.Errorf("replacement connection is unusable: %v", err)
	}
}

func TestResetSessionOnRelease_Direct(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("healthy connection is kept", func(t *testing.T) {
		conn := newSessionResetTestConn(t)
		if _, err := conn.Exec(ctx, "SET work_mem = '2345kB'"); err != nil {
			t.Fatalf("SET failed: %v", err)
		}
		if !resetSessionOnRelease(conn) {
			t.Fatal("resetSessionOnRelease returned false for a healthy connection")
		}
		var workMem string
		if err := conn.QueryRow(ctx, "SHOW work_mem").Scan(&workMem); err != nil {
			t.Fatalf("SHOW failed: %v", err)
		}
		if workMem == "2345kB" {
			t.Error("work_mem survived the reset")
		}
	})

	t.Run("terminated connection is destroyed", func(t *testing.T) {
		conn := newSessionResetTestConn(t)
		terminateBackend(ctx, t, conn.PgConn().PID())
		if resetSessionOnRelease(conn) {
			t.Fatal("resetSessionOnRelease returned true for a terminated connection")
		}
	})

	t.Run("open transaction is refused", func(t *testing.T) {
		conn := newSessionResetTestConn(t)
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatalf("Begin failed: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		err = resetSession(ctx, conn)
		if err == nil || !strings.Contains(err.Error(), "not idle") {
			t.Fatalf("resetSession in a transaction: got %v, want a not-idle error", err)
		}
	})

	t.Run("failed transaction is refused", func(t *testing.T) {
		conn := newSessionResetTestConn(t)
		tx, err := conn.Begin(ctx)
		if err != nil {
			t.Fatalf("Begin failed: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, "SELECT 1/0"); err == nil {
			t.Fatal("expected division by zero to fail")
		}

		if resetSessionOnRelease(conn) {
			t.Fatal("resetSessionOnRelease returned true in a failed transaction")
		}
	})

	t.Run("DISCARD ALL failure is reported", func(t *testing.T) {
		conn := newSessionResetTestConn(t)
		doneCtx, cancelNow := context.WithCancel(ctx)
		cancelNow()

		err := resetSession(doneCtx, conn)
		if err == nil || !strings.Contains(err.Error(), "DISCARD ALL") {
			t.Fatalf("resetSession with an ended context: got %v, want a DISCARD ALL error", err)
		}
	})
}
