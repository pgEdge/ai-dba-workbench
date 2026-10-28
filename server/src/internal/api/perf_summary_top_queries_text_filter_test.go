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
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// setTopQueryText replaces the recorded text of one statement in the
// fixture. When latestOnly is set only its most recent sample changes,
// leaving the baseline sample with the fixture's text, which is what a
// statement whose normalised text changed inside the window looks like.
func setTopQueryText(
	t *testing.T,
	pool *pgxpool.Pool,
	queryID int64,
	text string,
	latestOnly bool,
) {
	t.Helper()
	sql := `UPDATE metrics.pg_stat_statements SET query = $3
        WHERE connection_id = $1 AND queryid = $2`
	if latestOnly {
		sql += ` AND collected_at = (
            SELECT MAX(collected_at) FROM metrics.pg_stat_statements
            WHERE connection_id = $1 AND queryid = $2)`
	}
	if _, err := pool.Exec(context.Background(), sql, topQueriesConnID,
		queryID, text); err != nil {
		t.Fatalf("set query text of %d: %v", queryID, err)
	}
}

// TestTopQueries_QueryTextFilter covers the query_text filter (GitHub issue
// #757): it matches a case-insensitive plain substring, with no LIKE
// wildcards, against the text the page shows, as masked in SQL, and both
// X-Total-Count and paging reflect the filtered set.
func TestTopQueries_QueryTextFilter(t *testing.T) {
	h, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()
	seedTopQueriesFixture(t, pool)

	// 1002 and 1003 differ only where LIKE would see wildcards, so a term
	// read as a pattern would match both.
	setTopQueryText(t, pool, 1002, "SELECT '50%_off'", false)
	setTopQueryText(t, pool, 1003, "SELECT '50xyoff'", false)
	// 1004's secret is in the stored text only: RedactedText masks it in
	// SQL, and RedactStatementText masks it in the response.
	setTopQueryText(t, pool, 1004,
		"ALTER ROLE app PASSWORD 'example-secret'", false)
	// 1001's baseline sample carries text its latest sample does not, and
	// the page shows the latest, so the older text must not match.
	setTopQueryText(t, pool, 1001, "SELECT 1 /* baseline-only */", false)
	setTopQueryText(t, pool, 1001, "SELECT 1", true)
	// 1005 has no captured text at all, so it can match nothing.
	clearQueryText(t, pool, 1005)

	tests := []struct {
		name      string
		params    string
		wantIDs   []string
		wantTotal string
	}{
		{
			name:      "case-insensitive substring",
			params:    "query_text=select+1",
			wantIDs:   []string{"1001"},
			wantTotal: "1",
		},
		{
			name:      "percent and underscore match literally",
			params:    "query_text=" + url.QueryEscape("50%_off"),
			wantIDs:   []string{"1002"},
			wantTotal: "1",
		},
		{
			name:      "secret in stored text only does not match",
			params:    "query_text=example-secret",
			wantIDs:   []string{},
			wantTotal: "0",
		},
		{
			name:      "unmasked part of a masked statement matches",
			params:    "query_text=" + url.QueryEscape("alter role app password"),
			wantIDs:   []string{"1004"},
			wantTotal: "1",
		},
		{
			name:      "older text in the window does not match",
			params:    "query_text=baseline-only",
			wantIDs:   []string{},
			wantTotal: "0",
		},
		{
			name:      "statement with no text never matches",
			params:    "query_text=" + url.QueryEscape("SELECT 5"),
			wantIDs:   []string{},
			wantTotal: "0",
		},
		{
			// 1001, 1002, 1003 and 1006 match; 1004's text has no
			// SELECT and 1005's has no text. The count reports all four
			// though the page holds two.
			name:      "count and paging reflect the filtered set",
			params:    "query_text=select&limit=2&offset=1",
			wantIDs:   []string{"1002", "1003"},
			wantTotal: "4",
		},
		{
			name:      "combined with database_name",
			params:    "query_text=select&database_name=beta",
			wantIDs:   []string{"1006"},
			wantTotal: "1",
		},
		{
			name:      "combined with queryid",
			params:    "query_text=select&queryid=1003",
			wantIDs:   []string{"1003"},
			wantTotal: "1",
		},
		{
			name:      "empty term applies no filter",
			params:    "query_text=",
			wantIDs:   []string{"1001", "1002", "1003", "1004", "1005", "1006"},
			wantTotal: "6",
		},
		{
			name:      "term at the length limit is accepted",
			params:    "query_text=" + strings.Repeat("a", maxTopQueryTextLength),
			wantIDs:   []string{},
			wantTotal: "0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows, total := decodeTopQueries(t, callTopQueries(t, h,
				"connection_id=4242&"+tc.params))
			if got := queryIDs(rows); !reflect.DeepEqual(got, tc.wantIDs) {
				t.Errorf("query IDs = %v, want %v", got, tc.wantIDs)
			}
			if total != tc.wantTotal {
				t.Errorf("X-Total-Count = %q, want %q", total, tc.wantTotal)
			}
			for _, row := range rows {
				if strings.Contains(row.Query, "example-secret") {
					t.Errorf("row %s returned the secret: %q", row.QueryID,
						row.Query)
				}
			}
		})
	}
}

// TestTopQueries_InvalidQueryText checks that an unusable query_text is
// rejected with a 400 before any query runs, so that PostgreSQL's refusal
// of a malformed text parameter is not reported as a 500.
func TestTopQueries_InvalidQueryText(t *testing.T) {
	h, _, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()

	tests := []struct {
		name    string
		value   string
		wantMsg string
	}{
		{"over the length limit",
			strings.Repeat("a", maxTopQueryTextLength+1),
			"query_text must be 1000 characters or less"},
		{"over the limit in multi-byte characters",
			strings.Repeat("é", maxTopQueryTextLength+1),
			"query_text must be 1000 characters or less"},
		{"invalid UTF-8", "SELECT \xff",
			"Invalid query_text: must be valid UTF-8 without NUL characters"},
		{"NUL byte", "SELECT\x00 1",
			"Invalid query_text: must be valid UTF-8 without NUL characters"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := callTopQueries(t, h,
				"connection_id=4242&query_text="+url.QueryEscape(tc.value))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", rec.Code,
					rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantMsg) {
				t.Errorf("body = %s, want it to contain %q",
					rec.Body.String(), tc.wantMsg)
			}
			if got := rec.Header().Get("X-Total-Count"); got != "" {
				t.Errorf("X-Total-Count = %q on error path, want unset", got)
			}
		})
	}
}

// TestValidateTopQueryText pins the query_text validation without a
// database: a term of up to maxTopQueryTextLength characters, counted as
// runes rather than bytes, is accepted, and the empty term means no
// filter.
func TestValidateTopQueryText(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"empty", "", true},
		{"plain", "select 1", true},
		{"wildcards", "50%_off", true},
		{"at the limit in multi-byte characters",
			strings.Repeat("é", maxTopQueryTextLength), true},
		{"over the limit", strings.Repeat("a", maxTopQueryTextLength+1), false},
		{"invalid UTF-8", "\xc3", false},
		{"NUL byte", "a\x00", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			if got := validateTopQueryText(rec, tc.value); got != tc.want {
				t.Fatalf("validateTopQueryText(%q) = %v, want %v", tc.value,
					got, tc.want)
			}
			if !tc.want && rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
		})
	}
}
