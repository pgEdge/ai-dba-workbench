/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package sqllex

import (
	"strings"
	"testing"
)

func TestIsReadOnlyStatement(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		readOnly bool
	}{
		{"SELECT", "SELECT 1", true},
		{"select lowercase", "select * from users", true},
		{"SELECT with leading comment", "-- get data\nSELECT 1", true},
		{"WITH CTE", "WITH cte AS (SELECT 1) SELECT * FROM cte", true},
		{"with lowercase", "with x as (select 1) select * from x", true},
		{"SHOW", "SHOW shared_buffers", true},
		{"show lowercase", "show work_mem", true},
		{"EXPLAIN", "EXPLAIN SELECT 1", true},
		{"explain lowercase", "explain select 1", true},
		{"TABLE shorthand", "TABLE users", true},
		{"table lowercase", "table users", true},
		{"ALTER", "ALTER SYSTEM SET work_mem = '16MB'", false},
		{"CREATE", "CREATE TABLE foo (id int)", false},
		{"DROP", "DROP TABLE foo", false},
		{"INSERT", "INSERT INTO foo VALUES (1)", false},
		{"UPDATE", "UPDATE foo SET bar = 1", false},
		{"DELETE", "DELETE FROM foo", false},
		{"TRUNCATE", "TRUNCATE foo", false},
		{"GRANT", "GRANT SELECT ON foo TO bar", false},
		{"REVOKE", "REVOKE SELECT ON foo FROM bar", false},
		{"VACUUM", "VACUUM ANALYZE foo", false},
		{"REINDEX", "REINDEX TABLE foo", false},
		{"CLUSTER", "CLUSTER foo", false},
		{"REFRESH", "REFRESH MATERIALIZED VIEW mv", false},
		{"COMMENT", "COMMENT ON TABLE foo IS 'bar'", false},
		{"SET", "SET work_mem = '256MB'", false},
		{"ALTER with comment", "-- tune memory\nALTER SYSTEM SET work_mem = '16MB'", false},
		{"empty after stripping", "-- just a comment", false},
		{"writable CTE DELETE", "WITH deleted AS (DELETE FROM foo RETURNING *) SELECT * FROM deleted", false},
		{"writable CTE INSERT", "WITH ins AS (INSERT INTO foo VALUES (1) RETURNING *) SELECT * FROM ins", false},
		{"writable CTE UPDATE", "WITH upd AS (UPDATE foo SET bar = 1 RETURNING *) SELECT * FROM upd", false},
		{"CTE with updated_at column", "WITH cte AS (SELECT updated_at FROM foo) SELECT * FROM cte", true},
		{"CTE with delete_flag column", "WITH cte AS (SELECT delete_flag FROM foo) SELECT * FROM cte", true},
		{"CTE pure SELECT", "WITH cte AS (SELECT 1) SELECT * FROM cte", true},
		{"SELECT with block comment", "/* get data */ SELECT 1", true},
		{"ALTER with block comment", "/* tune */ ALTER SYSTEM SET work_mem = '16MB'", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsReadOnly(tt.input)
			if result != tt.readOnly {
				t.Errorf("IsReadOnly(%q) = %v, want %v",
					tt.input, result, tt.readOnly)
			}
		})
	}
}

