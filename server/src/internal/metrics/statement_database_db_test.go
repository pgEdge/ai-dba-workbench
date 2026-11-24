/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package metrics

import (
	"context"
	"maps"
	"testing"
)

// TestLookupStatementDatabases_Integration checks that the name lookup
// resolves each dbid from pg_stat_database and pg_stat_activity within
// the hour before the latest pg_stat_statements collection, preferring
// the newest name, and that it reports a failed read.
func TestLookupStatementDatabases_Integration(t *testing.T) {
	pool, closePool := newLatestRowsTestPool(t)
	defer closePool()
	ctx := context.Background()

	tables := []string{"pg_stat_statements", "pg_stat_database", "pg_stat_activity"}
	for _, name := range tables {
		dropTable(ctx, pool, name)
	}
	t.Cleanup(func() {
		for _, name := range tables {
			dropTable(context.Background(), pool, name)
		}
	})
	if _, err := pool.Exec(ctx, `
        CREATE SCHEMA IF NOT EXISTS metrics;
        CREATE TABLE metrics.pg_stat_statements (connection_id integer, collected_at timestamptz);
        CREATE TABLE metrics.pg_stat_database (connection_id integer, collected_at timestamptz,
            datid bigint, datname text);
        CREATE TABLE metrics.pg_stat_activity (connection_id integer, collected_at timestamptz,
            datid bigint, datname text);
        INSERT INTO metrics.pg_stat_statements VALUES
            (1, now() - interval '5 minutes'), (2, now());
        INSERT INTO metrics.pg_stat_database VALUES
            (1, now() - interval '30 minutes', 100, 'old_alpha'),
            (1, now() - interval '5 minutes', 100, 'alpha'),
            (1, now() - interval '2 hours', 300, 'too_old'),
            (1, now(), 400, 'after_latest'),
            (1, now() - interval '5 minutes', 500, NULL),
            (2, now(), 100, 'other_connection');
        INSERT INTO metrics.pg_stat_activity VALUES
            (1, now() - interval '10 minutes', 200, 'beta')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	names, err := LookupStatementDatabases(ctx, pool, 1)
	if err != nil {
		t.Fatalf("LookupStatementDatabases: %v", err)
	}
	want := map[int64]string{100: "alpha", 200: "beta"}
	if !maps.Equal(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}

	dropTable(ctx, pool, "pg_stat_activity")
	if _, err := LookupStatementDatabases(ctx, pool, 1); err == nil {
		t.Error("LookupStatementDatabases without pg_stat_activity should fail")
	}
}
