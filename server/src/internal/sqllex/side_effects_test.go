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
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

// TestCallsSideEffectingFunction pins issue #646: a call to a
// side-effecting function is found however it is written, whilst the
// names appearing as text are not calls.
func TestCallsSideEffectingFunction(t *testing.T) {
	tests := []struct {
		name  string
		input string
		calls bool
	}{
		// One member of each family.
		{"terminate backend", "SELECT pg_terminate_backend(1234)", true},
		{"cancel backend", "SELECT pg_cancel_backend(pid) FROM pg_stat_activity", true},
		{"reload conf", "SELECT pg_reload_conf()", true},
		{"rotate logfile", "SELECT pg_rotate_logfile()", true},
		{"log memory contexts", "SELECT pg_log_backend_memory_contexts(1)", true},
		{"set_config", "SELECT set_config('work_mem', '1GB', false)", true},
		{"restore point", "SELECT pg_create_restore_point('x')", true},
		{"switch wal", "SELECT pg_switch_wal()", true},
		{"backup start", "SELECT pg_backup_start('x')", true},
		{"legacy backup start", "SELECT pg_start_backup('x')", true},
		{"legacy switch xlog", "SELECT pg_switch_xlog()", true},
		{"promote", "SELECT pg_promote()", true},
		{"replay pause", "SELECT pg_wal_replay_pause()", true},
		{"legacy replay pause", "SELECT pg_xlog_replay_pause()", true},
		{"standby snapshot", "SELECT pg_log_standby_snapshot()", true},
		{"create slot", "SELECT pg_create_logical_replication_slot('s', 'pgoutput')", true},
		{"drop slot", "SELECT pg_drop_replication_slot('s')", true},
		{"slot get changes", "SELECT * FROM pg_logical_slot_get_changes('s', NULL, NULL)", true},
		{"origin create", "SELECT pg_replication_origin_create('o')", true},
		{"advisory lock", "SELECT pg_advisory_lock(1)", true},
		{"try advisory xact lock", "SELECT pg_try_advisory_xact_lock(1)", true},
		{"advisory unlock all", "SELECT pg_advisory_unlock_all()", true},
		{"stat reset", "SELECT pg_stat_reset()", true},
		{"stat reset shared", "SELECT pg_stat_reset_shared('bgwriter')", true},
		{"restore relation stats", "SELECT pg_restore_relation_stats('relation', 't')", true},
		{"brin summarize", "SELECT brin_summarize_new_values('i')", true},
		{"import collations", "SELECT pg_import_system_collations('pg_catalog')", true},
		{"xid assignment", "SELECT txid_current()", true},
		{"notify", "SELECT pg_notify('c', 'p')", true},
		{"nextval", "SELECT nextval('s')", true},
		{"setval", "SELECT setval('s', 1)", true},
		{"large object unlink", "SELECT lo_unlink(1234)", true},
		{"large object write", "SELECT lowrite(0, 'x')", true},
		{"query_to_xml runs text", "SELECT query_to_xml('SELECT 1', true, false, '')", true},
		{"ts_stat runs text", "SELECT * FROM ts_stat('SELECT v FROM t')", true},
		{"ts_rewrite runs text", "SELECT ts_rewrite('a'::tsquery, 'SELECT t, s FROM a')", true},
		{"dblink exec", "SELECT dblink_exec('dbname=x', 'DELETE FROM t')", true},
		{"dblink", "SELECT * FROM dblink('dbname=x', 'SELECT 1') AS t(a int)", true},
		{"pg_stat_statements reset", "SELECT pg_stat_statements_reset()", true},
		{"buffercache evict", "SELECT pg_buffercache_evict(1)", true},
		{"pg_surgery", "SELECT heap_force_kill('t', ARRAY['(0,1)']::tid[])", true},
		{"adminpack write", "SELECT pg_file_write('f', 'x', false)", true},
		{"spock unqualified", "SELECT replicate_ddl('DROP TABLE t')", true},
		{"spock qualified", "SELECT spock.sub_drop('s')", true},
		{"spock schema any function", "SELECT spock.some_future_function()", true},
		{"pglogical schema", "SELECT pglogical.drop_node('n')", true},
		{"pglogical unqualified", "SELECT create_subscription('s', 'host=x')", true},
		{"pglogical ddl unqualified", "SELECT replicate_ddl_command('DROP TABLE t')", true},
		{"pglogical status is read-only", "SELECT * FROM show_subscription_status()", false},
		{"pg_cron unqualified", "SELECT schedule('* * * * *', 'DELETE FROM t')", true},
		{"pg_cron in database", "SELECT schedule_in_database('j', '* * * * *', 'VACUUM', 'db')", true},
		{"pg_cron unschedule", "SELECT unschedule(1)", true},
		{"pg_cron alter_job", "SELECT alter_job(1, active := false)", true},
		{"pg_background launch", "SELECT pg_background_launch('DELETE FROM t')", true},
		{"pg_background result", "SELECT * FROM pg_background_result(1) AS (r text)", true},
		{"column named schedule", "SELECT schedule FROM jobs", false},
		{"cron schema", "SELECT cron.schedule('* * * * *', 'VACUUM')", true},

		// Every form of name a call can take.
		{"upper case", "SELECT PG_TERMINATE_BACKEND(1)", true},
		{"mixed case", "select Pg_Terminate_Backend(1)", true},
		{"schema qualified", "SELECT pg_catalog.pg_terminate_backend(1)", true},
		{"schema qualified with spaces", "SELECT pg_catalog . pg_terminate_backend (1)", true},
		{"quoted name", `SELECT "pg_terminate_backend"(1)`, true},
		{"quoted schema and name", `SELECT "pg_catalog"."pg_terminate_backend"(1)`, true},
		{"quoted upper case name", `SELECT "PG_TERMINATE_BACKEND"(1)`, true},
		{"unicode escaped name", `SELECT U&"pg\005fterminate\005fbackend"(1)`, true},
		{"unicode escaped name lower prefix", `SELECT u&"\0070g_reload_conf"()`, true},
		{"six digit unicode escape", `SELECT U&"pg\+00005freload_conf"()`, true},
		{"unicode escaped spock schema", `SELECT U&"sp\006fck".sub_drop('s')`, true},
		{"quoted spock schema", `SELECT "spock"."sub_drop"('s')`, true},
		{"three part name", "SELECT db.spock.sub_drop('s')", true},
		{"whitespace before paren", "SELECT pg_reload_conf\n\t ()", true},
		{"line comment before paren", "SELECT pg_reload_conf -- x\n()", true},
		{"block comment before paren", "SELECT pg_reload_conf/* x */()", true},
		{"nested block comment before paren", "SELECT pg_reload_conf/* a /* b */ c */()", true},
		{"comment around the dot", "SELECT spock/* a */./* b */sub_drop('s')", true},
		{"digit glued to the name", "SELECT 1pg_reload_conf()", true},
		{"hex literal glued to the name", "SELECT 0xApg_reload_conf()", true},
		{"in a FROM clause", "SELECT * FROM pg_terminate_backend(1) AS r", true},
		{"in ROWS FROM", "SELECT * FROM ROWS FROM (pg_reload_conf())", true},
		{"in a subquery", "SELECT (SELECT pg_reload_conf())", true},
		{"in a WHERE clause", "SELECT pid FROM pg_stat_activity WHERE pg_cancel_backend(pid)", true},
		{"in a CTE", "WITH t AS (SELECT pg_reload_conf()) SELECT * FROM t", true},
		{"lateral", "SELECT * FROM pg_stat_activity a, LATERAL (SELECT pg_cancel_backend(a.pid)) c", true},
		{"after a literal", "SELECT 'x', pg_reload_conf()", true},
		{"after a dollar quote", "SELECT $$x$$, pg_reload_conf()", true},
		{"after a quoted identifier", `SELECT 1 AS "a", pg_reload_conf()`, true},
		{"after an escape string", `SELECT E'\'', pg_reload_conf()`, true},
		{"explain analyze runs the call", "EXPLAIN ANALYZE SELECT pg_reload_conf()", true},
		{"explain options analyze runs the call", "EXPLAIN (ANALYZE, COSTS OFF) SELECT pg_reload_conf()", true},
		{"second statement", "SELECT 1; SELECT pg_reload_conf()", true},
		{"hidden under the escape reading",
			`SELECT '\'' AS c, pg_reload_conf() --'`, true},
		{"hidden under the standard reading",
			`SELECT 'a\' AS c, pg_reload_conf() --'`, true},

		// Fail closed.
		{"uescape", `SELECT U&"pg!005freload_conf" UESCAPE '!'()`, true},
		{"undecodable unicode escape", `SELECT U&"pg\zzzzreload"()`, true},
		{"truncated unicode escape", `SELECT U&"pg\00"()`, true},
		{"surrogate unicode escape", `SELECT U&"\D800"()`, true},
		{"out of range unicode escape", `SELECT U&"\+110000"()`, true},

		// The names as text, not calls.
		{"name in a string literal", "SELECT 'pg_terminate_backend(1)'", false},
		{"name in an escape string", `SELECT E'pg_reload_conf()\''`, false},
		{"name in a dollar quote", "SELECT $$pg_reload_conf()$$", false},
		{"name in a tagged dollar quote", "SELECT $q$pg_reload_conf()$q$", false},
		{"name in a line comment", "SELECT 1 -- pg_reload_conf()", false},
		{"name in a block comment", "SELECT 1 /* pg_reload_conf() */", false},
		{"name in a leading comment", "/* pg_reload_conf() */ SELECT 1", false},
		{"name as a column alias", `SELECT 1 AS "pg_reload_conf"`, false},
		{"name as a column", "SELECT nextval FROM t", false},
		{"name in a LIKE pattern", "SELECT query FROM pg_stat_activity WHERE query LIKE '%pg_terminate_backend(%'", false},
		{"name with a suffix", "SELECT pg_reload_confx()", false},
		{"name with a dollar suffix", "SELECT pg_reload_conf$x()", false},
		{"name with a prefix", "SELECT my_pg_reload_conf()", false},
		{"cast to regproc", "SELECT 'pg_reload_conf'::regproc", false},
		{"read-only origin lookalike", "SELECT pg_replication_origin_status FROM t", false},
		{"spock view", "SELECT * FROM spock.node", false},
		{"spock column", "SELECT spock.node.node_name FROM spock.node", false},
		{"ordinary functions", "SELECT now(), count(*), current_setting('work_mem') FROM t", false},
		{"pg_backend_pid", "SELECT pg_backend_pid()", false},
		{"currval and lastval", "SELECT currval('s'), lastval()", false},
		{"lo_get", "SELECT lo_get(1234)", false},
		{"monitoring query", "SELECT pid, state, pg_blocking_pids(pid) FROM pg_stat_activity", false},
		{"unicode identifier that is not listed", `SELECT U&"d\0061ta" FROM t`, false},
		{"unicode string literal", `SELECT U&'pg\005freload_conf()'`, false},

		// Names added after the first review.
		{"export snapshot", "SELECT pg_export_snapshot()", true},
		{"stat force next flush", "SELECT pg_stat_force_next_flush()", true},
		{"rotate logfile before 17", "SELECT pg_rotate_logfile_old()", true},
		{"adminpack 1.0 rotate", "SELECT pg_logfile_rotate()", true},
		{"spock convert column", "SELECT convert_column_to_int8('t', 'c')", true},
		{"spock 3.3 readonly", "SELECT set_cluster_readonly()", true},
		{"spock 3.3 prune", "SELECT prune_conflict_tracking()", true},

		// Field selection: PostgreSQL reads arg.func as func(arg).
		{"field selection", "SELECT (0).pg_terminate_backend", true},
		{"field selection upper case", "SELECT (0).PG_TERMINATE_BACKEND", true},
		{"field selection quoted", `SELECT (0)."pg_terminate_backend"`, true},
		{"field selection comment after dot", `SELECT (0)./**/"pg_terminate_backend"`, true},
		{"field selection newline after dot", "SELECT (0).\npg_terminate_backend", true},
		{"field selection nested comment after dot", "SELECT (0)./* a /* b */ c */pg_terminate_backend", true},
		{"field selection line comment before dot", "SELECT (0) -- c\n.pg_terminate_backend", true},
		{"field selection spaces around dot", "SELECT (0) . pg_terminate_backend", true},
		{"field selection unicode", `SELECT (0).U&"pg\005fterminate\005fbackend"`, true},
		{"field selection unicode lower prefix", `SELECT (0).u&"pg\005fterminate\005fbackend"`, true},
		{"field selection uescape", `SELECT (0).U&"pg!005fterminate!005fbackend" UESCAPE '!'`, true},
		{"field selection double parens", "SELECT ((0)).pg_terminate_backend", true},
		{"field selection cast", "SELECT (646::bigint).pg_advisory_xact_lock", true},
		{"field selection array subscript", "SELECT (ARRAY[0])[1].pg_terminate_backend", true},
		{"field selection FROM alias", "SELECT p.pg_terminate_backend FROM unnest(ARRAY[0]) p", true},
		{"field selection in TABLE ORDER BY", "TABLE t ORDER BY (0).pg_terminate_backend", true},
		{"field selection under explain analyze", "EXPLAIN ANALYZE SELECT (0).pg_terminate_backend", true},
		{"field selection prefix family", "SELECT (1).pg_advisory_unlock", true},
		{"field selection of reload", "SELECT x.pg_reload_conf FROM (SELECT 1) x", true},
		{"column sharing a listed name", "SELECT t.nextval FROM t", true},

		// Dots that are not field selection of a listed function.
		{"decimal literal", "SELECT 1.5", false},
		{"qualified columns", "SELECT t.id, t.name FROM t", false},
		{"spock subscription columns", "SELECT s.sub_name, s.sub_enabled FROM spock.subscription s", false},
		{"pg_cron schedule column", "SELECT j.schedule FROM cron.job j", false},
		{"origin status view", "SELECT * FROM pg_catalog.pg_replication_origin_status", false},
		{"alias named spock", "SELECT spock.node_id FROM t spock", false},

		// Names before a parenthesis that are not calls.
		{"CTE named like a function", "WITH schedule(id) AS (SELECT 1) SELECT * FROM schedule", false},
		{"recursive CTE", "WITH RECURSIVE pg_notify(n) AS (SELECT 1) SELECT * FROM pg_notify", false},
		{"materialized CTE", "WITH nextval(n) AS MATERIALIZED (SELECT 1) SELECT * FROM nextval", false},
		{"not materialized CTE", "WITH nextval(n) AS NOT MATERIALIZED (SELECT 1) SELECT * FROM nextval", false},
		{"second CTE", "WITH a AS (SELECT 1), setval(n) AS (SELECT 2) SELECT * FROM a, setval", false},
		{"CTE in a subquery", "SELECT * FROM (WITH schedule(id) AS (SELECT 1) SELECT * FROM schedule) s", false},
		{"CTE after explain", "EXPLAIN WITH schedule(id) AS (SELECT 1) SELECT * FROM schedule", false},
		{"alias column list", "SELECT * FROM t AS create_node(x)", false},
		{"dblink-named alias", "SELECT * FROM generate_series(1, 2) AS dblink_rows(x)", false},
		{"cast with a type modifier", "SELECT CAST(x AS nextval(10)) FROM t", false},

		// WITH lists that end before a call.
		{"call after a parenthesised main query",
			"WITH a AS (SELECT 1) (SELECT 1) ORDER BY 1, nextval('s')", true},
		{"call after WITH ORDINALITY",
			"SELECT * FROM unnest(ARRAY[1]) WITH ORDINALITY, pg_terminate_backend(0)", true},
		{"call in the main query", "WITH a AS (SELECT 1) SELECT 1, nextval('s')", true},
		{"call in a CTE body", "WITH a AS (SELECT nextval('s')) SELECT * FROM a", true},
		{"call in a CTE body under explain options",
			"EXPLAIN (ANALYZE) WITH a AS (SELECT nextval('s')) SELECT * FROM a", true},
		{"call after a second statement",
			"WITH a AS (SELECT 1) SELECT 1; SELECT nextval('s')", true},
		{"call after a CTE name and a dot", "WITH a.nextval(1) SELECT 1", true},
		{"call where a CTE name is expected", "WITH (nextval('s')) SELECT 1", true},
		{"quoted with is not a keyword", `SELECT 1 AS "with", nextval('s')`, true},

		// WITH that is not followed by a CTE definition (issue #646
		// review): the list must close, so the later call is found.
		{"with as a column label",
			"SELECT 1 AS with FROM pg_class, pg_terminate_backend(1)", true},
		{"with as a column label, chained",
			"SELECT 1 AS with FROM pg_class, pg_class c2, pg_terminate_backend(1)", true},
		{"with as a column label, in ORDER BY",
			"SELECT 1 AS with FROM pg_class ORDER BY relname, pg_terminate_backend(1)", true},
		{"with as a column label, in a subquery",
			"SELECT * FROM (SELECT 1 AS with FROM pg_class, pg_terminate_backend(1)) s", true},
		{"with as a column label, quoted call",
			`SELECT 1 AS with FROM pg_class, "pg_terminate_backend"(1)`, true},
		{"WITH TIME ZONE",
			"SELECT now()::timestamp WITH TIME ZONE, pg_terminate_backend(1)", true},
		{"CTE column list holding a literal",
			"WITH pg_terminate_backend(1) AS (SELECT 1) SELECT 1", true},
		{"CTE column list without AS", "WITH nextval(n) SELECT 1", true},
		{"CTE AS without a body", "WITH nextval(n) AS SELECT 1", true},
		{"CTE NOT without MATERIALIZED", "WITH nextval(n) AS NOT (SELECT 1) SELECT 1", true},
		{"CTE with a quoted AS", `WITH nextval(n) "as" (SELECT 1) SELECT 1`, true},
		{"CTE with a quoted MATERIALIZED",
			`WITH nextval(n) AS "materialized" (SELECT 1) SELECT 1`, true},
		{"quoted recursive is a CTE name",
			`WITH "recursive" pg_notify(n) AS (SELECT 1) SELECT 1`, true},
		{"unterminated CTE column list", "WITH nextval(n", true},
		{"CTE name at the end", "WITH nextval", false},
		{"WITH after AS is not a CTE list",
			"CREATE TABLE x AS WITH nextval(n) AS (SELECT 1) SELECT * FROM nextval", true},
		{"CTE after a semicolon",
			"SELECT 1; WITH schedule(id) AS (SELECT 1) SELECT * FROM schedule", false},
		{"call after a parenthesised CTE query",
			"SELECT (WITH a AS (SELECT 1) SELECT 1), nextval('s')", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CallsSideEffectingFunctionUnderAnyReading(tt.input)
			if got != tt.calls {
				t.Errorf("CallsSideEffectingFunctionUnderAnyReading(%q) = %v, want %v",
					tt.input, got, tt.calls)
			}
		})
	}
}

