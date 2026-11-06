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
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/pkg/rollback"
)

func TestDatastoreExecMode(t *testing.T) {
	if got := datastoreExecMode(false); got != pgx.QueryExecModeCacheStatement {
		t.Errorf("plain PostgreSQL mode = %v, want the statement cache", got)
	}
	if got := datastoreExecMode(true); got != pgx.QueryExecModeDescribeExec {
		t.Errorf("ColdFront mode = %v, want describe-exec", got)
	}
}

// TestNewDatastoreDetectsColdFront checks that NewDatastore picks the
// execution mode from the datastore: a view in the metrics schema, as
// every metrics relation is on pgEdge ColdFront (issue #755), selects
// describe-exec, and without one the pool keeps pgx's statement cache.
func TestNewDatastoreDetectsColdFront(t *testing.T) {
	cfg := testDatastoreConfig(t)
	ctx := context.Background()

	plain, err := NewDatastore(cfg, "s")
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	defer plain.Close()
	pool := plain.GetPool()
	var already bool
	if err := pool.QueryRow(ctx, coldFrontDatastoreQuery).Scan(&already); err != nil {
		t.Fatalf("detect: %v", err)
	}
	if want := datastoreExecMode(already); pool.Config().ConnConfig.DefaultQueryExecMode != want {
		t.Errorf("mode = %v with ColdFront detected %v, want %v",
			pool.Config().ConnConfig.DefaultQueryExecMode, already, want)
	}

	if _, err := pool.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS metrics;
        DROP VIEW IF EXISTS metrics.exec_mode_test_view;
        CREATE VIEW metrics.exec_mode_test_view AS SELECT 1 AS connection_id`); err != nil {
		t.Fatalf("create the stand-in view: %v", err)
	}
	defer func() {
		if _, err := pool.Exec(context.Background(), `DROP VIEW IF EXISTS metrics.exec_mode_test_view`); err != nil {
			t.Logf("drop the stand-in view: %v", err)
		}
	}()
	cf, err := NewDatastore(cfg, "s")
	if err != nil {
		t.Fatalf("NewDatastore with a metrics view: %v", err)
	}
	defer cf.Close()
	if got := cf.GetPool().Config().ConnConfig.DefaultQueryExecMode; got != pgx.QueryExecModeDescribeExec {
		t.Errorf("mode with a metrics view = %v, want describe-exec", got)
	}
}

// TestDetectColdFrontDatastoreQueryFailure covers the catalog-query
// failure of detectColdFrontDatastore: the connection succeeds, but a
// schema placed ahead of pg_catalog on its search_path shadows
// pg_extension with a table that has no extname column, so the
// detection query fails.
func TestDetectColdFrontDatastoreQueryFailure(t *testing.T) {
	cfg := testDatastoreConfig(t)
	ctx := context.Background()

	base, err := pgxpool.ParseConfig(cfg.BuildConnectionString())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	conn, err := pgx.ConnectConfig(ctx, base.ConnConfig.Copy())
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	defer conn.Close(context.Background()) //nolint:errcheck // test cleanup
	if _, err := conn.Exec(ctx, `DROP SCHEMA IF EXISTS coldfront_detect_shadow CASCADE;
        CREATE SCHEMA coldfront_detect_shadow;
        CREATE TABLE coldfront_detect_shadow.pg_extension (x int)`); err != nil {
		t.Fatalf("create the shadowing schema: %v", err)
	}
	defer func() {
		if _, err := conn.Exec(context.Background(),
			`DROP SCHEMA IF EXISTS coldfront_detect_shadow CASCADE`); err != nil {
			t.Logf("drop the shadowing schema: %v", err)
		}
	}()

	shadowed := base.ConnConfig.Copy()
	shadowed.RuntimeParams["search_path"] = "coldfront_detect_shadow, pg_catalog"
	_, err = detectColdFrontDatastore(ctx, shadowed)
	if err == nil || !strings.Contains(err.Error(), "failed to detect the datastore kind") {
		t.Fatalf("detectColdFrontDatastore error = %v, want a detection failure", err)
	}
}

// preparedAfterRollback runs a parameterised statement in a transaction
// that rolls back, as a timed-out or read-only request's does, and then
// returns how many named prepared statements the server holds on that
// connection: the statements a later execution would reuse.
func preparedAfterRollback(t *testing.T, cfg *pgxpool.Config) int {
	t.Helper()
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT $1::int + 0`, 41).Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if err := rollback.Tx(ctx, tx); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if err := conn.QueryRow(ctx, `SELECT $1::int + 0`, 42).Scan(&n); err != nil || n != 42 {
		t.Fatalf("re-run after rollback = %d, %v", n, err)
	}

	var prepared int
	// A simple-protocol query, so this check adds no prepared statement.
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM pg_prepared_statements`,
		pgx.QueryExecModeSimpleProtocol).Scan(&prepared); err != nil {
		t.Fatalf("count prepared statements: %v", err)
	}
	return prepared
}

// TestDescribeExecKeepsNoStatementAcrossRollback reproduces as much of
// the ColdFront failure as plain PostgreSQL can. The failure needs a
// named prepared statement, planned in a transaction that rolled back,
// to be executed again on the same connection. In pgx's default mode
// such a statement survives the rollback; in describe-exec, the mode
// datastoreExecMode picks for ColdFront, none exists. That the error
// itself is gone on ColdFront is left to live verification.
func TestDescribeExecKeepsNoStatementAcrossRollback(t *testing.T) {
	cfg := testDatastoreConfig(t)
	base, err := pgxpool.ParseConfig(cfg.BuildConnectionString())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	cached := base.Copy()
	cached.ConnConfig.DefaultQueryExecMode = datastoreExecMode(false)
	if got := preparedAfterRollback(t, cached); got == 0 {
		t.Errorf("the statement cache kept no prepared statement across a rollback; the test no longer shows the mechanism")
	}

	describe := base.Copy()
	describe.ConnConfig.DefaultQueryExecMode = datastoreExecMode(true)
	if got := preparedAfterRollback(t, describe); got != 0 {
		t.Errorf("describe-exec left %d prepared statements after a rollback, want 0", got)
	}
}
