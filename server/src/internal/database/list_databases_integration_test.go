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
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newListDatabasesTestPool connects to the Postgres instance named by
// TEST_AI_WORKBENCH_SERVER, skipping the test when none is configured.
func newListDatabasesTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping list databases test")
	}
	pool, err := pgxpool.New(context.Background(), connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestScanDatabasesSkipsNullSize feeds scanDatabases real rows in which
// one database has the NULL size that pg_database_size returns while that
// database is being dropped, and checks that the row is left out rather
// than failing the scan.
func TestScanDatabasesSkipsNullSize(t *testing.T) {
	pool := newListDatabasesTestPool(t)
	ctx := context.Background()

	rows, err := pool.Query(ctx, `
        SELECT name, owner, encoding, size
        FROM (VALUES
            ('alpha', 'postgres', 'UTF8', '8 kB'::text),
            ('dropping', 'postgres', 'UTF8', NULL::text),
            ('omega', 'postgres', 'UTF8', '16 kB'::text)
        ) AS v(name, owner, encoding, size)
        ORDER BY name
    `)
	if err != nil {
		t.Fatalf("query fixture rows: %v", err)
	}
	databases, err := scanDatabases(rows)
	if err != nil {
		t.Fatalf("scanDatabases failed: %v", err)
	}
	want := []DatabaseInfo{
		{Name: "alpha", Owner: "postgres", Encoding: "UTF8", Size: "8 kB"},
		{Name: "omega", Owner: "postgres", Encoding: "UTF8", Size: "16 kB"},
	}
	if len(databases) != len(want) {
		t.Fatalf("scanDatabases = %+v, want %+v", databases, want)
	}
	for i := range want {
		if databases[i] != want[i] {
			t.Errorf("scanDatabases[%d] = %+v, want %+v", i, databases[i], want[i])
		}
	}

	t.Run("all sizes NULL gives an empty list", func(t *testing.T) {
		rows, err := pool.Query(ctx,
			`SELECT 'dropping'::text, 'postgres'::text, 'UTF8'::text, NULL::text`)
		if err != nil {
			t.Fatalf("query fixture row: %v", err)
		}
		databases, err := scanDatabases(rows)
		if err != nil {
			t.Fatalf("scanDatabases failed: %v", err)
		}
		if databases == nil || len(databases) != 0 {
			t.Fatalf("scanDatabases = %#v, want an empty non-nil slice", databases)
		}
	})

	t.Run("scan failure is returned", func(t *testing.T) {
		rows, err := pool.Query(ctx, `SELECT 'only'::text, 'two'::text`)
		if err != nil {
			t.Fatalf("query fixture row: %v", err)
		}
		if databases, err := scanDatabases(rows); err == nil {
			t.Fatalf("scanDatabases = %+v, want a scan error", databases)
		}
	})
}

// listDatabasesVictim is the scratch database TestQueryDatabasesRetriesAfterDrop
// creates and drops; the statements below name it literally.
const (
	listDatabasesVictim             = "ai_workbench_listdb_victim"
	createListDatabasesVictim       = "CREATE DATABASE ai_workbench_listdb_victim"
	dropListDatabasesVictim         = "DROP DATABASE ai_workbench_listdb_victim"
	dropListDatabasesVictimIfExists = "DROP DATABASE IF EXISTS ai_workbench_listdb_victim"
	victimExistsQuery               = "SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_database WHERE datname = $1)"
)

// staleThenFreshQuerier sends its first query to a transaction whose
// snapshot predates a DROP DATABASE, and every later query to the pool.
type staleThenFreshQuerier struct {
	stale pgx.Tx
	fresh *pgxpool.Pool
	calls int
}

func (q *staleThenFreshQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	q.calls++
	if q.calls == 1 {
		return q.stale.Query(ctx, sql, args...)
	}
	return q.fresh.Query(ctx, sql, args...)
}

// TestQueryDatabasesRetriesAfterDrop reproduces the race in which a
// database is dropped after the catalog scan has seen it. A repeatable
// read transaction takes its snapshot, the database is then dropped and
// committed, and the listing query run in that snapshot still sees the
// database but fails with invalid_catalog_name when sizing it.
// queryDatabases must retry in a fresh snapshot and succeed.
func TestQueryDatabasesRetriesAfterDrop(t *testing.T) {
	pool := newListDatabasesTestPool(t)
	ctx := context.Background()

	// CREATE and DROP DATABASE take no parameters, so the statements are
	// literals naming a fixed scratch database. Any copy left by an
	// interrupted run is dropped first.
	if _, err := pool.Exec(ctx, dropListDatabasesVictimIfExists); err != nil {
		t.Fatalf("drop leftover %s: %v", listDatabasesVictim, err)
	}
	if _, err := pool.Exec(ctx, createListDatabasesVictim); err != nil {
		t.Fatalf("create %s: %v", listDatabasesVictim, err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), dropListDatabasesVictimIfExists); err != nil {
			t.Logf("drop %s: %v", listDatabasesVictim, err)
		}
	})

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatalf("begin repeatable read: %v", err)
	}
	defer func() {
		if err := tx.Rollback(context.Background()); err != nil &&
			!errors.Is(err, pgx.ErrTxClosed) {
			t.Logf("rollback: %v", err)
		}
	}()
	var seen bool
	if err := tx.QueryRow(ctx, victimExistsQuery, listDatabasesVictim).Scan(&seen); err != nil {
		t.Fatalf("take snapshot: %v", err)
	}
	if !seen {
		t.Fatalf("snapshot does not see %s", listDatabasesVictim)
	}

	if _, err := pool.Exec(ctx, dropListDatabasesVictim); err != nil {
		t.Fatalf("drop %s: %v", listDatabasesVictim, err)
	}

	q := &staleThenFreshQuerier{stale: tx, fresh: pool}
	databases, err := queryDatabases(ctx, q)
	if err != nil {
		t.Fatalf("queryDatabases failed: %v", err)
	}
	if q.calls != 2 {
		t.Fatalf("queryDatabases ran the query %d times, want 2", q.calls)
	}
	for _, db := range databases {
		if db.Name == listDatabasesVictim {
			t.Fatalf("queryDatabases listed the dropped database %q", listDatabasesVictim)
		}
	}
}