// TestCallsSideEffectingFunction_Readings shows why a caller that does
// not know standard_conforming_strings must check both readings: each
// statement hides its call inside a literal under one reading only.
func TestCallsSideEffectingFunction_Readings(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		standard bool
		escape   bool
	}{
		{"hidden under the standard reading",
			`SELECT '\'' AS c, pg_reload_conf() --'`, false, true},
		{"hidden under the escape reading",
			`SELECT 'a\' AS c, pg_reload_conf() --'`, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CallsSideEffectingFunction(tt.input, StandardStrings); got != tt.standard {
				t.Errorf("standard reading = %v, want %v", got, tt.standard)
			}
			if got := CallsSideEffectingFunction(tt.input, EscapeStrings); got != tt.escape {
				t.Errorf("escape reading = %v, want %v", got, tt.escape)
			}
			if !CallsSideEffectingFunctionUnderAnyReading(tt.input) {
				t.Error("CallsSideEffectingFunctionUnderAnyReading missed the call")
			}
		})
	}
}

// TestIsSideEffectingFunctionName covers the exact names, the family
// prefixes, and names that only resemble them.
func TestIsSideEffectingFunctionName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"pg_terminate_backend", true},
		{"pg_advisory_xact_lock_shared", true},
		{"pg_try_advisory_lock", true},
		{"pg_replication_origin_session_setup", true},
		{"pg_logical_slot_peek_changes", true},
		{"pg_stat_reset_single_table_counters", true},
		{"dblink_connect_u", true},
		{"pg_stat_statements_reset", true},
		{"pg_background_detach", true},
		{"pg_background_some_future_function", true},
		{"alter_subscription_resynchronize_table", true},
		{"wait_slot_confirm_lsn", false},
		{"pg_stat_get_activity", false},
		{"pg_backend_pid", false},
		{"current_setting", false},
		{"currval", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isSideEffectingFunctionName(tt.name); got != tt.want {
			t.Errorf("isSideEffectingFunctionName(%q) = %v, want %v",
				tt.name, got, tt.want)
		}
	}
}

