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
		{
			// A dollar-quoted body is never entered, because the marker
			// lands at the leading token; the $$ ... $$ text is left
			// byte-for-byte alone.
			name: "dollar quoted block is not entered",
			sql:  "DO $$ BEGIN PERFORM 1; END $$",
			want: "DO /* ai_dba_wb_internal */ $$ BEGIN PERFORM 1; END $$",
		},
		{
			name: "tagged dollar quoted function body",
			sql: "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS " +
				"$body$ BEGIN RETURN 1; END $body$",
			want: "CREATE /* ai_dba_wb_internal */ FUNCTION f() RETURNS " +
				"int LANGUAGE plpgsql AS $body$ BEGIN RETURN 1; END $body$",
		},
		{
			name: "dollar quoted literal in a select list",
			sql:  "SELECT $$SELECT 1$$ AS t",
			want: "SELECT /* ai_dba_wb_internal */ $$SELECT 1$$ AS t",
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

// TestTagOnlyTagsTheFirstStatementOfABatch pins the current handling of
// a semicolon-separated batch: Tag locates one leading token and so the
// marker reaches only the first statement, leaving the rest untagged and
// therefore visible in the Top Queries panel when monitoring queries are
// hidden.
//
// This is a pinning test rather than an assertion of desired behavior.
// No call site passes a batch today (every caller passes a single
// statement, and pgx's simple protocol is not used for these queries),
// so the gap is latent. The test exists so that a future change either
// to Tag or to a caller has to update it deliberately.
func TestTagOnlyTagsTheFirstStatementOfABatch(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{
			name: "two selects",
			sql:  "SELECT 1; SELECT 2",
			want: "SELECT /* ai_dba_wb_internal */ 1; SELECT 2",
		},
		{
			name: "insert then delete",
			sql: "INSERT INTO t (a) VALUES (1); " +
				"DELETE FROM t WHERE a = 2",
			want: "INSERT /* ai_dba_wb_internal */ INTO t (a) VALUES (1); " +
				"DELETE FROM t WHERE a = 2",
		},
		{
			name: "trailing semicolon on a single statement",
			sql:  "SELECT 1;",
			want: "SELECT /* ai_dba_wb_internal */ 1;",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Tag(tt.sql)
			if got != tt.want {
				t.Errorf("Tag(%q) = %q, want %q", tt.sql, got, tt.want)
			}
			if strings.Count(got, Marker) != 1 {
				t.Errorf("Tag(%q) inserted %d markers, want 1: %q",
					tt.sql, strings.Count(got, Marker), got)
			}
		})
	}
}

// TestTagLeadingStringLiteralPrefix pins the one input class Tag would
// corrupt: a statement beginning with a string-literal prefix letter,
// where the marker is inserted between the letter and the opening quote.
// PostgreSQL 18 rejects the result, for example `SELECT E /* m */ 'x'`
// fails with `type "e" does not exist`, because the prefix letter and
// the quote must be adjacent.
//
// The class is unreachable, so no defensive code guards against it. No
// valid PostgreSQL statement begins with a bare string literal, and
// every call site passes SQL beginning with a keyword: the collector's
// and alerter's tagged statements are compiled-in literals or are built
// by fmt.Sprintf from a fixed SELECT, INSERT, CREATE, or DROP prefix.
// The test records the behavior so that if Tag ever does become
// reachable from such input, the corruption is documented rather than
// discovered in production.
func TestTagLeadingStringLiteralPrefix(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{
			name: "escape string prefix",
			sql:  `E'x\n'`,
			want: `E /* ai_dba_wb_internal */'x\n'`,
		},
		{
			name: "bit string prefix",
			sql:  "B'1011'",
			want: "B /* ai_dba_wb_internal */'1011'",
		},
		{
			name: "hex bit string prefix",
			sql:  "X'1f'",
			want: "X /* ai_dba_wb_internal */'1f'",
		},
		{
			name: "unicode escape prefix",
			sql:  `U&'d\0061t'`,
			want: `U /* ai_dba_wb_internal */&'d\0061t'`,
		},
		{
			name: "national character prefix",
			sql:  "N'x'",
			want: "N /* ai_dba_wb_internal */'x'",
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
