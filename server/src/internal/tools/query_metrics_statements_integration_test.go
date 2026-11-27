/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package tools

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

// bucketValue matches a query_metrics row that holds a value.
var bucketValue = regexp.MustCompile(`Z\t\d`)

// TestQueryMetricsStatementDatabaseIntegration checks that query_metrics
// filters pg_stat_statements by the database each statement ran in, and
// counts each reading once, whichever database it was collected through
// (GitHub issue #762). The older collection holds a copy of each reading
// per probing database; the newer one was read through another database.
func TestQueryMetricsStatementDatabaseIntegration(t *testing.T) {
	f := newMarkedFixture(t)
	ctx := context.Background()
	for _, tbl := range collectorShapeTables {
		if _, err := f.pool.Exec(ctx, tbl.drop); err != nil {
			t.Fatalf("%s: %v", tbl.drop, err)
		}
		if _, err := f.pool.Exec(ctx, tbl.create); err != nil {
			t.Fatalf("%s: %v", tbl.create, err)
		}
		drop := tbl.drop
		t.Cleanup(func() {
			if _, err := f.pool.Exec(context.Background(), drop); err != nil {
				t.Logf("%s: %v", drop, err)
			}
		})
	}
	// One transaction, so now() is the same instant in both tables and
	// the name lookup sees pg_stat_database at the latest collection.
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, sql := range []string{
		`INSERT INTO metrics.pg_stat_statements (connection_id, collected_at, queryid, userid, dbid,
            database_name, query, calls) VALUES
            ($1, now() - interval '10 minutes', 1, 10, 100, 'alpha', 'SELECT 1', 10),
            ($1, now() - interval '10 minutes', 1, 10, 100, 'beta', 'SELECT 1', 10),
            ($1, now() - interval '10 minutes', 2, 10, 200, 'alpha', 'SELECT 2', 1000),
            ($1, now() - interval '10 minutes', 2, 10, 200, 'beta', 'SELECT 2', 1000),
            ($1, now() - interval '5 minutes', 1, 10, 100, 'postgres', 'SELECT 1', 20),
            ($1, now() - interval '5 minutes', 2, 10, 200, 'postgres', 'SELECT 2', 2000)`,
		`INSERT INTO metrics.pg_stat_database (connection_id, collected_at, database_name, datid, datname) VALUES
            ($1, now() - interval '5 minutes', 'alpha', 100, 'alpha'),
            ($1, now() - interval '5 minutes', 'beta', 200, 'beta')`,
	} {
		if _, err := tx.Exec(ctx, sql, f.live); err != nil {
			t.Fatalf("seed: %v\n%s", err, sql)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	tool := asSuperuser(QueryMetricsTool(f.pool, testRBACChecker(t), nil))
	args := func(db string) map[string]any {
		a := map[string]any{"connection_id": float64(f.live), "probe_name": "pg_stat_statements",
			"metrics": "calls", "aggregation": "sum", "buckets": float64(1), "time_start": "1h"}
		if db != "" {
			a["database_name"] = db
		}
		return a
	}
	for _, tt := range []struct{ db, want string }{
		// alpha's statement once per reading: 10 then 20.
		{"alpha", "\t30\n"},
		// Every statement, still once per reading.
		{"", "\t3030\n"},
		// No statement ran in a database with this name, so every bucket
		// is empty.
		{"gamma", ""},
	} {
		text, isErr := toolText(t, tool, args(tt.db))
		if tt.want == "" {
			if isErr || bucketValue.MatchString(text) {
				t.Errorf("query_metrics database_name=%q = %q (error %v), want empty buckets", tt.db, text, isErr)
			}
			continue
		}
		if isErr || !strings.Contains(text, tt.want) {
			t.Errorf("query_metrics database_name=%q = %q (error %v), want %q", tt.db, text, isErr, tt.want)
		}
	}

	if _, err := f.pool.Exec(ctx, `DROP TABLE metrics.pg_stat_database`); err != nil {
		t.Fatalf("drop pg_stat_database: %v", err)
	}
	text, isErr := toolText(t, tool, args("alpha"))
	if !isErr || !strings.Contains(text, "Failed to resolve database_name") {
		t.Errorf("query_metrics without pg_stat_database = %q (error %v), want a resolve error", text, isErr)
	}
}