// TestIsReadOnlyStatement_SideEffectingFunctions pins issue #646 for
// the query endpoint: a statement that calls a side-effecting function
// is a write wherever the classifier reads a SELECT body, whilst the
// names appearing as text are not calls. The spellings and bypass
// attempts the call finder handles are pinned in package sqllex.
func TestIsReadOnlyStatement_SideEffectingFunctions(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		readOnly bool
	}{
		{"select", "SELECT pg_terminate_backend(1234)", false},
		{"schema qualified", "SELECT pg_catalog.pg_terminate_backend(1)", false},
		{"quoted name", `SELECT "pg_terminate_backend"(1)`, false},
		{"unicode escaped name", `SELECT U&"pg\005fterminate\005fbackend"(1)`, false},
		{"comment before paren", "SELECT pg_reload_conf/* x */()", false},
		{"in a FROM clause", "SELECT * FROM pg_terminate_backend(1) AS r", false},
		{"in a CTE", "WITH t AS (SELECT pg_reload_conf()) SELECT * FROM t", false},
		{"in a CTE body after the CTEs", "WITH t AS (SELECT 1) SELECT pg_reload_conf() FROM t", false},
		{"table then a call", "TABLE t; SELECT nextval('s')", false},
		{"explain analyze runs the call", "EXPLAIN ANALYZE SELECT pg_reload_conf()", false},
		{"explain options analyze runs the call", "EXPLAIN (ANALYZE, COSTS OFF) SELECT pg_reload_conf()", false},
		{"second statement", "SELECT 1; SELECT pg_reload_conf()", false},
		{"hidden under the escape reading",
			`SELECT '\'' AS c, pg_reload_conf() --'`, false},
		{"hidden under the standard reading",
			`SELECT 'a\' AS c, pg_reload_conf() --'`, false},
		{"uescape fails closed", `SELECT U&"pg!005freload_conf" UESCAPE '!'()`, false},
		{"spock schema", "SELECT spock.sub_drop('s')", false},
		{"field selection on a literal", "SELECT (123).pg_terminate_backend", false},
		{"field selection on a cast", "SELECT ('x'::text).pg_create_restore_point", false},
		{"field selection running SQL from a string",
			"SELECT ('SELECT to_tsvector(pg_reload_conf()::text)'::text).ts_stat", false},
		{"field selection of a spock function", "SELECT ('s').sub_disable", false},
		{"field selection on a FROM item",
			"SELECT p.pg_terminate_backend FROM unnest(ARRAY[123]) p", false},
		{"table order by", "TABLE pg_namespace ORDER BY pg_reload_conf()", false},
		{"table limit",
			"TABLE pg_namespace LIMIT pg_create_restore_point('x')::text::int", false},
		{"table union", "TABLE t UNION ALL SELECT pg_terminate_backend(12345)", false},
		{"table order by field selection",
			"TABLE pg_namespace ORDER BY (12345).pg_terminate_backend", false},
		{"explain analyze table",
			"EXPLAIN ANALYZE TABLE pg_namespace ORDER BY pg_reload_conf()", false},
		{"listed name after a dot", "SELECT t.nextval FROM t", false},
		{"call after a WITH column label",
			"SELECT 1 AS with FROM pg_class, pg_terminate_backend(12345)", false},
		{"call after a WITH column label in ORDER BY",
			"SELECT 1 AS with FROM pg_class ORDER BY relname, pg_terminate_backend(12345)", false},
		{"call after WITH TIME ZONE",
			"SELECT now()::timestamp WITH TIME ZONE, pg_terminate_backend(12345)", false},
		{"columns after a dot", "SELECT t.id, t.name FROM t", true},
		{"spock subscription columns",
			"SELECT s.sub_name, s.sub_enabled FROM spock.subscription s", true},
		{"spock node_interface table", "SELECT * FROM spock.node_interface", true},
		{"CTE named like a function",
			"WITH schedule(id) AS (SELECT 1) SELECT * FROM schedule", true},

		{"name in a string literal", "SELECT 'pg_terminate_backend(1)'", true},
		{"name in a dollar quote", "SELECT $$pg_reload_conf()$$", true},
		{"name in a comment", "SELECT 1 /* pg_reload_conf() */", true},
		{"ordinary functions", "SELECT now(), count(*), current_setting('work_mem') FROM t", true},
		{"explain without analyze plans only", "EXPLAIN SELECT pg_reload_conf()", true},
		{"show", "SHOW work_mem", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsReadOnly(tt.input); got != tt.readOnly {
				t.Errorf("IsReadOnly(%q) = %v, want %v",
					tt.input, got, tt.readOnly)
			}
		})
	}
}

