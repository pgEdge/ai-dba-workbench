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
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestInjectLimitOffset_SelectQueries(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		limit         int
		offset        int
		wantModified  string
		wantHadLimit  bool
		wantHadOffset bool
	}{
		{
			name:         "select with limit injection",
			query:        "SELECT * FROM users",
			limit:        100,
			offset:       0,
			wantModified: "SELECT * FROM users\nLIMIT 101",
			wantHadLimit: false,
		},
		{
			name:         "select with existing limit",
			query:        "SELECT * FROM users LIMIT 10",
			limit:        100,
			offset:       0,
			wantModified: "SELECT * FROM users LIMIT 10",
			wantHadLimit: true,
		},
		{
			name:          "select with offset injection",
			query:         "SELECT * FROM users",
			limit:         100,
			offset:        50,
			wantModified:  "SELECT * FROM users\nLIMIT 101\nOFFSET 50",
			wantHadLimit:  false,
			wantHadOffset: false,
		},
		{
			name:          "select with existing offset",
			query:         "SELECT * FROM users OFFSET 10",
			limit:         100,
			offset:        50,
			wantModified:  "SELECT * FROM users OFFSET 10\nLIMIT 101",
			wantHadLimit:  false,
			wantHadOffset: true,
		},
		{
			name:         "with cte gets limit",
			query:        "WITH cte AS (SELECT 1) SELECT * FROM cte",
			limit:        100,
			offset:       0,
			wantModified: "WITH cte AS (SELECT 1) SELECT * FROM cte\nLIMIT 101",
			wantHadLimit: false,
		},
		{
			name:         "explain gets limit",
			query:        "EXPLAIN SELECT * FROM users",
			limit:        100,
			offset:       0,
			wantModified: "EXPLAIN SELECT * FROM users\nLIMIT 101",
			wantHadLimit: false,
		},
		{
			name:         "show gets limit",
			query:        "SHOW work_mem",
			limit:        100,
			offset:       0,
			wantModified: "SHOW work_mem\nLIMIT 101",
			wantHadLimit: false,
		},
		{
			name:         "trailing line comment",
			query:        "SELECT * FROM users -- every user",
			limit:        100,
			offset:       50,
			wantModified: "SELECT * FROM users -- every user\nLIMIT 101\nOFFSET 50",
			wantHadLimit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modified, hadLimit, hadOffset := injectLimitOffset(tt.query, tt.limit, tt.offset)
			if modified != tt.wantModified {
				t.Errorf("injectLimitOffset() modified = %q, want %q", modified, tt.wantModified)
			}
			if hadLimit != tt.wantHadLimit {
				t.Errorf("injectLimitOffset() hadLimit = %v, want %v", hadLimit, tt.wantHadLimit)
			}
			if hadOffset != tt.wantHadOffset {
				t.Errorf("injectLimitOffset() hadOffset = %v, want %v", hadOffset, tt.wantHadOffset)
			}
		})
	}
}

func TestInjectLimitOffset_NonSelectQueries(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{"insert", "INSERT INTO users (name) VALUES ('test')"},
		{"update", "UPDATE users SET name = 'test' WHERE id = 1"},
		{"delete", "DELETE FROM users WHERE id = 1"},
		{"create table", "CREATE TABLE test_rbac (id INT)"},
		{"alter table", "ALTER TABLE users ADD COLUMN email TEXT"},
		{"drop table", "DROP TABLE test_rbac"},
		{"truncate", "TRUNCATE users"},
		{"grant", "GRANT SELECT ON users TO reader"},
		{"create index", "CREATE INDEX idx_name ON users(name)"},
		{"comment leading insert", "-- comment\nINSERT INTO t VALUES (1)"},
		{"block comment leading create", "/* comment */ CREATE TABLE t (id INT)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modified, hadLimit, hadOffset := injectLimitOffset(tt.query, 100, 50)
			if modified != tt.query {
				t.Errorf("injectLimitOffset() should not modify non-SELECT query, got %q, want %q", modified, tt.query)
			}
			if hadLimit {
				t.Error("injectLimitOffset() hadLimit should be false for non-SELECT query")
			}
			if hadOffset {
				t.Error("injectLimitOffset() hadOffset should be false for non-SELECT query")
			}
		})
	}
}

func TestParseLimitOffset(t *testing.T) {
	tests := []struct {
		name       string
		args       map[string]any
		wantLimit  int
		wantOffset int
	}{
		{
			name:       "defaults",
			args:       map[string]any{},
			wantLimit:  100,
			wantOffset: 0,
		},
		{
			name:       "float64 limit",
			args:       map[string]any{"limit": float64(50)},
			wantLimit:  50,
			wantOffset: 0,
		},
		{
			name:       "int limit",
			args:       map[string]any{"limit": 50},
			wantLimit:  50,
			wantOffset: 0,
		},
		{
			name:       "limit below minimum",
			args:       map[string]any{"limit": float64(-5)},
			wantLimit:  1,
			wantOffset: 0,
		},
		{
			name:       "limit above maximum",
			args:       map[string]any{"limit": float64(5000)},
			wantLimit:  1000,
			wantOffset: 0,
		},
		{
			name:       "with offset",
			args:       map[string]any{"offset": float64(200)},
			wantLimit:  100,
			wantOffset: 200,
		},
		{
			name:       "negative offset clamped to zero",
			args:       map[string]any{"offset": float64(-10)},
			wantLimit:  100,
			wantOffset: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limit, offset := parseLimitOffset(tt.args)
			if limit != tt.wantLimit {
				t.Errorf("parseLimitOffset() limit = %d, want %d", limit, tt.wantLimit)
			}
			if offset != tt.wantOffset {
				t.Errorf("parseLimitOffset() offset = %d, want %d", offset, tt.wantOffset)
			}
		})
	}
}