// droppedDatabaseQuerier fails every query with the error PostgreSQL
// raises when a database no longer exists.
type droppedDatabaseQuerier struct {
	calls int
}

func (q *droppedDatabaseQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) {
	q.calls++
	return nil, &pgconn.PgError{
		Code:    sqlStateInvalidCatalogName,
		Message: "database with OID 1 does not exist",
	}
}

// TestQueryDatabasesGivesUpAfterRepeatedDrops checks that the retry is
// bounded and that the final error is returned.
func TestQueryDatabasesGivesUpAfterRepeatedDrops(t *testing.T) {
	q := &droppedDatabaseQuerier{}
	databases, err := queryDatabases(context.Background(), q)
	if err == nil || !strings.Contains(err.Error(), "failed to query databases") {
		t.Fatalf("queryDatabases error = %v, want a query failure", err)
	}
	if !isDatabaseDroppedError(err) {
		t.Fatalf("queryDatabases error = %v, want it to wrap the 3D000 error", err)
	}
	if databases != nil {
		t.Fatalf("queryDatabases returned %+v alongside an error", databases)
	}
	if q.calls != listDatabasesAttempts {
		t.Fatalf("queryDatabases ran the query %d times, want %d", q.calls, listDatabasesAttempts)
	}
}

// TestIsDatabaseDroppedError covers the error classification.
func TestIsDatabaseDroppedError(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "plain error", err: errors.New("boom"), want: false},
		{name: "other SQLSTATE", err: &pgconn.PgError{Code: "42P01"}, want: false},
		{name: "invalid_catalog_name", err: &pgconn.PgError{Code: "3D000"}, want: true},
		{
			name: "wrapped invalid_catalog_name",
			err:  fmt.Errorf("failed to read databases: %w", &pgconn.PgError{Code: "3D000"}),
			want: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDatabaseDroppedError(tc.err); got != tc.want {
				t.Fatalf("isDatabaseDroppedError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