// TestDecodeUnicodeEscapes covers the escape forms of a U&"..."
// identifier and the inputs the decoder refuses.
func TestDecodeUnicodeEscapes(t *testing.T) {
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{`plain`, "plain", true},
		{`d\0061ta`, "data", true},
		{`\+000064ata`, "data", true},
		{`a\\b`, `a\b`, true},
		{`\00e9`, "é", true},
		{`\`, "", false},
		{`\00`, "", false},
		{`\+0000`, "", false},
		{`\zzzz`, "", false},
		{`\0000`, "", false},
		{`\DFFF`, "", false},
		{`\+110000`, "", false},
	}
	for _, tt := range tests {
		got, ok := decodeUnicodeEscapes(tt.input)
		if ok != tt.ok || got != tt.want {
			t.Errorf("decodeUnicodeEscapes(%q) = (%q, %v), want (%q, %v)",
				tt.input, got, ok, tt.want, tt.ok)
		}
	}
}

// coreFunctionMinVersion holds the server_version_num of the first
// release to ship each core function added after PostgreSQL 14, the
// oldest release CI tests against. The list keeps these names because a
// monitored server may run any version, but pg_proc on an older test
// server does not have them.
var coreFunctionMinVersion = map[string]int{
	"pg_backup_start":            150000,
	"pg_backup_stop":             150000,
	"pg_stat_force_next_flush":   150000,
	"pg_log_standby_snapshot":    160000,
	"pg_sync_replication_slots":  170000,
	"pg_restore_relation_stats":  180000,
	"pg_clear_relation_stats":    180000,
	"pg_restore_attribute_stats": 180000,
	"pg_clear_attribute_stats":   180000,
}

// TestCoreFunctionMinVersion_NamesAreCore keeps coreFunctionMinVersion
// from naming a function no core family lists, which would hide nothing
// but would go stale silently.
func TestCoreFunctionMinVersion_NamesAreCore(t *testing.T) {
	core := map[string]bool{}
	for _, family := range sideEffectingFunctionFamilies {
		if family.core {
			for _, name := range family.names {
				core[name] = true
			}
		}
	}
	for name := range coreFunctionMinVersion {
		if !core[name] {
			t.Errorf("coreFunctionMinVersion names %q, which no core family lists", name)
		}
	}
}

// TestSideEffectingFunctions_CoreNamesExist checks every name in a core
// family against pg_proc on the test server, so that a misspelled name,
// which would never match a real call, fails here. A name newer than
// the server under test is skipped; it is checked on servers that have
// it.
func TestSideEffectingFunctions_CoreNamesExist(t *testing.T) {
	conn := connectSideEffectTestDB(t)
	ctx := context.Background()

	var serverVersion int
	if err := conn.QueryRow(ctx,
		"SELECT current_setting('server_version_num')::int").Scan(&serverVersion); err != nil {
		t.Fatalf("failed to read server_version_num: %v", err)
	}

	for _, family := range sideEffectingFunctionFamilies {
		if !family.core {
			continue
		}
		for _, name := range family.names {
			if minVersion, ok := coreFunctionMinVersion[name]; ok && serverVersion < minVersion {
				t.Logf("skipping %q: needs server_version_num %d, server is %d",
					name, minVersion, serverVersion)
				continue
			}
			var exists bool
			if err := conn.QueryRow(ctx,
				"SELECT EXISTS (SELECT 1 FROM pg_proc WHERE proname = $1)",
				name).Scan(&exists); err != nil {
				t.Fatalf("failed to look up %q: %v", name, err)
			}
			if !exists {
				t.Errorf("%s function %q is not in pg_proc", family.family, name)
			}
		}
	}
}

// TestSideEffectingFunctions_SpellingsMatchPostgres runs each spelling
// of a call against a real PostgreSQL server, in a transaction that is
// rolled back, and requires both that PostgreSQL accepts it as a call
// and that the classifier calls it a write. It pins the tokenizer's
// model of names, quoting and escapes to what the server does.
func TestSideEffectingFunctions_SpellingsMatchPostgres(t *testing.T) {
	conn := connectSideEffectTestDB(t)
	ctx := context.Background()

	statements := []string{
		"SELECT pg_advisory_xact_lock(646)",
		"SELECT PG_ADVISORY_XACT_LOCK(646)",
		"SELECT pg_catalog . pg_advisory_xact_lock (646)",
		`SELECT "pg_catalog"."pg_advisory_xact_lock"(646)`,
		`SELECT U&"pg\005fadvisory\005fxact\005flock"(646)`,
		`SELECT U&"pg\+00005fadvisory_xact_lock"(646)`,
		"SELECT pg_advisory_xact_lock -- c\n(646)",
		"SELECT pg_advisory_xact_lock/* a /* b */ c */(646)",
		"SELECT pg_catalog/* a */./* b */pg_advisory_xact_lock(646)",
		"SELECT set_config('work_mem', '8MB', true)",
		"SELECT * FROM ROWS FROM (pg_notify('issue646', 'x'))",
		"SELECT query_to_xml('SELECT 1', true, false, '')",
		// Field selection, which calls the function with no parenthesis.
		"SELECT (646::bigint).pg_advisory_xact_lock",
		`SELECT (646::bigint)./**/"pg_advisory_xact_lock"`,
		`SELECT (646::bigint).U&"pg\005fadvisory\005fxact\005flock"`,
		"SELECT ((646::bigint)) -- c\n.pg_advisory_xact_lock",
		"SELECT p.pg_advisory_xact_lock FROM unnest(ARRAY[646::bigint]) p",
		"WITH a AS (SELECT 1) (SELECT 1) ORDER BY 1, pg_try_advisory_xact_lock(646)",
		"SELECT * FROM unnest(ARRAY[1]) WITH ORDINALITY, pg_advisory_xact_lock(646)",
		// WITH as a column label, which must not open a CTE list.
		"SELECT 1 AS with FROM pg_class, pg_advisory_xact_lock(646) LIMIT 1",
		"SELECT 1 AS with FROM pg_class, pg_class c2, pg_advisory_xact_lock(646) LIMIT 1",
		"SELECT 1 AS with FROM pg_class ORDER BY relname, pg_try_advisory_xact_lock(646) LIMIT 1",
		"SELECT now()::timestamp WITH TIME ZONE, pg_advisory_xact_lock(646)",
	}

	for _, stmt := range statements {
		t.Run(stmt, func(t *testing.T) {
			if !CallsSideEffectingFunctionUnderAnyReading(stmt) {
				t.Errorf("no side-effecting call found in %q", stmt)
			}
			tx, err := conn.Begin(ctx)
			if err != nil {
				t.Fatalf("failed to begin: %v", err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			// The simple protocol sends the text as written, as the
			// workbench does.
			if _, err := tx.Conn().PgConn().Exec(ctx, stmt).ReadAll(); err != nil {
				t.Fatalf("PostgreSQL rejected %q: %v", stmt, err)
			}
		})
	}
}

// TestFieldSelection_NoCatalogNameMatches checks that no relation or
// column in the system catalogs or information_schema has a name that
// field selection treats as a call, so that a monitoring query such as
// SELECT s.some_column FROM pg_catalog.some_view s is never taken for
// a write. A name that fails here belongs in notFieldSelectable.
func TestFieldSelection_NoCatalogNameMatches(t *testing.T) {
	conn := connectSideEffectTestDB(t)
	ctx := context.Background()

	rows, err := conn.Query(ctx, `
		SELECT c.relname FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname IN ('pg_catalog', 'information_schema')
		  AND c.relkind IN ('r', 'v', 'm', 'p', 'f')
		UNION
		SELECT a.attname FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname IN ('pg_catalog', 'information_schema')
		  AND c.relkind IN ('r', 'v', 'm', 'p', 'f')
		  AND a.attnum > 0 AND NOT a.attisdropped`)
	if err != nil {
		t.Fatalf("failed to list catalog names: %v", err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("failed to read catalog names: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("no catalog names found")
	}
	for _, name := range names {
		if isFieldSelectedFunctionName(name) {
			t.Errorf("catalog name %q is taken for a field-selected call", name)
		}
	}
}

// connectSideEffectTestDB connects to the test database, skipping the
// test when none is configured.
func connectSideEffectTestDB(t *testing.T) *pgx.Conn {
	t.Helper()
	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping database test")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}
