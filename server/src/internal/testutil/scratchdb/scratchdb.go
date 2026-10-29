/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package scratchdb creates throwaway PostgreSQL databases for server
// integration tests that need the schema the collector really creates.
//
// Go cannot share helpers between the _test.go files of different
// packages, so the helper lives here. Only test code may import this
// package: it depends on the testing package and on the collector module,
// which the server binary does not build.
//
// A scratch database is named after the test database plus a suffix
// chosen by the calling package, so concurrent runs against per-session
// test databases on one cluster do not drop each other's scratch
// database, and packages running one after another do not reuse each
// other's. The server builds the name and quotes it with format('%I'),
// so the SQL text sent from Go stays constant.
package scratchdb

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	collectordb "github.com/pgedge/ai-workbench/collector/src/database"
)

// MaxIdentifierLength is PostgreSQL's NAMEDATALEN - 1. A longer database
// name would be truncated, and the pool would reach the wrong database.
const MaxIdentifierLength = 63

// statementsQuery returns the scratch database's name and the statements
// that drop and create it.
const statementsQuery = `
        SELECT n,
            format('DROP DATABASE IF EXISTS %I WITH (FORCE)', n),
            format('CREATE DATABASE %I', n)
        FROM (SELECT current_database() || $1 AS n) s
    `

// defaultPartitionsQuery returns, for each named table that is a
// partitioned table under the metrics schema, the statement that
// attaches a DEFAULT partition to it.
const defaultPartitionsQuery = `
        SELECT DISTINCT format('CREATE TABLE metrics.%I PARTITION OF metrics.%I DEFAULT',
                               tbl || '_default', tbl)
          FROM unnest($1::text[]) AS tbl
          JOIN pg_partitioned_table pt
            ON pt.partrelid = to_regclass(format('metrics.%I', tbl))
    `

// New creates an empty scratch database named after the database in
// TEST_AI_WORKBENCH_SERVER plus suffix, and returns a pool of at most
// maxConns connections on it. Any database left with that name by an
// earlier run is dropped first, and the new one is dropped when the test
// ends. The test is skipped when SKIP_DB_TESTS is set or
// TEST_AI_WORKBENCH_SERVER is not.
func New(t testing.TB, suffix string, maxConns int32) *pgxpool.Pool {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping scratch database test")
	}

	cfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}

	ctx := context.Background()
	admin, err := pgxpool.NewWithConfig(ctx, cfg.Copy())
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(admin.Close)

	var name, dropSQL, createSQL string
	if err := admin.QueryRow(ctx, statementsQuery, suffix).
		Scan(&name, &dropSQL, &createSQL); err != nil {
		t.Fatalf("name the scratch database: %v", err)
	}
	if len(name) > MaxIdentifierLength {
		t.Fatalf("scratch database name %q is longer than %d bytes", name, MaxIdentifierLength)
	}
	if _, err := admin.Exec(ctx, dropSQL); err != nil {
		t.Fatalf("drop stale %s: %v", name, err)
	}
	if _, err := admin.Exec(ctx, createSQL); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	// Cleanups run last-in first-out: the pool below closes before this
	// drop, which runs before the admin pool closes.
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), dropSQL); err != nil {
			t.Logf("drop %s: %v", name, err)
		}
	})

	cfg.ConnConfig.Database = name
	cfg.MaxConns = maxConns
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect to %s: %v", name, err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// NewCollectorSchema is New followed by MigrateCollectorSchema.
func NewCollectorSchema(t testing.TB, suffix string, maxConns int32) *pgxpool.Pool {
	t.Helper()
	pool := New(t, suffix, maxConns)
	MigrateCollectorSchema(t, pool)
	return pool
}

// MigrateCollectorSchema migrates the database behind pool to the
// collector's latest schema with the collector's own SchemaManager.
func MigrateCollectorSchema(t testing.TB, pool *pgxpool.Pool) {
	t.Helper()
	conn, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire connection for the collector schema: %v", err)
	}
	err = collectordb.NewSchemaManager().Migrate(conn)
	conn.Release()
	if err != nil {
		t.Fatalf("migrate collector schema: %v", err)
	}
}

// AttachDefaultPartitions attaches a DEFAULT partition, named after the
// table plus "_default", to each named metrics table. The collector
// creates weekly partitions as it collects; a default partition gives a
// fixture row with any timestamp somewhere to go. Names that are not
// partitioned tables under the metrics schema are ignored, and a name
// listed twice is attached once.
func AttachDefaultPartitions(t testing.TB, pool *pgxpool.Pool, tables []string) {
	t.Helper()
	ctx := context.Background()
	// Let the server quote each identifier with %I rather than building
	// the DDL by concatenation in Go.
	rows, err := pool.Query(ctx, defaultPartitionsQuery, tables)
	if err != nil {
		t.Fatalf("build default partition DDL: %v", err)
	}
	ddl, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("read default partition DDL: %v", err)
	}
	for _, stmt := range ddl {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("attach default partition (%s): %v", stmt, err)
		}
	}
}
