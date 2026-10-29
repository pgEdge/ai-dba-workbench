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
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/server/internal/testutil/scratchdb"
)

// The topology, estate and relationship tests run against the schema the
// collector really creates, migrated by its own SchemaManager, rather
// than a hand-built subset: those queries join a dozen tables and the
// subsets elsewhere in this package would drift from it. The schema goes
// into a scratch database of its own, named after the test database plus
// collectorSchemaSuffix, because other tests in this package create
// same-named tables with different shapes in the shared test database.
const collectorSchemaSuffix = "_db_schema"

// collectorSchemaPartitionedTables are the partitioned metrics tables the
// tests in this package write to.
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
	pool := scratchdb.NewCollectorSchema(t, collectorSchemaSuffix, 4)
	scratchdb.AttachDefaultPartitions(t, pool, collectorSchemaPartitionedTables)
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
