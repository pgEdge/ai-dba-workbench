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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/pkg/sqlmarker"
)

// assertTagged fails the test unless tagged carries the marker in a
// position that PostgreSQL preserves when it normalizes the statement
// for pg_stat_statements, which is to say after the leading keyword
// rather than in front of it.
func assertTagged(t *testing.T, what, tagged string) {
	t.Helper()

	if !strings.Contains(tagged, sqlmarker.Marker) {
		t.Errorf("%s is not tagged: %q", what, first80(tagged))
		return
	}
	trimmed := strings.TrimLeft(tagged, " \t\r\n")
	if strings.HasPrefix(trimmed, sqlmarker.Comment) {
		t.Errorf("%s carries the marker before its leading keyword, "+
			"where PostgreSQL strips it: %q", what, first80(tagged))
	}
}

// assertTaggable fails the test unless Tag inserts the marker into sql
// after the leading keyword. Registry SQL that fails this check would be
// silently passed through untagged by queryInternal.
func assertTaggable(t *testing.T, what, sql string) {
	t.Helper()

	assertTagged(t, what, sqlmarker.Tag(sql))
}

// fakeRowQuerier records the statements handed to it, standing in for
// the connection pool so the tagging can be asserted without a
// database. Returning a nil pgx.Rows is safe because the tests that use
// it call queryTagged directly and never iterate the result.
type fakeRowQuerier struct {
	sql  []string
	args [][]any
}

func (f *fakeRowQuerier) Query(_ context.Context, sql string,
	args ...any) (pgx.Rows, error) {
	f.sql = append(f.sql, sql)
	f.args = append(f.args, args)
	return nil, nil
}

// TestQueryTagged_TagsSQL asserts the chokepoint every registry query
// passes through tags the statement and forwards its arguments
// untouched. This needs no database, so it guards the fix on every run,
// including on servers where pg_stat_statements is not loaded and the
// end-to-end test below skips.
func TestQueryTagged_TagsSQL(t *testing.T) {
	q := &fakeRowQuerier{}
	ctx := context.Background()

	if _, err := queryTagged(ctx, q,
		"SELECT connection_id FROM metrics.pg_stat_activity "+
			"WHERE collected_at > now() - ($1 || ' days')::interval",
		7); err != nil {
		t.Fatalf("queryTagged() error = %v", err)
	}

	if len(q.sql) != 1 {
		t.Fatalf("recorded %d statements, want 1", len(q.sql))
	}
	assertTagged(t, "queryTagged statement", q.sql[0])
	if !strings.HasPrefix(q.sql[0], "SELECT "+sqlmarker.Comment+" ") {
		t.Errorf("marker is not immediately after the keyword: %q",
			first80(q.sql[0]))
	}
	if len(q.args[0]) != 1 || q.args[0][0] != 7 {
		t.Errorf("args = %v, want [7]", q.args[0])
	}
}

