/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// findTopQueryRow returns the row for a query ID, failing the test when it
// is absent, so the assertions below read as statements about one statement
// rather than about a position in the page.
func findTopQueryRow(
	t *testing.T,
	rows []TopQueryRow,
	queryID string,
) TopQueryRow {
	t.Helper()
	for _, row := range rows {
		if row.QueryID == queryID {
			return row
		}
	}
	t.Fatalf("query %s missing from the result: %#v", queryID, rows)
	return TopQueryRow{}
}

// TestTopQueries_QueryTextFromLatestSampleInWindow covers the lookup that
// resolves the query text for the rows on a page. The text is no longer
// carried through the delta aggregation, so these are the properties that
// keep it correct: the text reported is the one from the most recent sample
// inside the window, and the lookup matches the database name recorded on
// the sample rather than the name the response resolves through
// pg_stat_activity.
func TestTopQueries_QueryTextFromLatestSampleInWindow(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()
	seedTopQueriesFixture(t, pool)

	// Statement 1001 was recorded with one text at the baseline sample and
	// a different one at the latest, which is what a statement whose
	// normalised text changed inside the window looks like.
	if _, err := pool.Exec(context.Background(), `
		UPDATE metrics.pg_stat_statements
		SET query = 'SELECT 1 /* rewritten */'
		WHERE connection_id = $1
		  AND queryid = 1001
		  AND collected_at = (
		      SELECT MAX(collected_at)
		      FROM metrics.pg_stat_statements
		      WHERE connection_id = $1 AND queryid = 1001)
	`, topQueriesConnID); err != nil {
		t.Fatalf("rewrite the latest query text: %v", err)
	}

	rows, _ := decodeTopQueries(t, callTopQueries(t, h, "connection_id=4242"))

	if got := findTopQueryRow(t, rows, "1001").Query; got !=
		"SELECT 1 /* rewritten */" {
		t.Errorf("query text = %q, want the text of the latest sample", got)
	}

	// Statement 1005 carries a stale database_name on the sample and
	// resolves to "beta" through pg_stat_activity. The text lookup has to
	// use the stale name the row was stored under, or it would find
	// nothing and report an empty query.
	row := findTopQueryRow(t, rows, "1005")
	if row.DatabaseName != "beta" {
		t.Errorf("resolved database_name = %q, want \"beta\"",
			row.DatabaseName)
	}
	if row.Query != "SELECT 5" {
		t.Errorf("query text = %q, want \"SELECT 5\"", row.Query)
	}
}

// TestTopQueries_NullQueryTextIsReportedEmpty confirms that a statement
// whose text the collector never captured still appears on the page, with
// an empty query rather than being dropped. The text now arrives through a
// LEFT JOIN, so this pins the "left" part of it.
func TestTopQueries_NullQueryTextIsReportedEmpty(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()
	seedTopQueriesFixture(t, pool)
	clearQueryText(t, pool, 1002)

	rows, total := decodeTopQueries(t,
		callTopQueries(t, h, "connection_id=4242"))

	if total != "6" {
		t.Errorf("X-Total-Count = %q, want \"6\"", total)
	}
	row := findTopQueryRow(t, rows, "1002")
	if row.Query != "" {
		t.Errorf("query text = %q, want it empty", row.Query)
	}
	// The row is still a real measurement; only its text is missing.
	if row.Calls != 20 {
		t.Errorf("calls = %d, want 20", row.Calls)
	}
}

// clearQueryText removes the recorded text from every sample of one
// statement, which the fixture schema otherwise forbids because it declares
// the column NOT NULL. metrics.pg_stat_statements itself allows NULL there.
func clearQueryText(t *testing.T, pool *pgxpool.Pool, queryID int64) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		ALTER TABLE metrics.pg_stat_statements
			ALTER COLUMN query DROP NOT NULL
	`); err != nil {
		t.Fatalf("allow NULL query text: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE metrics.pg_stat_statements
		SET query = NULL
		WHERE connection_id = $1 AND queryid = $2
	`, topQueriesConnID, queryID); err != nil {
		t.Fatalf("clear query text: %v", err)
	}
}
