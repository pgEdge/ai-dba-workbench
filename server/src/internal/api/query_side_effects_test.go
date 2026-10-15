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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// TestIsReadOnlyStatement_SideEffectingFunctions pins issue #646: a
// query that calls a side-effecting function is a write, however the
// call is written, whilst the names appearing as text are not calls.
func TestIsReadOnlyStatement_SideEffectingFunctions(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		readOnly bool
	}{
		// One member of each family.
		{"terminate backend", "SELECT pg_terminate_backend(1234)", false},
		{"cancel backend", "SELECT pg_cancel_backend(pid) FROM pg_stat_activity", false},
		{"reload conf", "SELECT pg_reload_conf()", false},
		{"rotate logfile", "SELECT pg_rotate_logfile()", false},
		{"log memory contexts", "SELECT pg_log_backend_memory_contexts(1)", false},
		{"set_config", "SELECT set_config('work_mem', '1GB', false)", false},
		{"restore point", "SELECT pg_create_restore_point('x')", false},
		{"switch wal", "SELECT pg_switch_wal()", false},
		{"backup start", "SELECT pg_backup_start('x')", false},
		{"legacy backup start", "SELECT pg_start_backup('x')", false},
		{"legacy switch xlog", "SELECT pg_switch_xlog()", false},
		{"promote", "SELECT pg_promote()", false},
		{"replay pause", "SELECT pg_wal_replay_pause()", false},
		{"legacy replay pause", "SELECT pg_xlog_replay_pause()", false},
		{"standby snapshot", "SELECT pg_log_standby_snapshot()", false},
		{"create slot", "SELECT pg_create_logical_replication_slot('s', 'pgoutput')", false},
		{"drop slot", "SELECT pg_drop_replication_slot('s')", false},
		{"slot get changes", "SELECT * FROM pg_logical_slot_get_changes('s', NULL, NULL)", false},
		{"origin create", "SELECT pg_replication_origin_create('o')", false},
		{"advisory lock", "SELECT pg_advisory_lock(1)", false},
		{"try advisory xact lock", "SELECT pg_try_advisory_xact_lock(1)", false},
		{"advisory unlock all", "SELECT pg_advisory_unlock_all()", false},
		{"stat reset", "SELECT pg_stat_reset()", false},
		{"stat reset shared", "SELECT pg_stat_reset_shared('bgwriter')", false},
		{"restore relation stats", "SELECT pg_restore_relation_stats('relation', 't')", false},
		{"brin summarize", "SELECT brin_summarize_new_values('i')", false},
		{"import collations", "SELECT pg_import_system_collations('pg_catalog')", false},
		{"xid assignment", "SELECT txid_current()", false},
		{"notify", "SELECT pg_notify('c', 'p')", false},
		{"nextval", "SELECT nextval('s')", false},
		{"setval", "SELECT setval('s', 1)", false},
		{"large object unlink", "SELECT lo_unlink(1234)", false},
		{"large object write", "SELECT lowrite(0, 'x')", false},
		{"query_to_xml runs text", "SELECT query_to_xml('SELECT 1', true, false, '')", false},
		{"ts_stat runs text", "SELECT * FROM ts_stat('SELECT v FROM t')", false},
		{"ts_rewrite runs text", "SELECT ts_rewrite('a'::tsquery, 'SELECT t, s FROM a')", false},
		{"dblink exec", "SELECT dblink_exec('dbname=x', 'DELETE FROM t')", false},
		{"dblink", "SELECT * FROM dblink('dbname=x', 'SELECT 1') AS t(a int)", false},
		{"pg_stat_statements reset", "SELECT pg_stat_statements_reset()", false},
		{"buffercache evict", "SELECT pg_buffercache_evict(1)", false},
		{"pg_surgery", "SELECT heap_force_kill('t', ARRAY['(0,1)']::tid[])", false},
		{"adminpack write", "SELECT pg_file_write('f', 'x', false)", false},
		{"spock unqualified", "SELECT replicate_ddl('DROP TABLE t')", false},
		{"spock qualified", "SELECT spock.sub_drop('s')", false},
		{"spock schema any function", "SELECT spock.some_future_function()", false},
		{"pglogical schema", "SELECT pglogical.drop_node('n')", false},
		{"cron schema", "SELECT cron.schedule('* * * * *', 'VACUUM')", false},

		// Every form of name a call can take.
		{"upper case", "SELECT PG_TERMINATE_BACKEND(1)", false},
		{"mixed case", "select Pg_Terminate_Backend(1)", false},
		{"schema qualified", "SELECT pg_catalog.pg_terminate_backend(1)", false},
		{"schema qualified with spaces", "SELECT pg_catalog . pg_terminate_backend (1)", false},
		{"quoted name", `SELECT "pg_terminate_backend"(1)`, false},
		{"quoted schema and name", `SELECT "pg_catalog"."pg_terminate_backend"(1)`, false},
		{"quoted upper case name", `SELECT "PG_TERMINATE_BACKEND"(1)`, false},
		{"unicode escaped name", `SELECT U&"pg\005fterminate\005fbackend"(1)`, false},
		{"unicode escaped name lower prefix", `SELECT u&"\0070g_reload_conf"()`, false},
		{"six digit unicode escape", `SELECT U&"pg\+00005freload_conf"()`, false},
		{"unicode escaped spock schema", `SELECT U&"sp\006fck".sub_drop('s')`, false},
		{"quoted spock schema", `SELECT "spock"."sub_drop"('s')`, false},
		{"three part name", "SELECT db.spock.sub_drop('s')", false},
		{"whitespace before paren", "SELECT pg_reload_conf\n\t ()", false},
		{"line comment before paren", "SELECT pg_reload_conf -- x\n()", false},
		{"block comment before paren", "SELECT pg_reload_conf/* x */()", false},
		{"nested block comment before paren", "SELECT pg_reload_conf/* a /* b */ c */()", false},
		{"comment around the dot", "SELECT spock/* a */./* b */sub_drop('s')", false},
		{"digit glued to the name", "SELECT 1pg_reload_conf()", false},
		{"hex literal glued to the name", "SELECT 0xApg_reload_conf()", false},
		{"in a FROM clause", "SELECT * FROM pg_terminate_backend(1) AS r", false},
		{"in ROWS FROM", "SELECT * FROM ROWS FROM (pg_reload_conf())", false},
		{"in a subquery", "SELECT (SELECT pg_reload_conf())", false},
		{"in a WHERE clause", "SELECT pid FROM pg_stat_activity WHERE pg_cancel_backend(pid)", false},
		{"in a CTE", "WITH t AS (SELECT pg_reload_conf()) SELECT * FROM t", false},
		{"lateral", "SELECT * FROM pg_stat_activity a, LATERAL (SELECT pg_cancel_backend(a.pid)) c", false},
		{"after a literal", "SELECT 'x', pg_reload_conf()", false},
		{"after a dollar quote", "SELECT $$x$$, pg_reload_conf()", false},
		{"after a quoted identifier", `SELECT 1 AS "a", pg_reload_conf()`, false},
		{"after an escape string", `SELECT E'\'', pg_reload_conf()`, false},
		{"explain analyze runs the call", "EXPLAIN ANALYZE SELECT pg_reload_conf()", false},
		{"explain options analyze runs the call", "EXPLAIN (ANALYZE, COSTS OFF) SELECT pg_reload_conf()", false},
		{"second statement", "SELECT 1; SELECT pg_reload_conf()", false},
		{"hidden under the escape reading",
			`SELECT '\'' AS c, pg_reload_conf() --'`, false},
		{"hidden under the standard reading",
			`SELECT 'a\' AS c, pg_reload_conf() --'`, false},

		// Fail closed.
		{"uescape", `SELECT U&"pg!005freload_conf" UESCAPE '!'()`, false},
		{"undecodable unicode escape", `SELECT U&"pg\zzzzreload"()`, false},
		{"truncated unicode escape", `SELECT U&"pg\00"()`, false},
		{"surrogate unicode escape", `SELECT U&"\D800"()`, false},
		{"out of range unicode escape", `SELECT U&"\+110000"()`, false},

		// The names as text, not calls.
		{"name in a string literal", "SELECT 'pg_terminate_backend(1)'", true},
		{"name in an escape string", `SELECT E'pg_reload_conf()\''`, true},
		{"name in a dollar quote", "SELECT $$pg_reload_conf()$$", true},
		{"name in a tagged dollar quote", "SELECT $q$pg_reload_conf()$q$", true},
		{"name in a line comment", "SELECT 1 -- pg_reload_conf()", true},
		{"name in a block comment", "SELECT 1 /* pg_reload_conf() */", true},
		{"name in a leading comment", "/* pg_reload_conf() */ SELECT 1", true},
		{"name as a column alias", `SELECT 1 AS "pg_reload_conf"`, true},
		{"name as a column", "SELECT nextval FROM t", true},
		{"name in a LIKE pattern", "SELECT query FROM pg_stat_activity WHERE query LIKE '%pg_terminate_backend(%'", true},
		{"name with a suffix", "SELECT pg_reload_confx()", true},
		{"name with a dollar suffix", "SELECT pg_reload_conf$x()", true},
		{"name with a prefix", "SELECT my_pg_reload_conf()", true},
		{"cast to regproc", "SELECT 'pg_reload_conf'::regproc", true},
		{"read-only origin lookalike", "SELECT pg_replication_origin_status FROM t", true},
		{"spock view", "SELECT * FROM spock.node", true},
		{"spock column", "SELECT spock.node.node_name FROM spock.node", true},
		{"ordinary functions", "SELECT now(), count(*), current_setting('work_mem') FROM t", true},
		{"pg_backend_pid", "SELECT pg_backend_pid()", true},
		{"currval and lastval", "SELECT currval('s'), lastval()", true},
		{"lo_get", "SELECT lo_get(1234)", true},
		{"monitoring query", "SELECT pid, state, pg_blocking_pids(pid) FROM pg_stat_activity", true},
		{"explain without analyze plans only", "EXPLAIN SELECT pg_reload_conf()", true},
		{"unicode identifier that is not listed", `SELECT U&"d\0061ta" FROM t`, true},
		{"unicode string literal", `SELECT U&'pg\005freload_conf()'`, true},
		{"show", "SHOW work_mem", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isReadOnlyStatement(tt.input); got != tt.readOnly {
				t.Errorf("isReadOnlyStatement(%q) = %v, want %v",
					tt.input, got, tt.readOnly)
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

// TestSideEffectingFunction_RequiresConfirmation proves that the query
// endpoint asks for confirmation before running a SELECT that calls a
// side-effecting function, and lists that statement alone as the write.
// The handler answers before it reaches the nil datastore.
func TestSideEffectingFunction_RequiresConfirmation(t *testing.T) {
	handler := newTestConnectionHandlerWithRBAC(t)

	query := "SELECT 1; SELECT pg_terminate_backend(pid) FROM pg_stat_activity"
	body, err := json.Marshal(queryRequest{Query: query})
	if err != nil {
		t.Fatalf("failed to encode the request: %v", err)
	}
	req := withSuperuser(httptest.NewRequest(http.MethodPost,
		"/api/v1/connections/1/query", bytes.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.executeQuery(rec, req, 1)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %q)",
			rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp multiQueryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resp.RequiresConfirmation {
		t.Fatal("expected requires_confirmation for pg_terminate_backend")
	}
	if len(resp.WriteStatements) != 1 ||
		!strings.Contains(resp.WriteStatements[0], "pg_terminate_backend") {
		t.Errorf("write statements = %q, want the pg_terminate_backend statement",
			resp.WriteStatements)
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
			if isReadOnlyStatement(stmt) {
				t.Errorf("classifier calls %q read-only", stmt)
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
