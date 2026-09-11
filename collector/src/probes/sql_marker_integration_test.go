/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Integration tests for the Workbench-internal SQL marker (issue #364).
// These verify against a live PostgreSQL instance that the marker
// survives into pg_stat_statements, which is the whole point of putting
// it after the leading keyword rather than in front of the statement.
package probes

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/pgedge/ai-workbench/pkg/sqlmarker"
)

// TestStoreMetrics_MarkerSurvivesInPgStatStatements is the end-to-end
// proof of the fix: the collector's bulk metric INSERT is recorded by
// pg_stat_statements with the marker intact, so the server's Top Queries
// filter can exclude it.
//
// The test skips where pg_stat_statements is not loaded; the tagging
// itself is asserted unconditionally, and without a database, by
// TestBuildMetricsInsert_Tagged and its neighbors in sql_marker_test.go.
func TestStoreMetrics_MarkerSurvivesInPgStatStatements(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	if _, err := conn.Exec(ctx,
		"CREATE EXTENSION IF NOT EXISTS pg_stat_statements"); err != nil {
		t.Skipf("skipping end-to-end marker check: the "+
			"pg_stat_statements extension cannot be created: %v", err)
	}
	requirePgStatStatementsReadable(t, conn)
	// A unique table name keeps this test independent of the shared
	// metrics tables used by the other integration tests, and gives the
	// statement a queryid of its own: pg_stat_statements normalizes
	// literals into $n placeholders and ignores both comments and column
	// aliases when computing the queryid, so the relation name is the
	// only reliable discriminator. The statistics are deliberately not
	// reset, because that would discard the whole instance's history.
	table := fmt.Sprintf("marker_probe_%d", time.Now().UnixNano())
	ident := pgx.Identifier{"metrics", table}.Sanitize()
	// The relation name is generated from a timestamp above and quoted
	// with pgx.Identifier; CREATE TABLE cannot name its relation with a
	// bind parameter.
	//nosemgrep: go_sql_rule-concat-sqli -- test-only DDL; timestamp-derived name sanitized by pgx.Identifier
	if _, err := conn.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE %s (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL
		)`, ident)); err != nil {
		t.Fatalf("create test table: %v", err)
	}
	t.Cleanup(func() {
		//nosemgrep: go_sql_rule-concat-sqli -- test-only DDL; same sanitized identifier as the CREATE above
		if _, err := conn.Exec(context.Background(),
			fmt.Sprintf("DROP TABLE IF EXISTS %s", ident),
		); err != nil {
			t.Logf("cleanup: drop %s: %v", ident, err)
		}
	})

	err := StoreMetrics(ctx, conn, table,
		[]string{"connection_id", "collected_at"},
		[][]any{{1, time.Now().UTC()}})
	if err != nil {
		t.Fatalf("StoreMetrics() error = %v", err)
	}

	var count int
	// The SQL is a fixed literal; the table name and the marker are
	// bound as $1 and $2 rather than concatenated into it.
	//nosemgrep: go_sql_rule-concat-sqli -- fixed SQL literal; table name and marker bound as $1 and $2
	err = conn.QueryRow(ctx, `
        SELECT count(*)
        FROM pg_stat_statements
        WHERE query LIKE '%INSERT%' || $1 || '%'
          AND query LIKE '%' || $2 || '%'
    `, table, sqlmarker.Marker).Scan(&count)
	if err != nil {
		t.Fatalf("read pg_stat_statements: %v", err)
	}
	if count == 0 {
		// Dump what was recorded to make a regression obvious. The SQL
		// is a fixed literal and the table name is bound as $1.
		//nosemgrep: go_sql_rule-concat-sqli -- fixed SQL literal; table name bound as $1
		rows, qerr := conn.Query(ctx, `
            SELECT query FROM pg_stat_statements
            WHERE query LIKE '%' || $1 || '%'
        `, table)
		if qerr == nil {
			defer rows.Close()
			for rows.Next() {
				var q string
				if scanErr := rows.Scan(&q); scanErr == nil {
					t.Logf("recorded: %s", q)
				}
			}
		}
		t.Errorf("no pg_stat_statements entry for the INSERT into "+
			"metrics.%s carries %q", table, sqlmarker.Marker)
	}
}

// TestStoreMetrics_CommitError covers the commit-failure branch of
// StoreMetrics using a deferred unique constraint: the INSERT succeeds
// and the COMMIT fails.
func TestStoreMetrics_CommitError(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	table := fmt.Sprintf("marker_commit_%d", time.Now().UnixNano())
	ident := pgx.Identifier{"metrics", table}.Sanitize()
	//nosemgrep: go_sql_rule-concat-sqli -- test-only DDL; timestamp-derived name sanitized by pgx.Identifier
	if _, err := conn.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE %s (
			connection_id INTEGER NOT NULL,
			collected_at TIMESTAMPTZ NOT NULL,
			UNIQUE (connection_id) DEFERRABLE INITIALLY DEFERRED
		)`, ident)); err != nil {
		t.Fatalf("create test table: %v", err)
	}
	t.Cleanup(func() {
		//nosemgrep: go_sql_rule-concat-sqli -- test-only DDL; same sanitized identifier as the CREATE above
		if _, err := conn.Exec(context.Background(),
			fmt.Sprintf("DROP TABLE IF EXISTS %s", ident),
		); err != nil {
			t.Logf("cleanup: drop %s: %v", ident, err)
		}
	})

	now := time.Now().UTC()
	err := StoreMetrics(ctx, conn, table,
		[]string{"connection_id", "collected_at"},
		[][]any{{1, now}, {1, now}})
	if err == nil {
		t.Fatal("expected a commit error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to commit transaction") {
		t.Errorf("error = %v, want a commit failure", err)
	}
}

// TestGetLastCollectionTime_WithRow covers the branch that returns a
// real timestamp, and confirms the tagged query still works against a
// live datastore.
func TestGetLastCollectionTime_WithRow(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	const connectionID = 987654
	collectedAt := time.Now().UTC().Truncate(time.Millisecond)

	if err := EnsurePartition(
		ctx, conn, "pg_stat_activity", collectedAt); err != nil {
		t.Fatalf("EnsurePartition() error = %v", err)
	}
	if _, err := conn.Exec(ctx, `
        INSERT INTO metrics.pg_stat_activity (connection_id, collected_at)
        VALUES ($1, $2)
    `, connectionID, collectedAt); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	t.Cleanup(func() {
		if _, err := conn.Exec(context.Background(),
			"DELETE FROM metrics.pg_stat_activity WHERE connection_id = $1",
			connectionID); err != nil {
			t.Logf("cleanup: delete seeded rows: %v", err)
		}
	})

	got, err := GetLastCollectionTime(
		ctx, conn, "pg_stat_activity", connectionID)
	if err != nil {
		t.Fatalf("GetLastCollectionTime() error = %v", err)
	}
	if !got.Equal(collectedAt) {
		t.Errorf("got %v, want %v", got, collectedAt)
	}
}

// TestGetLastCollectionTime_QueryError covers the error branch for a
// failure that is not a missing table, by breaking the connection first.
func TestGetLastCollectionTime_QueryError(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	ctx := context.Background()

	breakConn(t, conn)

	_, err := GetLastCollectionTime(ctx, conn, "pg_stat_activity", 1)
	if err == nil {
		t.Fatal("expected an error from a closed connection, got nil")
	}
	if !strings.Contains(
		err.Error(), "failed to query last collection time") {
		t.Errorf("error = %v, want a query failure", err)
	}
}
