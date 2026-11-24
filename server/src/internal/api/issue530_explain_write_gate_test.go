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
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// issue530Payload is the exact request body from issue #530: an EXPLAIN
// ANALYZE that really executes a DELETE, with a quoted $N-shaped literal
// that used to push it onto the simple-protocol path and past the write
// gate.
const issue530Payload = `{"query": "EXPLAIN ANALYZE DELETE FROM t WHERE name = '$1'"}`

// TestIssue530_ContainsDollarParam covers the quoting-aware placeholder
// scan: only a $N that is real SQL counts.
func TestIssue530_ContainsDollarParam(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"bare placeholder", "SELECT * FROM t WHERE id = $1", true},
		{"second placeholder", "SELECT $2", true},
		{"no placeholder", "SELECT * FROM t", false},
		{"dollar zero is not a placeholder", "SELECT $0", false},
		{"single-quoted literal", "SELECT * FROM t WHERE name = '$1'", false},
		{"double-quoted identifier", `SELECT "$1" FROM t`, false},
		{"line comment", "SELECT 1 -- $1", false},
		{"line comment then real placeholder", "SELECT 1 -- $1\n, $2", true},
		{"block comment", "SELECT 1 /* $1 */", false},
		{"nested block comment", "SELECT 1 /* a /* $1 */ b */", false},
		{"tagged dollar quote", "SELECT $tag$ $1 $tag$", false},
		{"anonymous dollar quote", "SELECT $$ $1 $$", false},
		{"placeholder after a dollar-quoted body", "SELECT $$ body $$, $1", true},
		{"escaped quote inside a literal", "SELECT 'it''s $1'", false},
		{"unterminated single quote", "SELECT 'oops $1", false},
		{"unterminated double quote", `SELECT "oops $1`, false},
		{"unterminated block comment", "SELECT /* oops $1", false},
		{"unterminated dollar quote", "SELECT $tag$ oops $1", false},
		{"trailing dollar", "SELECT 1 $", false},
		{"empty", "", false},
		{"backslash-escaped quote in an E literal",
			`EXPLAIN SELECT E'\'' , $1`, true},
		{"placeholder text inside an E literal", `SELECT E'\'$1'`, false},
		{"E literal with a trailing backslash", `SELECT E'oops\`, false},
		{"unicode literal then a placeholder", `SELECT U&'\0024', $1`, true},
		{"placeholder text inside a unicode identifier",
			`SELECT U&"$1" FROM t`, false},
		{"bit-string literal then a placeholder", "SELECT B'1010', $1", true},
		{"hex literal then a placeholder", "SELECT X'ff', $1", true},
		{"national literal then a placeholder", "SELECT N'x', $1", true},
		{"identifier ending in e is not an E literal",
			"SELECT code'$1", false},
		{"lone e before a placeholder", "SELECT e, $1", true},
		{"quote inside a dollar quote then a placeholder",
			"SELECT $q$'$q$, $1", true},
		{"dollar continuing an identifier is not a dollar quote",
			"SELECT a$q$ FROM t WHERE id = $1 -- $q$", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsDollarParam(tt.input); got != tt.want {
				t.Errorf("containsDollarParam(%q) = %v, want %v",
					tt.input, got, tt.want)
			}
		})
	}
}

// TestIssue530_NeedsSimpleProtocol covers the combination of the EXPLAIN
// prefix test and the quoting-aware placeholder scan.
func TestIssue530_NeedsSimpleProtocol(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  bool
	}{
		{"explain with a placeholder",
			[]string{"EXPLAIN SELECT * FROM t WHERE id = $1"}, true},
		{"explain without a placeholder", []string{"EXPLAIN SELECT 1"}, false},
		{"explain with a quoted placeholder-shaped literal",
			[]string{"EXPLAIN ANALYZE DELETE FROM t WHERE name = '$1'"}, false},
		{"select with a placeholder is not an explain",
			[]string{"SELECT * FROM t WHERE id = $1"}, false},
		{"second statement is the explain",
			[]string{"SELECT 1", "EXPLAIN SELECT $1"}, true},
		{"explain with a leading comment",
			[]string{"-- plan\nEXPLAIN SELECT $1"}, true},
		{"explainable identifier", []string{"EXPLAINABLE SELECT $1"}, false},
		{"no statements", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := needsSimpleProtocol(tt.input); got != tt.want {
				t.Errorf("needsSimpleProtocol(%v) = %v, want %v",
					tt.input, got, tt.want)
			}
		})
	}
}

// TestIssue530_ExplainAnalyzeDeleteRefusedAsWrite proves the
// issue's payload is now classified as a write, so an unconfirmed caller
// gets a 409 refusal instead of a silent DELETE. The handler
// answers before it reaches the nil datastore.
func TestIssue530_ExplainAnalyzeDeleteRefusedAsWrite(t *testing.T) {
	handler := newTestConnectionHandlerWithRBAC(t)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/connections/1/query", strings.NewReader(issue530Payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.executeQuery(rec, req, 1)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (body %q)",
			rec.Code, http.StatusConflict, rec.Body.String())
	}

	var resp writeRefusedResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v (body %q)", err, rec.Body.String())
	}
	if !resp.RequiresAction {
		t.Fatal("expected requires_action for EXPLAIN ANALYZE DELETE")
	}
	if len(resp.WriteStatements) != 1 {
		t.Fatalf("write statements = %v, want 1", resp.WriteStatements)
	}
	if !strings.Contains(resp.WriteStatements[0], "DELETE FROM t") {
		t.Errorf("write statement = %q, want the EXPLAIN ANALYZE DELETE",
			resp.WriteStatements[0])
	}
}

// TestIssue530_ExplainAnalyzeDeleteRefusedForReadOnlyCaller proves that a
// caller with read-only access to the connection is refused even when it
// confirms, so the payload can no longer reach execution through the
// simple-protocol branch.
func TestIssue530_ExplainAnalyzeDeleteRefusedForReadOnlyCaller(t *testing.T) {
	store, cleanup := createTestAuthStoreForAlertOverrides(t)
	defer cleanup()

	const connID = 530
	userID := newGroupGrantedUser(t, store, "issue530_reader", connID,
		auth.AccessLevelRead)

	handler := NewConnectionHandlerWithSecurity(nil, store,
		auth.NewRBACChecker(store), false, nil, nil)

	ctx := context.WithValue(context.Background(), auth.UserIDContextKey, userID)
	ctx = context.WithValue(ctx, auth.IsSuperuserContextKey, false)
	ctx = context.WithValue(ctx, auth.UsernameContextKey, "issue530_reader")

	body := `{"query": "EXPLAIN ANALYZE DELETE FROM t WHERE name = '$1'",` +
		`"confirmed": true}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/connections/1/query", strings.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.executeQuery(rec, req, connID)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (body %q)",
			rec.Code, http.StatusConflict, rec.Body.String())
	}
	var resp writeRefusedResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode refusal: %v (body %q)", err, rec.Body.String())
	}
	if !resp.RequiresAction {
		t.Errorf("requires_action = false, want true (body %q)", rec.Body.String())
	}
}

