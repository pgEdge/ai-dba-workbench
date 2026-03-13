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

import "testing"

func TestIsSelectQuery(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want bool
	}{
		// SELECT variants
		{"simple select", "SELECT 1", true},
		{"select with columns", "SELECT id, name FROM users", true},
		{"select lowercase", "select * from users", true},
		{"select mixed case", "SeLeCt * FROM users", true},
		{"select with leading spaces", "   SELECT 1", true},
		{"select with leading tabs", "\t\tSELECT 1", true},
		{"select with leading newlines", "\n\nSELECT 1", true},

		// WITH (CTE)
		{"with cte", "WITH cte AS (SELECT 1) SELECT * FROM cte", true},
		{"with lowercase", "with cte as (select 1) select * from cte", true},
		{"with leading whitespace", "  WITH cte AS (SELECT 1) SELECT * FROM cte", true},

		// EXPLAIN
		{"explain select", "EXPLAIN SELECT * FROM users", true},
		{"explain analyze", "EXPLAIN ANALYZE SELECT * FROM users", true},
		{"explain lowercase", "explain select 1", true},

		// SHOW
		{"show", "SHOW work_mem", true},
		{"show all", "SHOW ALL", true},
		{"show lowercase", "show work_mem", true},

		// VALUES
		{"values", "VALUES (1, 'a'), (2, 'b')", true},
		{"values lowercase", "values (1, 'a')", true},

		// TABLE
		{"table command", "TABLE users", true},
		{"table lowercase", "table users", true},

		// Non-SELECT statements
		{"insert", "INSERT INTO users (name) VALUES ('test')", false},
		{"update", "UPDATE users SET name = 'test'", false},
		{"delete", "DELETE FROM users WHERE id = 1", false},
		{"create table", "CREATE TABLE test (id INT)", false},
		{"alter table", "ALTER TABLE users ADD COLUMN email TEXT", false},
		{"drop table", "DROP TABLE users", false},
		{"truncate", "TRUNCATE users", false},
		{"grant", "GRANT SELECT ON users TO reader", false},
		{"revoke", "REVOKE SELECT ON users FROM reader", false},
		{"create index", "CREATE INDEX idx_name ON users(name)", false},
		{"vacuum", "VACUUM users", false},
		{"analyze", "ANALYZE users", false},

		// Leading comments
		{"line comment then select", "-- comment\nSELECT 1", true},
		{"multiple line comments then select",
			"-- comment 1\n-- comment 2\nSELECT 1", true},
		{"block comment then select", "/* comment */ SELECT 1", true},
		{"nested block comment then select",
			"/* outer /* inner */ still outer */ SELECT 1", true},
		{"line comment then insert", "-- comment\nINSERT INTO t VALUES (1)", false},
		{"block comment then create", "/* comment */ CREATE TABLE t (id INT)", false},
		{"mixed comments then select",
			"-- line\n/* block */\n  SELECT 1", true},
		{"block comment with whitespace then select",
			"  /* comment */  SELECT 1", true},

		// Defense-in-depth: these are classified as SELECT/read but
		// the read-only transaction prevents any actual writes.
		{"multi-statement starting with SELECT",
			"SELECT 1; DROP TABLE foo", true},
		{"SELECT INTO blocked by read-only tx",
			"SELECT * INTO new_table FROM users", true},
		{"writable CTE blocked by read-only tx",
			"WITH cte AS (DELETE FROM users RETURNING *) SELECT * FROM cte", true},
		{"EXPLAIN ANALYZE write blocked by read-only tx",
			"EXPLAIN ANALYZE DELETE FROM users", true},

		// Correctly classified as write operations
		{"DO block is write", "DO $$ BEGIN EXECUTE 'DROP TABLE users'; END $$", false},
		{"COPY TO is write", "COPY users TO '/tmp/out'", false},
		{"COPY FROM is write", "COPY users FROM '/tmp/in'", false},

		// Edge cases
		{"empty string", "", false},
		{"only whitespace", "   \n\t  ", false},
		{"only comments", "-- just a comment\n/* another */", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSelectQuery(tt.sql)
			if got != tt.want {
				t.Errorf("isSelectQuery(%q) = %v, want %v", tt.sql, got, tt.want)
			}
		})
	}
}

func TestFirstSQLKeyword(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{"simple keyword", "SELECT 1", "SELECT"},
		{"lowercase", "select 1", "SELECT"},
		{"leading whitespace", "  INSERT INTO t", "INSERT"},
		{"line comment", "-- comment\nUPDATE t SET x=1", "UPDATE"},
		{"block comment", "/* comment */ DELETE FROM t", "DELETE"},
		{"empty", "", ""},
		{"only whitespace", "   ", ""},
		{"digit-leading token", "123abc", "123ABC"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstSQLKeyword(tt.sql)
			if got != tt.want {
				t.Errorf("firstSQLKeyword(%q) = %q, want %q", tt.sql, got, tt.want)
			}
		})
	}
}

func TestHasLimitClause(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want bool
	}{
		{"no limit", "SELECT * FROM users", false},
		{"with limit", "SELECT * FROM users LIMIT 10", true},
		{"limit lowercase", "SELECT * FROM users limit 10", true},
		{"column name credit_limit", "SELECT credit_limit FROM accounts", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasLimitClause(tt.sql)
			if got != tt.want {
				t.Errorf("hasLimitClause(%q) = %v, want %v", tt.sql, got, tt.want)
			}
		})
	}
}

func TestHasOffsetClause(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want bool
	}{
		{"no offset", "SELECT * FROM users", false},
		{"with offset", "SELECT * FROM users OFFSET 10", true},
		{"offset lowercase", "SELECT * FROM users offset 10", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasOffsetClause(tt.sql)
			if got != tt.want {
				t.Errorf("hasOffsetClause(%q) = %v, want %v", tt.sql, got, tt.want)
			}
		})
	}
}
