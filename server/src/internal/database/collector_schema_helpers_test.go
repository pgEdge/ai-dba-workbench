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
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	collectordb "github.com/pgedge/ai-workbench/collector/src/database"
)

// The topology, estate and relationship tests run against the schema the
// collector really creates, migrated by its own SchemaManager, rather
// than a hand-built subset: those queries join a dozen tables and the
// subsets elsewhere in this package would drift from it. The schema goes
// into a scratch database of its own because other tests in this package
// create same-named tables with different shapes in the shared test
// database. The scratch name is the test database's name plus
// collectorSchemaSuffix, so concurrent runs against per-session test
// databases on one cluster do not drop each other's scratch database;
// the server quotes the name with format('%I'), so the SQL text sent
// from here stays constant.
const collectorSchemaSuffix = "_db_schema"

// collectorSchemaStatementsQuery returns the scratch database's name and
// the statements that drop and create it.
const collectorSchemaStatementsQuery = `
        SELECT n,
            format('DROP DATABASE IF EXISTS %I WITH (FORCE)', n),
            format('CREATE DATABASE %I', n)
        FROM (SELECT current_database() || $1 AS n) s
    `

// collectorSchemaMaxIdentifier is PostgreSQL's NAMEDATALEN - 1; a longer
// name would be truncated and the pool would reach the wrong database.
const collectorSchemaMaxIdentifier = 63

// collectorSchemaPartitionedTables are the partitioned metrics tables the
// tests in this package write to. The collector creates weekly
// partitions as it collects; the tests attach a DEFAULT partition
// instead so a row with any timestamp has somewhere to go.
var collectorSchemaPartitionedTables = []string{
	"pg_connectivity", "pg_node_role", "pg_server_info", "pg_sys_os_info",
	"pg_extension", "pg_settings", "pg_sys_cpu_info", "pg_sys_memory_info",
	"pg_sys_disk_info", "pg_hba_file_rules", "pg_ident_file_mappings",
}

// newCollectorSchemaDatastore creates the scratch database, migrates it
// to the collector's latest schema, attaches a default partition to each
// metrics table the tests write, and returns a Datastore and its pool.
// The database is dropped when the test ends.
func newCollectorSchemaDatastore(t *testing.T) (*Datastore, *pgxpool.Pool) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping collector schema test")
	}

	ctx := context.Background()
	admin, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(admin.Close)

	var name, dropSQL, createSQL string
	if err := admin.QueryRow(ctx, collectorSchemaStatementsQuery, collectorSchemaSuffix).
		Scan(&name, &dropSQL, &createSQL); err != nil {
		t.Fatalf("name the scratch database: %v", err)
	}
	if len(name) > collectorSchemaMaxIdentifier {
		t.Fatalf("scratch database name %q is longer than %d bytes", name, collectorSchemaMaxIdentifier)
	}
	if _, err := admin.Exec(ctx, dropSQL); err != nil {
		t.Fatalf("drop stale %s: %v", name, err)
	}
	if _, err := admin.Exec(ctx, createSQL); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}

	cfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}
	cfg.ConnConfig.Database = name
	cfg.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect to %s: %v", name, err)
	}
	// Cleanups run last-in first-out: the pool closes before the drop,
	// which runs before the admin pool closes.
	t.Cleanup(func() {
		pool.Close()
		if _, err := admin.Exec(context.Background(), dropSQL); err != nil {
			t.Logf("drop %s: %v", name, err)
		}
	})

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection to %s: %v", name, err)
	}
	err = collectordb.NewSchemaManager().Migrate(conn)
	conn.Release()
	if err != nil {
		t.Fatalf("migrate collector schema: %v", err)
	}

	for _, table := range collectorSchemaPartitionedTables {
		if _, err := pool.Exec(ctx, "CREATE TABLE metrics."+table+"_default PARTITION OF metrics."+table+" DEFAULT"); err != nil {
			t.Fatalf("attach default partition to metrics.%s: %v", table, err)
		}
	}

	return NewTestDatastore(pool), pool
}

// execCollectorSchema runs one statement against the scratch database and
// fails the test on error.
func execCollectorSchema(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}
