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
	"slices"
	"sort"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	collectordb "github.com/pgedge/ai-workbench/collector/src/database"
)

// The collector schema is migrated into a scratch database of its own,
// separate from the shared test database, because other tests in this
// package create metrics tables with the same names but different shapes
// there. The scratch name is the test database's name plus this suffix,
// so concurrent runs against per-session test databases on one cluster
// do not drop each other's scratch database. The server builds the name
// and quotes it with format('%I'), so the SQL text sent stays constant.
const entityKeySchemaSuffix = "_metrics_schema"

// entityKeySchemaStatementsQuery returns the scratch database's name and
// the statements that drop and create it.
const entityKeySchemaStatementsQuery = `
        SELECT n,
            format('DROP DATABASE IF EXISTS %I WITH (FORCE)', n),
            format('CREATE DATABASE %I', n)
        FROM (SELECT current_database() || $1 AS n) s
    `

// metricsKeyedProbesQuery lists every probe table in the metrics schema
// (partitioned parents and plain tables, not partitions) that has a
// primary key.
const metricsKeyedProbesQuery = `
        SELECT c.relname
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'metrics'
            AND c.relkind IN ('r', 'p')
            AND NOT c.relispartition
            AND EXISTS (SELECT 1 FROM pg_constraint con
                        WHERE con.conrelid = c.oid AND con.contype = 'p')
        ORDER BY c.relname
    `

// newMigratedMetricsPool creates the scratch database, migrates it to the
// collector's latest schema and returns a pool on it. The database is
// dropped when the test ends.
func newMigratedMetricsPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping entity key schema test")
	}

	ctx := context.Background()
	admin, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(admin.Close)

	var name, dropSQL, createSQL string
	if err := admin.QueryRow(ctx, entityKeySchemaStatementsQuery, entityKeySchemaSuffix).
		Scan(&name, &dropSQL, &createSQL); err != nil {
		t.Fatalf("name the scratch database: %v", err)
	}
	// NAMEDATALEN - 1: a longer name would be truncated and the pool
	// below would connect to the wrong database.
	if len(name) > 63 {
		t.Fatalf("scratch database name %q is longer than 63 bytes", name)
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
	cfg.MaxConns = 2
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
	defer conn.Release()
	if err := collectordb.NewSchemaManager().Migrate(conn); err != nil {
		t.Fatalf("migrate collector schema: %v", err)
	}
	return pool
}

// TestProbeEntityKeyFallbackMatchesCollectorSchema is the drift guard for
// probeEntityKeyFallback: against the schema the collector really
// creates, every probe with a primary key must be in the map with the
// columns its key gives, and the map must hold nothing else.
func TestProbeEntityKeyFallbackMatchesCollectorSchema(t *testing.T) {
	pool := newMigratedMetricsPool(t)
	ctx := context.Background()

	rows, err := pool.Query(ctx, metricsKeyedProbesQuery)
	if err != nil {
		t.Fatalf("list keyed probes: %v", err)
	}
	var probes []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("read keyed probes: %v", err)
		}
		probes = append(probes, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("read keyed probes: %v", err)
	}
	if len(probes) == 0 {
		t.Fatal("the migrated schema has no keyed metrics probes; the guard would prove nothing")
	}

	seen := make(map[string]bool, len(probes))
	for _, probe := range probes {
		seen[probe] = true
		keys, hasKey, err := primaryKeyEntityColumns(ctx, pool, probe)
		if err != nil {
			t.Fatalf("primary key of %s: %v", probe, err)
		}
		if !hasKey {
			t.Fatalf("%s has a primary key in pg_constraint but none in information_schema", probe)
		}
		want, ok := probeEntityKeyFallback[probe]
		if !ok {
			t.Errorf("probe %s has primary-key entity columns %q but no probeEntityKeyFallback entry", probe, keys)
			continue
		}
		if !slices.Equal(keys, want) {
			t.Errorf("probeEntityKeyFallback[%q] = %q, but its primary key gives %q", probe, want, keys)
		}
		// On plain PostgreSQL the map must never be what answers.
		got, err := GetProbeEntityKeyColumns(ctx, pool, probe)
		if err != nil {
			t.Fatalf("GetProbeEntityKeyColumns(%s): %v", probe, err)
		}
		if !slices.Equal(got, keys) {
			t.Errorf("GetProbeEntityKeyColumns(%s) = %q, want the primary key's %q", probe, got, keys)
		}
	}

	var stale []string
	for probe := range probeEntityKeyFallback {
		if !seen[probe] {
			stale = append(stale, probe)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("probeEntityKeyFallback has entries for probes with no primary key in the collector schema: %q", stale)
	}
}
