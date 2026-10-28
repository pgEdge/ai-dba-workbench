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
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	collectordb "github.com/pgedge/ai-workbench/collector/src/database"
	"github.com/pgedge/ai-workbench/pkg/anomaly"
)

// The baseline and anomaly comparison tests run against the schema the
// collector really creates, migrated by its own SchemaManager, because
// the shared historical queries in pkg/anomaly join connections and
// several metrics tables, and a hand-built subset would drift from them.
// The schema goes into a scratch database named after the test database
// plus metricsSchemaSuffix, so concurrent runs against per-session test
// databases on one cluster do not drop each other's scratch database.
// The server quotes the name with format('%I'), so the SQL sent from here
// stays constant.
const metricsSchemaSuffix = "_metrics_schema"

// metricsSchemaStatementsQuery returns the scratch database's name and
// the statements that drop and create it.
const metricsSchemaStatementsQuery = `
        SELECT n,
            format('DROP DATABASE IF EXISTS %I WITH (FORCE)', n),
            format('CREATE DATABASE %I', n)
        FROM (SELECT current_database() || $1 AS n) s
    `

// newCollectorSchemaPool creates the scratch database, migrates it to the
// collector's latest schema, attaches a DEFAULT partition to every
// metrics table a baselined metric reads (the collector creates weekly
// partitions as it collects, so without one a fixture row has nowhere to
// go), and returns a pool on it. The database is dropped when the test
// ends.
func newCollectorSchemaPool(t *testing.T) *pgxpool.Pool {
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
	if err := admin.QueryRow(ctx, metricsSchemaStatementsQuery, metricsSchemaSuffix).
		Scan(&name, &dropSQL, &createSQL); err != nil {
		t.Fatalf("name the scratch database: %v", err)
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

	tables := []string{"pg_node_role", "pg_settings"}
	for _, def := range anomaly.BaselineMetrics() {
		tables = append(tables, def.ProbeName)
	}
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT format('CREATE TABLE metrics.%I PARTITION OF metrics.%I DEFAULT',
		                       tbl || '_default', tbl)
		  FROM unnest($1::text[]) AS tbl
		  JOIN pg_partitioned_table pt
		    ON pt.partrelid = to_regclass(format('metrics.%I', tbl))`, tables)
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
	return pool
}

// execSchema runs one statement against the scratch database and fails
// the test on error.
func execSchema(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// insertTestConnection adds a live connection row and returns its id.
func insertTestConnection(t *testing.T, pool *pgxpool.Pool, name string) int {
	t.Helper()
	var id int
	if err := pool.QueryRow(context.Background(), `
        INSERT INTO connections (name, host, port, database_name, username,
                                 owner_username, is_monitored)
        VALUES ($1, 'localhost', 5432, 'postgres', 'postgres', 'test-owner', TRUE)
        RETURNING id`, name).Scan(&id); err != nil {
		t.Fatalf("insert connection %s: %v", name, err)
	}
	return id
}