func TestSafeToolQueryError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"pg error", &pgconn.PgError{Message: "relation missing"}, "p: relation missing"},
		{"pg error with detail", &pgconn.PgError{Message: "bad", Detail: "why"}, "p: bad (why)"},
		{"wrapped pg error", fmt.Errorf("wrap: %w", &pgconn.PgError{Message: "inner"}), "p: inner"},
		{"deadline", context.DeadlineExceeded, "p: query timed out"},
		{"canceled", fmt.Errorf("x: %w", context.Canceled), "p: query was canceled"},
		{"connection", errors.New("dial tcp 192.0.2.1:5432: Connection Refused"),
			"p: connection error; the database may be unreachable"},
		{"placeholders", errors.New("expected 1 arguments, got 0"), "p: query contains parameter placeholders"},
		{"other", errors.New("secret internal detail"), "p: an internal error occurred"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := safeToolQueryError("p: ", tc.err)
			if !strings.HasPrefix(got, tc.want) {
				t.Errorf("got %q, want prefix %q", got, tc.want)
			}
			if strings.Contains(got, "secret") || strings.Contains(got, "192.0.2.1") {
				t.Errorf("message leaks the underlying error: %q", got)
			}
		})
	}
}

func TestFormatPaginatedResults(t *testing.T) {
	rows := &queryResult{Rows: [][]any{{1}, {2}}, ResultsTSV: "n\n1\n2"}
	truncated := &queryResult{Rows: [][]any{{1}, {2}}, ResultsTSV: "n\n1\n2", WasTruncated: true}
	cases := []struct {
		name   string
		qr     *queryResult
		offset int
		want   string
	}{
		{"first page", rows, 0, "Results (2 rows):"},
		{"first page truncated", truncated, 0,
			"Results (2 rows shown, more available - use offset=2 for next page or increase limit):"},
		{"later page", rows, 4, "Results (rows 5-6):"},
		{"later page truncated", truncated, 4, "Results (rows 5-6, more available - use offset=6 for next page):"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sb strings.Builder
			formatPaginatedResults(&sb, tc.qr, "SELECT 1", 2, tc.offset, " or increase limit")
			got := sb.String()
			if !strings.HasPrefix(got, "SQL Query:\nSELECT 1\n\n") || !strings.Contains(got, tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExecuteQueryIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	t.Cleanup(cleanup)
	ctx := context.Background()
	series := "SELECT g AS n FROM generate_series(1, $1::int) AS g"

	qr, errResp := executeReadOnlyQuery(ctx, pool, series+" LIMIT 4", 3, "", 5)
	if errResp != nil {
		t.Fatalf("query failed: %+v", errResp)
	}
	if !qr.WasTruncated || len(qr.Rows) != 3 || qr.ColumnNames[0] != "n" {
		t.Errorf("expected three rows and truncation, got %+v", qr)
	}

	qr, errResp = executeReadOnlyQuery(ctx, pool, series, 3, "", 5)
	if errResp != nil || !qr.WasTruncated || len(qr.Rows) != 3 {
		t.Errorf("the cap should apply without an injected limit: %+v %+v", qr, errResp)
	}

	qr, errResp = executeReadOnlyQuery(ctx, pool, series, 0, "", 5)
	if errResp != nil || qr.WasTruncated || len(qr.Rows) != 5 {
		t.Errorf("a zero limit should not cap rows: %+v %+v", qr, errResp)
	}

	_, errResp = executeReadOnlyQuery(ctx, pool, "SELECT * FROM no_such_table_566", 0, "Query failed: ")
	if errResp == nil || !strings.Contains(errResp.Content[0].Text, "Query failed: ") ||
		!strings.Contains(errResp.Content[0].Text, "no_such_table_566") {
		t.Errorf("expected the server's error message, got %+v", errResp)
	}

	_, errResp = executeReadOnlyQuery(ctx, pool, "CREATE TABLE ro_test_566 (id int)", 0, "")
	if errResp == nil || !strings.Contains(errResp.Content[0].Text, "read-only") {
		t.Errorf("a read-only transaction should refuse a write, got %+v", errResp)
	}

	_, errResp = executeReadOnlyQuery(ctx, pool, "SELECT 1/(n - 3) FROM generate_series(1, 5) AS n", 0, "")
	if errResp == nil || !strings.Contains(errResp.Content[0].Text, "Error iterating rows") {
		t.Errorf("an error mid-result should be reported, got %+v", errResp)
	}
}
