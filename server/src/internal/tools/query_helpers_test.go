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
	"testing"
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
			wantModified: "SELECT * FROM users LIMIT 101",
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
			wantModified:  "SELECT * FROM users LIMIT 101 OFFSET 50",
			wantHadLimit:  false,
			wantHadOffset: false,
		},
		{
			name:          "select with existing offset",
			query:         "SELECT * FROM users OFFSET 10",
			limit:         100,
			offset:        50,
			wantModified:  "SELECT * FROM users OFFSET 10 LIMIT 101",
			wantHadLimit:  false,
			wantHadOffset: true,
		},
		{
			name:         "with cte gets limit",
			query:        "WITH cte AS (SELECT 1) SELECT * FROM cte",
			limit:        100,
			offset:       0,
			wantModified: "WITH cte AS (SELECT 1) SELECT * FROM cte LIMIT 101",
			wantHadLimit: false,
		},
		{
			name:         "explain gets limit",
			query:        "EXPLAIN SELECT * FROM users",
			limit:        100,
			offset:       0,
			wantModified: "EXPLAIN SELECT * FROM users LIMIT 101",
			wantHadLimit: false,
		},
		{
			name:         "show gets limit",
			query:        "SHOW work_mem",
			limit:        100,
			offset:       0,
			wantModified: "SHOW work_mem LIMIT 101",
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
