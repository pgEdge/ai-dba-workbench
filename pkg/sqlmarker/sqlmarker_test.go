/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package sqlmarker

import (
	"strings"
	"testing"
)

func TestMarkerConstants(t *testing.T) {
	if Marker != "ai_dba_wb_internal" {
		t.Fatalf("Marker = %q, want ai_dba_wb_internal", Marker)
	}
	if Comment != "/* ai_dba_wb_internal */" {
		t.Fatalf("Comment = %q, want /* ai_dba_wb_internal */", Comment)
	}
}

func TestTag(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{
			name: "select",
			sql:  "SELECT * FROM metrics.pg_settings WHERE connection_id = $1",
			want: "SELECT /* ai_dba_wb_internal */ * FROM metrics.pg_settings WHERE connection_id = $1",
		},
		{
			name: "insert",
			sql:  `INSERT INTO "metrics"."pg_stat_statements" ("a") VALUES ($1)`,
			want: `INSERT /* ai_dba_wb_internal */ INTO "metrics"."pg_stat_statements" ("a") VALUES ($1)`,
		},
		{
			name: "with",
			sql:  "WITH recent_statements AS (SELECT 1) SELECT * FROM recent_statements",
			want: "WITH /* ai_dba_wb_internal */ recent_statements AS (SELECT 1) SELECT * FROM recent_statements",
		},
		{
			name: "update",
			sql:  "UPDATE probe_configs SET is_enabled = true",
			want: "UPDATE /* ai_dba_wb_internal */ probe_configs SET is_enabled = true",
		},
		{
			name: "delete",
			sql:  "DELETE FROM metrics.pg_stat_activity WHERE collected_at < now()",
			want: "DELETE /* ai_dba_wb_internal */ FROM metrics.pg_stat_activity WHERE collected_at < now()",
		},
		{
			name: "create table ddl",
			sql:  "CREATE TABLE IF NOT EXISTS metrics.x PARTITION OF metrics.y",
			want: "CREATE /* ai_dba_wb_internal */ TABLE IF NOT EXISTS metrics.x PARTITION OF metrics.y",
		},
		{
			name: "drop table ddl",
			sql:  `DROP TABLE IF EXISTS "metrics"."pg_settings_20260101"`,
			want: `DROP /* ai_dba_wb_internal */ TABLE IF EXISTS "metrics"."pg_settings_20260101"`,
		},
		{
			name: "lowercase keyword",
			sql:  "select 1",
			want: "select /* ai_dba_wb_internal */ 1",
		},
		{
			name: "leading newline and indent",
			sql: `
        SELECT connection_id, value
        FROM metrics.pg_stat_database
    `,
			want: `
        SELECT /* ai_dba_wb_internal */ connection_id, value
        FROM metrics.pg_stat_database
    `,
		},
		{
			name: "leading tabs and carriage return",
			sql:  "\r\n\t\vSELECT 1",
			want: "\r\n\t\vSELECT /* ai_dba_wb_internal */ 1",
		},
		{
			name: "keyword immediately followed by star",
			sql:  "SELECT*FROM t",
			want: "SELECT /* ai_dba_wb_internal */*FROM t",
		},
		{
			name: "empty input unchanged",
			sql:  "",
			want: "",
		},
		{
			name: "whitespace only input unchanged",
			sql:  "  \n\t ",
			want: "  \n\t ",
		},
		{
			name: "parenthesised leading token unchanged",
			sql:  "(SELECT 1) UNION (SELECT 2)",
			want: "(SELECT 1) UNION (SELECT 2)",
		},
		{
			name: "leading comment unchanged",
			sql:  "/* hint */ SELECT 1",
			want: "/* hint */ SELECT 1",
		},
		{
			name: "leading line comment unchanged",
			sql:  "-- note\nSELECT 1",
			want: "-- note\nSELECT 1",
		},
		{
			name: "already tagged is unchanged",
			sql:  "SELECT /* ai_dba_wb_internal */ 1",
			want: "SELECT /* ai_dba_wb_internal */ 1",
		},
		{
			name: "marker present anywhere is unchanged",
			sql:  "SELECT 'ai_dba_wb_internal' AS m",
			want: "SELECT 'ai_dba_wb_internal' AS m",
		},
		{
			name: "multibyte leading character unchanged",
			sql:  "é SELECT 1",
			want: "é SELECT 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Tag(tt.sql)
			if got != tt.want {
				t.Errorf("Tag(%q) = %q, want %q", tt.sql, got, tt.want)
			}
		})
	}
}

// TestTagIsIdempotent guards against double-tagging when a statement
// passes through more than one helper on its way to the datastore.
func TestTagIsIdempotent(t *testing.T) {
	sql := "INSERT INTO metrics.pg_stat_activity (a) VALUES ($1)"
	once := Tag(sql)
	twice := Tag(once)
	if once != twice {
		t.Errorf("Tag is not idempotent: %q vs %q", once, twice)
	}
	if strings.Count(once, Marker) != 1 {
		t.Errorf("expected exactly one marker, got %d in %q",
			strings.Count(once, Marker), once)
	}
}

// TestTagDoesNotPrefixStatement is the regression guard for the
// pg_stat_statements behavior documented on Tag: PostgreSQL strips a
// comment that precedes the leading keyword, so the marker must never
// appear at the start of the statement text.
func TestTagDoesNotPrefixStatement(t *testing.T) {
	for _, sql := range []string{
		"SELECT 1",
		"INSERT INTO t (a) VALUES (1)",
		"\n    WITH x AS (SELECT 1) SELECT * FROM x",
	} {
		tagged := Tag(sql)
		trimmed := strings.TrimLeft(tagged, " \t\r\n\v\f")
		if strings.HasPrefix(trimmed, Comment) {
			t.Errorf("Tag(%q) placed the marker at the start: %q",
				sql, tagged)
		}
		if !strings.Contains(tagged, Comment) {
			t.Errorf("Tag(%q) did not insert the marker: %q", sql, tagged)
		}
		// The marker must follow the first keyword, never precede it.
		markerAt := strings.Index(tagged, Comment)
		keywordAt := len(tagged) - len(trimmed)
		if markerAt <= keywordAt {
			t.Errorf("Tag(%q) put the marker at %d, before or at the "+
				"keyword at %d: %q", sql, markerAt, keywordAt, tagged)
		}
	}
}

// TestTagPreservesTrailingSQL confirms the remainder of the statement,
// including bind-parameter placeholders, survives untouched.
func TestTagPreservesTrailingSQL(t *testing.T) {
	sql := "UPDATE t SET a = $1, b = $2 WHERE id = $3 RETURNING id"
	tagged := Tag(sql)
	rest := strings.TrimPrefix(tagged, "UPDATE "+Comment)
	if rest != strings.TrimPrefix(sql, "UPDATE") {
		t.Errorf("Tag altered the statement body: %q", tagged)
	}
}