// issue530ProbeSchema creates a table and a function that writes to it.
// The function call is classified read-only because the statement starts
// with SELECT, which is exactly the case the read-only transaction has
// to catch on the simple-protocol path.
const issue530ProbeSchema = `
CREATE TABLE IF NOT EXISTS issue530_probe (id int);
CREATE OR REPLACE FUNCTION issue530_probe_write() RETURNS int
    LANGUAGE sql AS 'INSERT INTO issue530_probe VALUES (1) RETURNING id';
`

const issue530ProbeTeardown = `
DROP FUNCTION IF EXISTS issue530_probe_write();
DROP TABLE IF EXISTS issue530_probe;
`

// TestIssue530_QueriesRunInsideReadOnlyTransaction is the backstop that
// holds when classification does not: a statement that classification
// calls read-only but that actually writes must be refused by
// PostgreSQL, on both the extended-protocol branch and the
// simple-protocol branch, because each runs inside a read-only
// transaction. With every write now routed through proposed actions
// (issue #774), this transaction is all that stands between /query and
// a write classification misses.
func TestIssue530_QueriesRunInsideReadOnlyTransaction(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	connID := seedQueryExecConnection(t, pool, target, target.host, target.port)

	if _, err := pool.Exec(context.Background(), issue530ProbeSchema); err != nil {
		t.Fatalf("failed to create the probe schema: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), issue530ProbeTeardown)
	}()

	tests := []struct {
		name string
		body string
	}{
		// The extended protocol, the path every ordinary query takes.
		{"extended protocol", `{"query":"SELECT issue530_probe_write();"}`},
		// The write comes first so that it runs before the EXPLAIN,
		// which PostgreSQL rejects for its unbound placeholder; the
		// EXPLAIN is only there to route the request onto the
		// simple-protocol branch.
		{"simple protocol", `{"query":"SELECT issue530_probe_write(); EXPLAIN SELECT 1 WHERE 1 = $1;"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := postQuery(t, h, connID, tt.body)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d (body %q)",
					rec.Code, http.StatusOK, rec.Body.String())
			}
			resp := decodeMultiQuery(t, rec)
			if len(resp.Results) != 1 {
				t.Fatalf("results = %d, want 1 (execution stops at the refusal)",
					len(resp.Results))
			}
			if !strings.Contains(strings.ToLower(resp.Results[0].Error), "read-only transaction") {
				t.Errorf("error = %q, want a read-only transaction refusal",
					resp.Results[0].Error)
			}

			var rows int
			if err := pool.QueryRow(context.Background(),
				"SELECT count(*) FROM issue530_probe").Scan(&rows); err != nil {
				t.Fatalf("failed to count the probe rows: %v", err)
			}
			if rows != 0 {
				t.Errorf("probe rows = %d, want 0: the write was not refused", rows)
			}

			// The rollback must leave the pooled connection usable.
			rec = postQuery(t, h, connID, `{"query":"SELECT 1"}`)
			if rec.Code != http.StatusOK {
				t.Fatalf("follow-up status = %d, want %d (body %q)",
					rec.Code, http.StatusOK, rec.Body.String())
			}
		})
	}
}

// TestIssue530_SimpleProtocolCommitsCleanRun covers the commit at the
// end of a successful read-only simple-protocol run. GENERIC_PLAN, which
// lets an EXPLAIN keep its placeholders, needs PostgreSQL 16 or later;
// on an older server the statement errors instead and the rollback path
// runs, so the assertions only require a well-formed response.
func TestIssue530_SimpleProtocolCommitsCleanRun(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	connID := seedQueryExecConnection(t, pool, target, target.host, target.port)

	rec := postQuery(t, h, connID, `{"query":"EXPLAIN (GENERIC_PLAN) `+
		`SELECT * FROM connections WHERE id = $1"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %q)",
			rec.Code, http.StatusOK, rec.Body.String())
	}
	resp := decodeMultiQuery(t, rec)
	if len(resp.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(resp.Results))
	}
	if resp.Results[0].Error == "" && len(resp.Results[0].Rows) == 0 {
		t.Error("expected either plan rows or an error from the EXPLAIN")
	}
}

// TestIssue530_RunSimpleStatementsTransactionDiscipline covers the
// transaction control around the simple-protocol path with an injected
// executor, including the failure branches a live connection cannot
// easily be driven into.
func TestIssue530_RunSimpleStatementsTransactionDiscipline(t *testing.T) {
	boom := errors.New("control statement failed")

	// failOn returns an executor that fails on the named statement and
	// records the statements it saw.
	failOn := func(target string, seen *[]string) func(context.Context, string) error {
		return func(_ context.Context, sql string) error {
			*seen = append(*seen, sql)
			if sql == target {
				return boom
			}
			return nil
		}
	}
	okResult := func(_ context.Context, stmt string) statementResult {
		return statementResult{Query: stmt, RowCount: 1}
	}
	failResult := func(_ context.Context, stmt string) statementResult {
		return statementResult{Query: stmt, Error: "Query error: nope"}
	}

	tests := []struct {
		name      string
		failAt    string
		run       func(context.Context, string) statementResult
		wantSQL   []string
		wantCount int
		wantLast  string
	}{
		{
			name: "read-only run commits", run: okResult,
			wantSQL:   []string{"BEGIN", "SET TRANSACTION READ ONLY", "COMMIT"},
			wantCount: 2,
		},
		{
			name: "statement failure rolls back", run: failResult,
			wantSQL:   []string{"BEGIN", "SET TRANSACTION READ ONLY", "ROLLBACK"},
			wantCount: 1, wantLast: "SELECT 1",
		},
		{
			name:   "begin failure stops before any statement",
			failAt: "BEGIN", run: okResult,
			wantSQL: []string{"BEGIN"}, wantCount: 1, wantLast: "BEGIN",
		},
		{
			name:   "read-only failure rolls back",
			failAt: "SET TRANSACTION READ ONLY", run: okResult,
			wantSQL:   []string{"BEGIN", "SET TRANSACTION READ ONLY", "ROLLBACK"},
			wantCount: 1, wantLast: "SET TRANSACTION READ ONLY",
		},
		{
			name:   "commit failure rolls back and is reported",
			failAt: "COMMIT", run: okResult,
			wantSQL:   []string{"BEGIN", "SET TRANSACTION READ ONLY", "COMMIT", "ROLLBACK"},
			wantCount: 3, wantLast: "COMMIT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seen []string
			results := runSimpleStatementsWith(context.Background(),
				failOn(tt.failAt, &seen), tt.run,
				[]string{"SELECT 1", "SELECT 2"}, 1)

			if len(results) != tt.wantCount {
				t.Fatalf("results = %d, want %d", len(results), tt.wantCount)
			}
			if strings.Join(seen, ";") != strings.Join(tt.wantSQL, ";") {
				t.Errorf("control statements = %v, want %v", seen, tt.wantSQL)
			}
			if tt.wantLast != "" {
				last := results[len(results)-1]
				if last.Query != tt.wantLast {
					t.Errorf("last result query = %q, want %q",
						last.Query, tt.wantLast)
				}
				if last.Error == "" {
					t.Error("last result carries no error")
				}
			}
		})
	}
}

// TestIssue530_RollbackSimpleLogsFailure covers the logged branch taken
// when the rollback itself fails; there is nothing else the handler can
// do about it, so the call must simply return.
func TestIssue530_RollbackSimpleLogsFailure(t *testing.T) {
	called := false
	rollbackSimple(context.Background(),
		func(context.Context, string) error {
			called = true
			return errors.New("rollback failed")
		}, 1)
	if !called {
		t.Error("rollbackSimple did not issue the rollback")
	}
}

// TestIssue530_SplitStatementsQuoting covers the statement splitter's
// handling of quoted identifiers and prefixed string literals, where a
// semicolon is part of the literal rather than a split point.
func TestIssue530_SplitStatementsQuoting(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"semicolon in a quoted identifier", `SELECT "a;b"`,
			[]string{`SELECT "a;b"`}},
		{"semicolon in an escaped E literal", `SELECT E'\';'`,
			[]string{`SELECT E'\';'`}},
		{"semicolon in a unicode literal", `SELECT U&'a;b'`,
			[]string{`SELECT U&'a;b'`}},
		{"semicolon in a unicode identifier", `SELECT U&"a;b"`,
			[]string{`SELECT U&"a;b"`}},
		{"real split after a quoted identifier", `SELECT "a;b"; SELECT 2`,
			[]string{`SELECT "a;b"`, "SELECT 2"}},
		{"dollar continuing an identifier still splits",
			"SELECT 1 AS a$q$; SELECT 2 -- $q$",
			[]string{"SELECT 1 AS a$q$", "SELECT 2 -- $q$"}},
		{"carriage return ends a line comment", "SELECT 1 -- c\r; DELETE FROM t",
			[]string{"SELECT 1 -- c", "DELETE FROM t"}},
		{"plain literal still splits after it", `SELECT 'a;b'; SELECT 2`,
			[]string{`SELECT 'a;b'`, "SELECT 2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitStatements(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("splitStatements(%q) = %q, want %q",
					tt.input, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitStatements(%q)[%d] = %q, want %q",
						tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestIssue530_SetCurrentConnectionStoresValidDatabaseName checks the
// database override on the path that saves it: a legitimate name is
// stored and echoed back, and an unknown connection is still refused
// after the name has passed validation.
func TestIssue530_SetCurrentConnectionStoresValidDatabaseName(t *testing.T) {
	ds, pool, cleanupDS := newIssue269ConnectionDatastore(t)
	defer cleanupDS()

	_, store, cleanupStore := createTestRBACHandler(t)
	defer cleanupStore()

	const owner = "issue530_session_owner"
	userID := newTestUser(t, store, owner)
	rawToken, _, err := store.AuthenticateUser(owner, "Password1234")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}
	tokenHash := auth.GetTokenHashByRawToken(rawToken)

	const connID = 7530
	seedIssue269Connection(t, pool, connID, owner, "issue530-conn")

	checker := mockSharingChecker(t, store, connID, owner, false)
	handler := NewConnectionHandlerWithSecurity(ds, store, checker, false, nil, nil)

	post := func(id int, dbName string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(CurrentConnectionRequest{
			ConnectionID: id,
			DatabaseName: &dbName,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/connections/current",
			bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = withUser(req, userID)
		req = withUsername(req, owner)
		req = withBearerRaw(req, rawToken)
		rec := httptest.NewRecorder()
		handler.setCurrentConnection(rec, req, tokenHash)
		return rec
	}

	// A name with URL-significant characters is legitimate: escaping,
	// not rejection, is what keeps it inside the connection string.
	const dbName = "sales db?x=1"
	rec := post(connID, dbName)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp CurrentConnectionResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.DatabaseName == nil || *resp.DatabaseName != dbName {
		t.Errorf("response database_name = %v, want %q", resp.DatabaseName, dbName)
	}
	session, err := store.GetConnectionSession(tokenHash)
	if err != nil {
		t.Fatalf("GetConnectionSession: %v", err)
	}
	if session == nil || session.DatabaseName == nil || *session.DatabaseName != dbName {
		t.Errorf("stored session = %+v, want database %q", session, dbName)
	}

	// A connection the owner may reach but which does not exist.
	missing := mockSharingChecker(t, store, connID+1, owner, false)
	handler = NewConnectionHandlerWithSecurity(ds, store, missing, false, nil, nil)
	if rec := post(connID+1, "postgres"); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown connection: expected 400, got %d", rec.Code)
	}
}

// TestIssue530_ContainsDollarParamEitherReading pins that a placeholder
// visible under only one reading of a plain '...' literal still counts.
func TestIssue530_ContainsDollarParamEitherReading(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"visible only under an escape reading", `SELECT '\'', $1 --'`, true},
		{"visible only under a standard reading", `SELECT '\', $1 --'`, true},
		{"hidden under both readings", `SELECT '$1'`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsDollarParam(tt.input); got != tt.want {
				t.Errorf("containsDollarParam(%q) = %v, want %v",
					tt.input, got, tt.want)
			}
		})
	}
}

// TestIssue530_StandardConformingStringsOffRefusedAsWrite proves
// that the round-four payload, which turns standard_conforming_strings
// off and then hides a DELETE CTE behind '\”, now yields the
// 409 refusal. The handler answers before it reaches the nil
// datastore.
func TestIssue530_StandardConformingStringsOffRefusedAsWrite(t *testing.T) {
	handler := newTestConnectionHandlerWithRBAC(t)

	query := `SELECT set_config('standard_conforming_strings', 'off', false); ` +
		`WITH x AS (SELECT '\'' AS c), d AS (DELETE FROM t RETURNING *) ` +
		"SELECT * FROM d --'"
	body, err := json.Marshal(queryRequest{Query: query})
	if err != nil {
		t.Fatalf("failed to encode the request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/connections/1/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.executeQuery(rec, req, 1)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (body %q)",
			rec.Code, http.StatusConflict, rec.Body.String())
	}
	var resp writeRefusedResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v (body %q)", err, rec.Body.String())
	}
	if !resp.RequiresAction {
		t.Fatal("expected requires_action for the hidden DELETE CTE")
	}
	// The set_config call is a write in its own right (issue #646), so
	// both statements are listed; the point here is that the hidden
	// DELETE is among them.
	if len(resp.WriteStatements) != 2 ||
		!strings.Contains(resp.WriteStatements[1], "DELETE FROM t") {
		t.Errorf("write statements = %q, want set_config and the WITH ... DELETE statement",
			resp.WriteStatements)
	}
}

// TestIssue530_LexerReadingsMatchPostgres runs each payload against a
// real PostgreSQL server in a read-write transaction that is rolled
// back, and requires both that PostgreSQL really deleted a row and that
// the classifier calls the payload a write. It pins the scanner's model
// of the lexer to what the server actually does, not only to the
// reading of scan.l behind it.
func TestIssue530_LexerReadingsMatchPostgres(t *testing.T) {
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
	defer func() { _ = conn.Close(ctx) }()

	tests := []struct {
		name       string
		conforming string
		statements []string
	}{
		{"backslash quote with standard_conforming_strings off", "off",
			[]string{`WITH x AS (SELECT '\'' AS c), ` +
				"d AS (DELETE FROM issue530_lex RETURNING *) SELECT * FROM d --'"}},
		{"set_config turns standard_conforming_strings off", "on",
			[]string{
				`SELECT set_config('standard_conforming_strings', 'off', false)`,
				`WITH x AS (SELECT '\'' AS c), ` +
					"d AS (DELETE FROM issue530_lex RETURNING *) SELECT * FROM d --'",
			}},
		{"backslash quote in a national literal", "off",
			[]string{`WITH x AS (SELECT N'\'' AS c), ` +
				"d AS (DELETE FROM issue530_lex RETURNING *) SELECT * FROM d --'"}},
		{"escape string continued onto the next line", "on",
			[]string{"WITH x AS (SELECT E'a'\n'\\'' AS c), " +
				"d AS (DELETE FROM issue530_lex RETURNING *) SELECT * FROM d --'"}},
		{"escape string continued after a line comment", "on",
			[]string{"WITH x AS (SELECT E'a' -- c\n  '\\'' AS c), " +
				"d AS (DELETE FROM issue530_lex RETURNING *) SELECT * FROM d --'"}},
		{"escape string continued after a carriage return", "on",
			[]string{"WITH x AS (SELECT E'a'\r'\\'' AS c), " +
				"d AS (DELETE FROM issue530_lex RETURNING *) SELECT * FROM d --'"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if isReadOnlyStatement(strings.Join(tt.statements, "; ")) {
				t.Errorf("classifier calls %q read-only", tt.statements)
			}

			tx, err := conn.Begin(ctx)
			if err != nil {
				t.Fatalf("failed to begin: %v", err)
			}
			defer func() { _ = tx.Rollback(ctx) }()

			if _, err := tx.Exec(ctx,
				"SELECT set_config('standard_conforming_strings', $1, true)",
				tt.conforming); err != nil {
				t.Fatalf("failed to set standard_conforming_strings: %v", err)
			}
			setup := []string{
				"SET LOCAL escape_string_warning = off",
				"CREATE TEMP TABLE issue530_lex (a int) ON COMMIT DROP",
				"INSERT INTO issue530_lex VALUES (1), (2)",
			}
			for _, stmt := range setup {
				if _, err := tx.Exec(ctx, stmt); err != nil {
					t.Fatalf("setup %q failed: %v", stmt, err)
				}
			}
			// Each statement goes over the simple protocol, as the
			// workbench sends it, so the server's lexer reads the text
			// exactly as written.
			for _, stmt := range tt.statements {
				if _, err := tx.Conn().PgConn().Exec(ctx, stmt).ReadAll(); err != nil {
					t.Fatalf("PostgreSQL rejected %q: %v", stmt, err)
				}
			}
			var rows int
			if err := tx.QueryRow(ctx,
				"SELECT count(*) FROM issue530_lex").Scan(&rows); err != nil {
				t.Fatalf("failed to count rows: %v", err)
			}
			if rows != 0 {
				t.Errorf("rows left = %d, want 0: PostgreSQL did not run the DELETE", rows)
			}
		})
	}
}

// TestIssue530_ClientEncodingChangeStopsReadOnlyBatch proves that a
// statement which moves the connection off UTF8 ends the batch before
// the next statement is lexed under the new encoding, on both the pgx
// path and the simple-protocol path. A direct set_config call is now
// classified as a write (issue #646), so the change is made through a
// wrapper function the classifier cannot see into, which is how a
// statement classified as read-only can still change the encoding.
func TestIssue530_ClientEncodingChangeStopsReadOnlyBatch(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	defer createSetClientEncodingWrapper(t, pool)()

	connID := seedQueryExecConnection(t, pool, target, target.host, target.port)

	tests := []struct {
		name  string
		query string
	}{
		{"pgx path",
			"SELECT issue530_set_client_encoding(false); SELECT 1"},
		{"simple-protocol path",
			"SELECT issue530_set_client_encoding(false); " +
				"EXPLAIN SELECT 1 WHERE 1 = $1"},
		{"transaction-local change",
			"SELECT issue530_set_client_encoding(true); SELECT 1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(queryRequest{Query: tt.query})
			if err != nil {
				t.Fatalf("failed to encode the request: %v", err)
			}
			rec := postQuery(t, h, connID, string(body))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d (body %q)",
					rec.Code, http.StatusOK, rec.Body.String())
			}
			resp := decodeMultiQuery(t, rec)
			if len(resp.Results) != 1 {
				t.Fatalf("results = %d, want 1 (the batch stops at the change)",
					len(resp.Results))
			}
			if resp.Results[0].Error != clientEncodingChangedError {
				t.Errorf("error = %q, want %q",
					resp.Results[0].Error, clientEncodingChangedError)
			}
		})
	}
}