// TestIssue530_IsReadOnlyStatementExplain covers classification of
// EXPLAIN by what it explains rather than by its leading keyword.
func TestIssue530_IsReadOnlyStatementExplain(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		readOnly bool
	}{
		{"explain select", "EXPLAIN SELECT * FROM t", true},
		{"explain delete plans only", "EXPLAIN DELETE FROM t", true},
		{"explain verbose delete plans only", "EXPLAIN VERBOSE DELETE FROM t", true},
		{"legacy explain verbose select", "EXPLAIN VERBOSE SELECT 1", true},
		{"explain analyze select", "EXPLAIN ANALYZE SELECT 1", true},
		{"explain analyze delete", "EXPLAIN ANALYZE DELETE FROM t", false},
		{"explain analyse update", "EXPLAIN ANALYSE UPDATE t SET a = 1", false}, //nolint:misspell // British spelling accepted by PostgreSQL
		{"explain analyze verbose insert", "EXPLAIN ANALYZE VERBOSE INSERT INTO t VALUES (1)", false},
		{"explain verbose analyze insert", "EXPLAIN VERBOSE ANALYZE INSERT INTO t VALUES (1)", false},
		{"explain paren analyze buffers insert",
			"EXPLAIN (ANALYZE, BUFFERS) INSERT INTO t VALUES (1)", false},
		{"explain paren format json select", "EXPLAIN (FORMAT JSON) SELECT 1", true},
		{"explain paren format json delete plans only",
			"EXPLAIN (FORMAT JSON) DELETE FROM t", true},
		{"explain paren analyze false is still a write",
			"EXPLAIN (ANALYZE false) DELETE FROM t", false},
		{"explain paren analyze writable cte",
			"EXPLAIN (ANALYZE) WITH x AS (DELETE FROM t RETURNING *) SELECT * FROM x", false},
		{"explain paren analyze read-only cte",
			"EXPLAIN (ANALYZE) WITH x AS (SELECT 1) SELECT * FROM x", true},
		{"explain analyze mixed case", "ExPlAiN aNaLyZe DeLeTe FROM t", false},
		{"explain analyze lowercase", "explain analyze delete from t", false},
		{"form feed before a comment hiding analyze",
			"EXPLAIN\f/* x */ ANALYZE DELETE FROM t", false},
		{"form feed before a line comment hiding analyze",
			"EXPLAIN\f-- c\nANALYZE DELETE FROM t", false},
		{"form feed before a comment hiding the option list",
			"EXPLAIN\f/**/(ANALYZE) DELETE FROM t", false},
		{"vertical tab before a comment hiding analyze",
			"EXPLAIN\v/* x */ ANALYZE DELETE FROM t", false},
		{"carriage return ends a line comment before analyze",
			"EXPLAIN -- c\rANALYZE DELETE FROM t", false},
		{"explain with leading comment",
			"-- plan it\nEXPLAIN ANALYZE DELETE FROM t", false},
		{"explain with block comment before options",
			"EXPLAIN /* opts */ (ANALYZE) DELETE FROM t", false},
		{"analyze hidden behind a comment containing a paren",
			"EXPLAIN (/* ) */ ANALYZE) DELETE FROM t", false},
		{"analyze in an option value is a value, not a name",
			"EXPLAIN (FORMAT 'analyze') DELETE FROM t", true},
		{"unbalanced options fail closed", "EXPLAIN (ANALYZE DELETE FROM t", false},
		{"nested parentheses in the options",
			"EXPLAIN (ANALYZE (on)) DELETE FROM t", false},
		{"analyze inside an option comment is not an option name",
			"EXPLAIN (FORMAT JSON -- ANALYZE\n) DELETE FROM t", true},
		{"explain with no statement", "EXPLAIN", true},
		{"explain analyze with no statement", "EXPLAIN ANALYZE", false},
		{"explainable identifier is not explain", "EXPLAINABLE SELECT 1", false},
		{"nested explain analyze of a select",
			"EXPLAIN ANALYZE EXPLAIN ANALYZE SELECT 1", true},
		{"nested explain analyze of a delete",
			"EXPLAIN ANALYZE EXPLAIN ANALYZE DELETE FROM t", false},
		{"pathological nesting fails closed",
			strings.Repeat("EXPLAIN ANALYZE ", 64) + "SELECT 1", false},
		{"no-analyze nesting stays read-only",
			strings.Repeat("EXPLAIN ", 64) + "DELETE FROM t", true},
		{"unicode-escaped analyze option name",
			`EXPLAIN (U&"\0061nalyze") DELETE FROM t WHERE id = 99`, false},
		{"lowercase unicode-escaped analyze option name",
			`EXPLAIN (u&"\0061nalyze") DELETE FROM t WHERE id = 99`, false},
		{"unicode escape at the end of the option name",
			`EXPLAIN (U&"analyz\0065") DELETE FROM t WHERE id = 99`, false},
		{"unicode-escaped analyze option name with a value",
			`EXPLAIN (U&"\0061nalyze" true) DELETE FROM t WHERE id = 99`, false},
		{"unicode-escaped option name after a recognized one",
			`EXPLAIN (VERBOSE, U&"\0061nalyze") DELETE FROM t`, false},
		{"unicode-escaped name of a recognized option still fails closed",
			`EXPLAIN (U&"verbose") DELETE FROM t`, false},
		{"quoted analyze option name",
			`EXPLAIN ("analyze") DELETE FROM t`, false},
		{"quoted analyze option name with a value",
			`EXPLAIN ("analyze" true) DELETE FROM t`, false},
		{"quoted recognized option name still fails closed",
			`EXPLAIN ("verbose") DELETE FROM t`, false},
		{"unrecognized option name fails closed",
			"EXPLAIN (FUTURE_OPTION) DELETE FROM t", false},
		{"empty option list fails closed", "EXPLAIN () DELETE FROM t", false},
		{"trailing empty option entry fails closed",
			"EXPLAIN (VERBOSE,) DELETE FROM t", false},
		{"every recognized option name is non-executing",
			"EXPLAIN (VERBOSE, COSTS off, SETTINGS on, GENERIC_PLAN on, " +
				"BUFFERS off, WAL off, TIMING off, SUMMARY off, MEMORY off, " +
				"SERIALIZE none, FORMAT JSON) DELETE FROM t", true},
		{"recognized options lowercase",
			"explain (verbose, format json) delete from t", true},
		{"parenthesis inside a dollar-quoted option value",
			"EXPLAIN (FORMAT $$)$$, ANALYZE) DELETE FROM t", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsReadOnly(tt.input); got != tt.readOnly {
				t.Errorf("IsReadOnly(%q) = %v, want %v",
					tt.input, got, tt.readOnly)
			}
		})
	}
}

