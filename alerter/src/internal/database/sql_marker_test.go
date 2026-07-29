/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Tests for the Workbench-internal SQL marker applied to the alerter's
// metric-evaluation queries against its own datastore. See GitHub issue
// #364: these queries previously leaked into the server's Top Queries
// panel even with "Hide monitoring queries" enabled.
package database

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/pkg/sqlmarker"
)

// assertTaggable fails the test unless Tag inserts the marker into sql
// after the leading keyword. Registry SQL that fails this check would be
// silently passed through untagged by queryInternal.
func assertTaggable(t *testing.T, what, sql string) {
	t.Helper()

	tagged := sqlmarker.Tag(sql)
	if !strings.Contains(tagged, sqlmarker.Marker) {
		t.Errorf("%s cannot be tagged; it has no leading keyword: %q",
			what, first80(sql))
		return
	}
	trimmed := strings.TrimLeft(tagged, " \t\r\n")
	if strings.HasPrefix(trimmed, sqlmarker.Comment) {
		t.Errorf("%s would carry the marker before its leading keyword, "+
			"where PostgreSQL strips it: %q", what, first80(tagged))
	}
}

// first80 truncates SQL for readable failure messages.
func first80(sql string) string {
	s := strings.Join(strings.Fields(sql), " ")
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}

// TestMetricRegistrySQLIsTaggable walks the whole metric registry and
// asserts that every latest and historical statement is in a shape that
// queryInternal can tag. This is the guard for statements added to the
// registry in future: the tagging happens centrally, so a statement that
// Tag cannot recognize would silently go unmarked.
func TestMetricRegistrySQLIsTaggable(t *testing.T) {
	if len(metricRegistry) == 0 {
		t.Fatal("metricRegistry is empty")
	}

	for name, cfg := range metricRegistry {
		if strings.TrimSpace(cfg.latestSQL) != "" {
			assertTaggable(t, name+".latestSQL", cfg.latestSQL)
		}
		if strings.TrimSpace(cfg.historicalSQL) != "" {
			assertTaggable(t, name+".historicalSQL", cfg.historicalSQL)
		}
	}
}

// markerTestDatastore returns a Datastore over the integration test
// database. It is deliberately lighter than
// newMetricRegistryTestDatastore because the metric scan helpers can be
// driven with literal SELECTs that touch no tables at all.
func markerTestDatastore(t *testing.T) *Datastore {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set; skipping")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}
	t.Cleanup(pool.Close)

	return &Datastore{pool: pool}
}

