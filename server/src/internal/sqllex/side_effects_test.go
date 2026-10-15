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

// TestSideEffectingFunctions_CoreNamesExist checks every name in a core
// family against pg_proc on the test server, so that a misspelled name,
// which would never match a real call, fails here.
func TestSideEffectingFunctions_CoreNamesExist(t *testing.T) {
	conn := connectSideEffectTestDB(t)
	ctx := context.Background()

	for _, family := range sideEffectingFunctionFamilies {
		if !family.core {
			continue
		}
		for _, name := range family.names {
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