// TestIssue530_WritingSelectClauses covers the statements that read at
// first glance but write: a writable CTE using MERGE, SELECT ... INTO,
// which creates a table, and the row-locking clauses, which stamp the
// lock onto every row they return.
func TestIssue530_WritingSelectClauses(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		readOnly bool
	}{
		{"writable CTE MERGE",
			"WITH c AS (SELECT 1) MERGE INTO t USING s ON t.id = s.id " +
				"WHEN MATCHED THEN DO NOTHING", false},
		{"select into", "SELECT * INTO x FROM t", false},
		{"select into lowercase", "select a into x from t", false},
		{"select for update", "SELECT * FROM t FOR UPDATE", false},
		{"select for no key update", "SELECT * FROM t FOR NO KEY UPDATE", false},
		{"select for share", "SELECT * FROM t FOR SHARE", false},
		{"select for key share", "SELECT * FROM t FOR KEY SHARE", false},
		{"select for update lowercase", "select * from t for update", false},
		{"table shorthand for update", "TABLE t FOR UPDATE", false},
		{"explain analyze select for update",
			"EXPLAIN ANALYZE SELECT * FROM t FOR UPDATE", false},
		{"cte with a locking clause",
			"WITH c AS (SELECT 1 FOR UPDATE) SELECT * FROM c", false},
		{"plain select is unaffected", "SELECT * FROM t", true},
		{"substring for is not a locking clause",
			"SELECT substring(a FROM 1 FOR 2) FROM t", true},
		{"a column called format is not a locking clause",
			"SELECT format FROM t", true},
		{"a column called information is not INTO",
			"SELECT information FROM t", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsReadOnly(tt.input); got != tt.readOnly {
				t.Errorf("IsReadOnly(%q) = %v, want %v",
					tt.input, got, tt.readOnly)
			}
		})
	}
}