// TestQueryTagged_TagsEveryRegistryStatement drives every statement in
// the metric registry through the real chokepoint with a fake pool, so
// the tagging of the alerter's whole metric-evaluation surface is
// verified without a database. It complements
// TestMetricRegistrySQLIsTaggable, which checks only that Tag can
// handle the SQL, by proving that the code path actually applies it.
func TestQueryTagged_TagsEveryRegistryStatement(t *testing.T) {
	if len(metricRegistry) == 0 {
		t.Fatal("metricRegistry is empty")
	}
	ctx := context.Background()

	for name, cfg := range metricRegistry {
		for label, sql := range map[string]string{
			name + ".latestSQL":     cfg.latestSQL,
			name + ".historicalSQL": cfg.historicalSQL,
		} {
			if strings.TrimSpace(sql) == "" {
				continue
			}
			q := &fakeRowQuerier{}
			if _, err := queryTagged(ctx, q, sql, 1); err != nil {
				t.Fatalf("queryTagged(%s) error = %v", label, err)
			}
			if len(q.sql) != 1 {
				t.Fatalf("%s recorded %d statements, want 1",
					label, len(q.sql))
			}
			assertTagged(t, label, q.sql[0])
		}
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

// requirePgStatStatements skips the test unless pg_stat_statements is
// both installed and readable on the test server.
//
// Readability has to be established by actually reading the view rather
// than by inspecting shared_preload_libraries or pg_extension, because
// the extension can be created successfully and still raise SQLSTATE
// 55000 ("must be loaded via shared_preload_libraries") on every
// select. That is exactly how the CI PostgreSQL containers are
// configured, and probing the read also covers the extension being
// absent, unprivileged, or otherwise unusable, for whatever reason.
func requirePgStatStatements(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	if _, err := pool.Exec(ctx,
		"CREATE EXTENSION IF NOT EXISTS pg_stat_statements"); err != nil {
		t.Skipf("skipping end-to-end marker check: the "+
			"pg_stat_statements extension cannot be created: %v", err)
	}
	if _, err := pool.Exec(ctx,
		"SELECT count(*) FROM pg_stat_statements"); err != nil {
		t.Skipf("skipping end-to-end marker check: the "+
			"pg_stat_statements view is not readable, most likely "+
			"because the library is not listed in "+
			"shared_preload_libraries: %v", err)
	}
}

// TestQueryInternal_TagsSQL proves end-to-end that a registry-style query
// reaches PostgreSQL with the marker intact, by reading its own entry
// back out of pg_stat_statements. This is the behavior the server's
// filter depends on, and the reason the marker sits after the leading
// keyword rather than in front of the statement.
//
// The test skips where pg_stat_statements is not loaded; the tagging
// itself is asserted unconditionally by TestQueryTagged_TagsSQL and
// TestQueryTagged_TagsEveryRegistryStatement above.
func TestQueryInternal_TagsSQL(t *testing.T) {
	ds := markerTestDatastore(t)
	ctx := context.Background()

	requirePgStatStatements(t, ds.pool)

	// The statement must be identifiable in the view and must have a
	// queryid of its own, so it reads from a uniquely named scratch
	// table. Neither a string literal nor a column alias would do:
	// literals are normalized into $n placeholders, and aliases do not
	// contribute to the queryid, so a statement differing only in its
	// aliases collapses onto an existing entry and keeps that entry's
	// original text.
	tag := fmt.Sprintf("marker_probe_%d", time.Now().UnixNano())
	tagIdent := pgx.Identifier{tag}.Sanitize()
	// The relation name is generated from a timestamp above and quoted
	// with pgx.Identifier; CREATE TABLE cannot name its relation with a
	// bind parameter.
	//nosemgrep: go_sql_rule-concat-sqli -- test-only DDL; timestamp-derived name sanitized by pgx.Identifier
	if _, err := ds.pool.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE %s (
			connection_id INTEGER,
			value DOUBLE PRECISION,
			collected_at TIMESTAMPTZ
		)`, tagIdent)); err != nil {
		t.Fatalf("create scratch table: %v", err)
	}
	t.Cleanup(func() {
		//nosemgrep: go_sql_rule-concat-sqli -- test-only DDL; same sanitized identifier as the CREATE above
		if _, err := ds.pool.Exec(context.Background(),
			fmt.Sprintf("DROP TABLE IF EXISTS %s", tagIdent)); err != nil {
			t.Logf("cleanup: drop %s: %v", tagIdent, err)
		}
	})

	sql := fmt.Sprintf(
		"SELECT connection_id, value, collected_at FROM %s", tagIdent)

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
	// The SQL is a fixed literal; the table name and the marker are
	// bound as $1 and $2 rather than concatenated into it.
	//nosemgrep: go_sql_rule-concat-sqli -- fixed SQL literal; table name and marker bound as $1 and $2
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
	schemaIdent := pgx.Identifier{schema}.Sanitize()

	// name is boolean rather than text, so scanning it into a string
	// destination fails on the first row. The schema name is generated
	// from a timestamp above and quoted with pgx.Identifier; CREATE
	// SCHEMA cannot name its schema with a bind parameter.
	//nosemgrep: go_sql_rule-concat-sqli -- test-only DDL; timestamp-derived schema name sanitized by pgx.Identifier
	if _, err := pool.Exec(ctx, fmt.Sprintf(`
		CREATE SCHEMA %[1]s;
		CREATE TABLE %[1]s.connections (
			id INTEGER PRIMARY KEY,
			name BOOLEAN NOT NULL,
			cluster_id INTEGER
		);
		INSERT INTO %[1]s.connections VALUES (1, TRUE, 10), (2, FALSE, 10);
	`, schemaIdent)); err != nil {
		t.Fatalf("create scratch schema: %v", err)
	}
	// Registered with defer rather than t.Cleanup so the drop runs
	// while the pool is still open: the fixture's own cleanup closes
	// the pool, and deferred calls unwind before t.Cleanup functions.
	defer func() {
		//nosemgrep: go_sql_rule-concat-sqli -- test-only DDL; same sanitized schema identifier as the CREATE above
		if _, err := pool.Exec(context.Background(),
			fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schemaIdent),
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