// createSetClientEncodingWrapper creates issue530_set_client_encoding,
// a SQL function that switches client_encoding to SJIS. A direct
// set_config call is classified as a write (issue #646), whereas a call
// to a user-defined wrapper is not, so the tests use it to change the
// encoding from a statement the classifier treats as read-only. The
// returned function drops it, and must run before the pool closes.
func createSetClientEncodingWrapper(t *testing.T, pool *pgxpool.Pool) func() {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `CREATE OR REPLACE FUNCTION
		issue530_set_client_encoding(is_local boolean) RETURNS text
		LANGUAGE sql AS
		$$SELECT set_config('client_encoding', 'SJIS', is_local)$$`); err != nil {
		t.Fatalf("failed to create the wrapper function: %v", err)
	}
	return func() {
		_, _ = pool.Exec(context.Background(),
			"DROP FUNCTION IF EXISTS issue530_set_client_encoding(boolean)")
	}
}

// TestIssue530_ClientEncodingPinnedAtStartup proves that the query
// connection starts on UTF8 even when the database's own default says
// otherwise, because the startup parameter takes precedence over it.
func TestIssue530_ClientEncodingPinnedAtStartup(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	connID := seedQueryExecConnection(t, pool, target, target.host, target.port)

	ctx := context.Background()
	// The pool is connected to the target database, so the server names
	// it with format('%I', ...) and no SQL is assembled here.
	if _, err := pool.Exec(ctx, `DO $$ BEGIN
		EXECUTE format('ALTER DATABASE %I SET client_encoding = ''SJIS''',
			current_database());
	END $$`); err != nil {
		t.Skipf("cannot set a database default for client_encoding: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, `DO $$ BEGIN
			EXECUTE format('ALTER DATABASE %I RESET client_encoding',
				current_database());
		END $$`)
	}()

	rec := postQuery(t, h, connID, `{"query":"SHOW client_encoding"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %q)",
			rec.Code, http.StatusOK, rec.Body.String())
	}
	resp := decodeMultiQuery(t, rec)
	if len(resp.Results) != 1 || resp.Results[0].Error != "" {
		t.Fatalf("results = %+v, want one clean result", resp.Results)
	}
	if got := resp.Results[0].Rows; len(got) != 1 || got[0][0] != "UTF8" {
		t.Errorf("client_encoding = %q, want UTF8", got)
	}
}