// TestIssue530_KeywordScansIgnoreNonCode pins that the write-clause and
// writable-CTE scans read code only, so a keyword sitting in a string
// literal, a quoted identifier or a comment no longer classifies an
// ordinary read as a write.
func TestIssue530_KeywordScansIgnoreNonCode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		readOnly bool
	}{
		{"into inside a string literal",
			"SELECT * FROM audit WHERE msg LIKE '%signed into%'", true},
		{"into inside a trailing line comment",
			"SELECT * FROM t -- copy into x", true},
		{"into inside a block comment",
			"SELECT * FROM t /* select into x */ WHERE a = 1", true},
		{"locking clause inside a string literal",
			"SELECT * FROM t WHERE note = 'FOR UPDATE'", true},
		{"into as a quoted identifier",
			`SELECT "into" FROM t`, true},
		{"locking clause as a quoted identifier",
			`SELECT "for", "update" FROM t`, true},
		{"delete inside a literal in a CTE",
			"WITH c AS (SELECT 'delete' AS a) SELECT * FROM c", true},
		{"insert inside a comment in a CTE",
			"WITH c AS (SELECT 1) /* insert */ SELECT * FROM c", true},
		{"escape literal containing into",
			`SELECT * FROM t WHERE a = E'signed into\n'`, true},
		{"unicode literal containing for update",
			`SELECT * FROM t WHERE a = U&'for update'`, true},
		{"genuine into beside a masking literal",
			"SELECT 'into' AS a INTO x FROM t", false},
		{"genuine locking clause beside a comment",
			"SELECT * FROM t -- nothing here\nFOR UPDATE", false},
		{"genuine writable CTE beside a literal",
			"WITH c AS (DELETE FROM t WHERE a = 'delete' RETURNING *) " +
				"SELECT * FROM c", false},
		{"delete inside a dollar-quoted literal in a CTE",
			"WITH c AS (SELECT $$delete$$ AS a) SELECT * FROM c", true},
		{"single quote inside a dollar quote hides nothing",
			"WITH a AS (SELECT $q$'$q$ AS c), d AS (DELETE FROM t RETURNING *) " +
				"SELECT * FROM d", false},
		{"double quote inside a dollar quote hides nothing",
			`WITH a AS (SELECT $$"$$ AS c), d AS (DELETE FROM t RETURNING *) ` +
				"SELECT * FROM d", false},
		{"line comment marker inside a dollar quote hides nothing",
			"WITH a AS (SELECT $$--$$ AS c), d AS (DELETE FROM t RETURNING *) " +
				"SELECT * FROM d", false},
		{"block comment marker inside a dollar quote hides nothing",
			"WITH a AS (SELECT $$/*$$ AS c), d AS (DELETE FROM t RETURNING *) " +
				"SELECT * FROM d", false},
		{"into after a dollar quote holding a quote",
			"SELECT $$'$$ AS a INTO x FROM t", false},
		{"dollar continuing an identifier opens no dollar quote",
			"WITH a AS (SELECT 1 AS c$q$), d AS (DELETE FROM t RETURNING *) " +
				"SELECT * FROM d -- $q$", false},
		{"quote inside a non-ASCII dollar tag hides nothing",
			"WITH a AS (SELECT $é$'$é$ AS c), d AS (DELETE FROM t RETURNING *) " +
				"SELECT * FROM d", false},
		{"explain analyze of a non-ASCII dollar tag hiding a delete",
			"EXPLAIN ANALYZE WITH a AS (SELECT $é$'$é$ AS c), " +
				"d AS (DELETE FROM t RETURNING *) SELECT * FROM d", false},
		{"into after a non-ASCII dollar tag holding a quote",
			"SELECT $é$'$é$ AS c INTO newtab FROM t", false},
		{"comment marker inside a mixed non-ASCII dollar tag hides nothing",
			"WITH a AS (SELECT $qé$--$qé$ AS c), d AS (DELETE FROM t RETURNING *) " +
				"SELECT * FROM d", false},
		{"dollar continuing a non-ASCII identifier opens no dollar quote",
			"WITH a AS (SELECT 1 AS é$q$), d AS (DELETE FROM t RETURNING *) " +
				"SELECT * FROM d -- $q$", false},
		{"E after a non-ASCII identifier byte is not an escape prefix",
			`WITH a AS (SELECT éE'\' AS c), d AS (DELETE FROM t RETURNING *) ` +
				"SELECT * FROM d", false},
		{"E after a dollar continuing an identifier is not an escape prefix",
			`WITH x AS (SELECT a$E'\' AS c), d AS (DELETE FROM t RETURNING *) ` +
				"SELECT * FROM d", false},
		{"explain analyze of an identifier dollar before an E literal",
			`EXPLAIN ANALYZE WITH x AS (SELECT a$E'\' AS c), ` +
				"d AS (DELETE FROM t RETURNING *) SELECT * FROM d", false},
		{"into after an identifier dollar before an E literal",
			`SELECT a$E'\' AS c INTO newtab FROM t`, false},
		{"carriage return ends a line comment",
			"WITH a AS (SELECT 1) -- x\r, d AS (DELETE FROM t RETURNING *) " +
				"SELECT * FROM d", false},
		{"form feed before a leading comment",
			"\f/* x */ WITH d AS (DELETE FROM t RETURNING *) SELECT * FROM d", false},
		{"backslash quote under standard_conforming_strings off",
			`WITH x AS (SELECT '\'' AS c), d AS (DELETE FROM t RETURNING *) ` +
				"SELECT * FROM d --'", false},
		{"standard_conforming_strings turned off earlier in the request",
			`SELECT set_config('standard_conforming_strings', 'off', false); ` +
				`WITH x AS (SELECT '\'' AS c), d AS (DELETE FROM t RETURNING *) ` +
				"SELECT * FROM d --'", false},
		{"backslash quote in a national literal",
			`WITH x AS (SELECT N'\'' AS c), d AS (DELETE FROM t RETURNING *) ` +
				"SELECT * FROM d --'", false},
		{"explain analyze of a backslash quote",
			`EXPLAIN ANALYZE WITH x AS (SELECT '\'' AS c), ` +
				"d AS (DELETE FROM t RETURNING *) SELECT * FROM d --'", false},
		{"into after a backslash quote",
			`SELECT '\'' AS c INTO newtab FROM t --'`, false},
		{"semicolon only an escape reading treats as code",
			`SELECT '\''; DELETE FROM t; --'`, false},
		{"escape string continued onto the next line",
			"WITH x AS (SELECT E'a'\n'\\'' AS c), d AS (DELETE FROM t RETURNING *) " +
				"SELECT * FROM d --'", false},
		{"escape string continued after a line comment",
			"WITH x AS (SELECT E'a' -- c\n  '\\'' AS c), " +
				"d AS (DELETE FROM t RETURNING *) SELECT * FROM d --'", false},
		{"escape string continued after a carriage return",
			"WITH x AS (SELECT E'a'\r'\\'' AS c), d AS (DELETE FROM t RETURNING *) " +
				"SELECT * FROM d --'", false},
		{"backslash in a plain literal under both readings",
			`SELECT 'C:\temp' AS path FROM t`, true},
		{"literal continued onto the next line",
			"SELECT 'signed'\n' into' AS msg FROM t", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsReadOnly(tt.input); got != tt.readOnly {
				t.Errorf("IsReadOnly(%q) = %v, want %v",
					tt.input, got, tt.readOnly)
			}
		})
	}
}