// TestQueryInternal_TagsSQL proves end-to-end that a registry-style query
// reaches PostgreSQL with the marker intact, by reading its own entry
// back out of pg_stat_statements. This is the behavior the server's
// filter depends on, and the reason the marker sits after the leading
// keyword rather than in front of the statement.
func TestQueryInternal_TagsSQL(t *testing.T) {
	ds := markerTestDatastore(t)
	ctx := context.Background()

	if _, err := ds.pool.Exec(ctx,
		"CREATE EXTENSION IF NOT EXISTS pg_stat_statements"); err != nil {
		t.Skipf("pg_stat_statements unavailable: %v", err)
	}

	// The statement must be identifiable in the view and must have a
	// queryid of its own, so it reads from a uniquely named scratch
	// table. Neither a string literal nor a column alias would do:
	// literals are normalized into $n placeholders, and aliases do not
	// contribute to the queryid, so a statement differing only in its
	// aliases collapses onto an existing entry and keeps that entry's
	// original text.
	tag := fmt.Sprintf("marker_probe_%d", time.Now().UnixNano())
	if _, err := ds.pool.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE %s (
			connection_id INTEGER,
			value DOUBLE PRECISION,
			collected_at TIMESTAMPTZ
		)`, tag)); err != nil {
		t.Fatalf("create scratch table: %v", err)
	}
	t.Cleanup(func() {
		if _, err := ds.pool.Exec(context.Background(),
			fmt.Sprintf("DROP TABLE IF EXISTS %s", tag)); err != nil {
			t.Logf("cleanup: drop %s: %v", tag, err)
		}
	})

	sql := fmt.Sprintf(
		"SELECT connection_id, value, collected_at FROM %s", tag)

	rows, err := ds.queryInternal(ctx, sql)
	if err != nil {
		t.Fatalf("queryInternal() error = %v", err)
	}
	// Drain the result so PostgreSQL finishes executing the statement
	// and pg_stat_statements records it before the assertion below.
	for rows.Next() {
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating tagged query: %v", err)
	}

	var count int
	err = ds.pool.QueryRow(ctx, `
        SELECT count(*) FROM pg_stat_statements
        WHERE query LIKE '%' || $1 || '%'
          AND query LIKE '%' || $2 || '%'
    `, tag, sqlmarker.Marker).Scan(&count)
	if err != nil {
		t.Fatalf("read pg_stat_statements: %v", err)
	}
	if count == 0 {
		t.Errorf("no pg_stat_statements entry for the tagged query "+
			"carries %q", sqlmarker.Marker)
	}
}

// TestMetricScanHelpers drives each scan helper through its happy path,
// its query-error path, and its scan-error path. The SQL is literal so
// the tests do not depend on the metrics schema.
func TestMetricScanHelpers(t *testing.T) {
	ds := markerTestDatastore(t)
	ctx := context.Background()

	t.Run("queryMetricValues", func(t *testing.T) {
		got, err := ds.queryMetricValues(ctx,
			"SELECT 7::int, 1.5::float8, now()")
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(got) != 1 || got[0].ConnectionID != 7 || got[0].Value != 1.5 {
			t.Fatalf("unexpected result: %+v", got)
		}

		if _, err := ds.queryMetricValues(ctx, "SELECT nonsense("); err == nil {
			t.Error("expected a query error for invalid SQL")
		}
		if _, err := ds.queryMetricValues(ctx,
			"SELECT 'x'::text, 'y'::text, 'z'::text"); err == nil {
			t.Error("expected a scan error for mistyped columns")
		}
	})

	t.Run("queryMetricValuesWithDB", func(t *testing.T) {
		got, err := ds.queryMetricValuesWithDB(ctx,
			"SELECT 7::int, 'appdb'::text, 1.5::float8, now()")
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(got) != 1 || got[0].DatabaseName == nil ||
			*got[0].DatabaseName != "appdb" {
			t.Fatalf("unexpected result: %+v", got)
		}

		if _, err := ds.queryMetricValuesWithDB(
			ctx, "SELECT nonsense("); err == nil {
			t.Error("expected a query error for invalid SQL")
		}
		if _, err := ds.queryMetricValuesWithDB(ctx,
			"SELECT 'x'::text, 'y'::text, 'z'::text, 'w'::text"); err == nil {
			t.Error("expected a scan error for mistyped columns")
		}
	})

	t.Run("queryMetricValuesWithDBAndObject", func(t *testing.T) {
		got, err := ds.queryMetricValuesWithDBAndObject(ctx,
			"SELECT 7::int, 'appdb'::text, 'slot_a'::text, "+
				"1.5::float8, now()")
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(got) != 1 || got[0].ObjectName == nil ||
			*got[0].ObjectName != "slot_a" {
			t.Fatalf("unexpected result: %+v", got)
		}

		if _, err := ds.queryMetricValuesWithDBAndObject(
			ctx, "SELECT nonsense("); err == nil {
			t.Error("expected a query error for invalid SQL")
		}
		if _, err := ds.queryMetricValuesWithDBAndObject(ctx,
			"SELECT 'x'::text, 'y'::text, 'z'::text, 'w'::text, "+
				"'v'::text"); err == nil {
			t.Error("expected a scan error for mistyped columns")
		}
	})

	t.Run("queryHistoricalMetricValuesBasic", func(t *testing.T) {
		got, err := ds.queryHistoricalMetricValuesBasic(ctx,
			"SELECT 7::int, NULL::text, 1.5::float8, now() "+
				"WHERE $1::int > 0", 7)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(got) != 1 || got[0].ConnectionID != 7 {
			t.Fatalf("unexpected result: %+v", got)
		}

		if _, err := ds.queryHistoricalMetricValuesBasic(
			ctx, "SELECT nonsense($1", 7); err == nil {
			t.Error("expected a query error for invalid SQL")
		}
		if _, err := ds.queryHistoricalMetricValuesBasic(ctx,
			"SELECT 'x'::text, 'y'::text, 'z'::text, 'w'::text "+
				"WHERE $1::int > 0", 7); err == nil {
			t.Error("expected a scan error for mistyped columns")
		}
	})

	t.Run("queryHistoricalMetricValuesWithDB", func(t *testing.T) {
		got, err := ds.queryHistoricalMetricValuesWithDB(ctx,
			"SELECT 7::int, 'appdb'::text, 1.5::float8, now() "+
				"WHERE $1::int > 0", 7)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(got) != 1 || got[0].DatabaseName == nil ||
			*got[0].DatabaseName != "appdb" {
			t.Fatalf("unexpected result: %+v", got)
		}

		if _, err := ds.queryHistoricalMetricValuesWithDB(
			ctx, "SELECT nonsense($1", 7); err == nil {
			t.Error("expected a query error for invalid SQL")
		}
		if _, err := ds.queryHistoricalMetricValuesWithDB(ctx,
			"SELECT 'x'::text, 'y'::text, 'z'::text, 'w'::text "+
				"WHERE $1::int > 0", 7); err == nil {
			t.Error("expected a scan error for mistyped columns")
		}
	})
}

// TestGetClusterPeers_ScanError covers the scan-error branch of
// GetClusterPeers, which now runs through queryInternal.
//
// The branch is only reachable when a column cannot be scanned into its
// destination, so the test puts a scratch schema holding a deliberately
// mistyped connections table ahead of public on the search path. The
// full schema is still created because the query also reads
// metrics.pg_node_role, which is schema-qualified and therefore not
// affected by the search path.
func TestGetClusterPeers_ScanError(t *testing.T) {
	_, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	schema := fmt.Sprintf("marker_scan_%d", time.Now().UnixNano())

	// name is boolean rather than text, so scanning it into a string
	// destination fails on the first row.
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
		CREATE SCHEMA %[1]s;
		CREATE TABLE %[1]s.connections (
			id INTEGER PRIMARY KEY,
			name BOOLEAN NOT NULL,
			cluster_id INTEGER
		);
		INSERT INTO %[1]s.connections VALUES (1, TRUE, 10), (2, FALSE, 10);
	`, schema)); err != nil {
		t.Fatalf("create scratch schema: %v", err)
	}
	// Registered with defer rather than t.Cleanup so the drop runs
	// while the pool is still open: the fixture's own cleanup closes
	// the pool, and deferred calls unwind before t.Cleanup functions.
	defer func() {
		if _, err := pool.Exec(context.Background(),
			fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schema),
		); err != nil {
			t.Logf("cleanup: drop schema %s: %v", schema, err)
		}
	}()

	cfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	scratchPool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	defer scratchPool.Close()

	ds := &Datastore{pool: scratchPool}
	if _, err := ds.GetClusterPeers(ctx, 1); err == nil {
		t.Fatal("expected a scan error, got nil")
	} else if !strings.Contains(err.Error(), "scan cluster peer") {
		t.Errorf("error = %v, want a scan failure", err)
	}
}
