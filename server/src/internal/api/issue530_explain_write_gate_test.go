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
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// issue530Payload is the exact request body from issue #530: an EXPLAIN
// ANALYZE that really executes a DELETE, with a quoted $N-shaped literal
// that used to push it onto the simple-protocol path and past the write
// gate.
const issue530Payload = `{"query": "EXPLAIN ANALYZE DELETE FROM t WHERE name = '$1'"}`

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
			if got := isReadOnlyStatement(tt.input); got != tt.readOnly {
				t.Errorf("isReadOnlyStatement(%q) = %v, want %v",
					tt.input, got, tt.readOnly)
			}
		})
	}
}

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

// TestIssue530_ExplainAnalyzeDeleteRequiresConfirmation proves the
// issue's payload is now classified as a write, so an unconfirmed caller
// gets the confirmation prompt instead of a silent DELETE. The handler
// answers before it reaches the nil datastore.
func TestIssue530_ExplainAnalyzeDeleteRequiresConfirmation(t *testing.T) {
	handler := newTestConnectionHandlerWithRBAC()

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/connections/1/query", strings.NewReader(issue530Payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.executeQuery(rec, req, 1)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %q)",
			rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp multiQueryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v (body %q)", err, rec.Body.String())
	}
	if !resp.RequiresConfirmation {
		t.Fatal("expected requires_confirmation for EXPLAIN ANALYZE DELETE")
	}
	if len(resp.WriteStatements) != 1 {
		t.Fatalf("write statements = %v, want 1", resp.WriteStatements)
	}
	if !strings.Contains(resp.WriteStatements[0], "DELETE FROM t") {
		t.Errorf("write statement = %q, want the EXPLAIN ANALYZE DELETE",
			resp.WriteStatements[0])
	}
}

// TestIssue530_ExplainAnalyzeDeleteRequiresWriteAccess proves that a
// caller with read-only access to the connection is refused even when it
// confirms, so the payload can no longer reach execution through the
// simple-protocol branch.
func TestIssue530_ExplainAnalyzeDeleteRequiresWriteAccess(t *testing.T) {
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

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body %q)",
			rec.Code, http.StatusForbidden, rec.Body.String())
	}
	want := "Permission denied: you do not have write access to this connection"
	var resp ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode error: %v (body %q)", err, rec.Body.String())
	}
	if resp.Error != want {
		t.Errorf("error = %q, want %q", resp.Error, want)
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

// TestIssue530_SimpleProtocolRunsInsideReadOnlyTransaction is the
// backstop that holds when classification does not: a statement that classification calls read-only but
// that actually writes must be refused by PostgreSQL, because the
// simple-protocol branch now runs inside a read-only transaction.
func TestIssue530_SimpleProtocolRunsInsideReadOnlyTransaction(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	connID := seedQueryExecConnection(t, pool, target, target.host, target.port)

	if _, err := pool.Exec(context.Background(), issue530ProbeSchema); err != nil {
		t.Fatalf("failed to create the probe schema: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), issue530ProbeTeardown)
	}()

	// The write comes first so that it runs before the EXPLAIN, which
	// PostgreSQL rejects for its unbound placeholder; the EXPLAIN is
	// only there to route the request onto the simple-protocol branch.
	rec := postQuery(t, h, connID,
		`{"query":"SELECT issue530_probe_write(); EXPLAIN SELECT 1 WHERE 1 = $1;"}`)

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
		readOnly  bool
		failAt    string
		run       func(context.Context, string) statementResult
		wantSQL   []string
		wantCount int
		wantLast  string
	}{
		{
			name: "read-only run commits", readOnly: true, run: okResult,
			wantSQL:   []string{"BEGIN", "SET TRANSACTION READ ONLY", "COMMIT"},
			wantCount: 2,
		},
		{
			name: "statement failure rolls back", readOnly: true, run: failResult,
			wantSQL:   []string{"BEGIN", "SET TRANSACTION READ ONLY", "ROLLBACK"},
			wantCount: 1, wantLast: "SELECT 1",
		},
		{
			name: "begin failure stops before any statement", readOnly: true,
			failAt: "BEGIN", run: okResult,
			wantSQL: []string{"BEGIN"}, wantCount: 1, wantLast: "BEGIN",
		},
		{
			name: "read-only failure rolls back", readOnly: true,
			failAt: "SET TRANSACTION READ ONLY", run: okResult,
			wantSQL:   []string{"BEGIN", "SET TRANSACTION READ ONLY", "ROLLBACK"},
			wantCount: 1, wantLast: "SET TRANSACTION READ ONLY",
		},
		{
			name: "commit failure rolls back and is reported", readOnly: true,
			failAt: "COMMIT", run: okResult,
			wantSQL:   []string{"BEGIN", "SET TRANSACTION READ ONLY", "COMMIT", "ROLLBACK"},
			wantCount: 3, wantLast: "COMMIT",
		},
		{
			name: "confirmed write runs without a transaction", readOnly: false,
			run: okResult, wantSQL: nil, wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seen []string
			results := runSimpleStatementsWith(context.Background(),
				failOn(tt.failAt, &seen), tt.run,
				[]string{"SELECT 1", "SELECT 2"}, 1, tt.readOnly)

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

// TestIssue530_SkipQuoted covers the literal scanner directly, including
// the prefixed forms and the unterminated cases that run to the end.
func TestIssue530_SkipQuoted(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"plain literal", "'abc' rest", 5},
		{"doubled quote", "'a''b' rest", 6},
		{"quoted identifier", `"a b" rest`, 5},
		{"doubled double quote", `"a""b" rest`, 6},
		{"escape literal", `E'\'' rest`, 5},
		{"escape literal lowercase", `e'\'' rest`, 5},
		{"unicode literal", `U&'ab' rest`, 6},
		{"unicode identifier", `U&"ab" rest`, 6},
		{"bit string", "B'10' rest", 5},
		{"hex string", "x'ff' rest", 5},
		{"national string", "N'ab' rest", 5},
		{"unterminated literal", "'abc", 4},
		{"not a literal prefix", "abc", 1},
		{"prefix letter at the end", "E", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := skipQuoted(tt.input, 0); got != tt.want {
				t.Errorf("skipQuoted(%q, 0) = %d, want %d",
					tt.input, got, tt.want)
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
			if got := isReadOnlyStatement(tt.input); got != tt.readOnly {
				t.Errorf("isReadOnlyStatement(%q) = %v, want %v",
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

// TestIssue530_MaskNonCode pins the masking helper itself: every masked
// region becomes spaces and the result keeps the original byte length.
func TestIssue530_MaskNonCode(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain code is untouched", "SELECT a FROM t", "SELECT a FROM t"},
		{"string literal", "SELECT 'into' FROM t", "SELECT        FROM t"},
		{"quoted identifier", `SELECT "into" FROM t`, "SELECT        FROM t"},
		{"line comment", "SELECT a -- into\nFROM t", "SELECT a        \nFROM t"},
		{"block comment", "SELECT /* into */ a", "SELECT            a"},
		{"unterminated literal", "SELECT 'into", "SELECT      "},
		{"dollar-quoted literal", "SELECT $q$'$q$ AS c", "SELECT " + strings.Repeat(" ", 7) + " AS c"},
		{"anonymous dollar quote", "SELECT $$x$$", "SELECT      "},
		{"unterminated dollar quote", "SELECT $q$ into", "SELECT " + strings.Repeat(" ", 8)},
		{"dollar continuing an identifier", "SELECT a$q$ FROM t", "SELECT a$q$ FROM t"},
		{"placeholder is code", "SELECT $1", "SELECT $1"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maskNonCode(tt.input)
			if got != tt.want {
				t.Errorf("maskNonCode(%q) = %q, want %q",
					tt.input, got, tt.want)
			}
			if len(got) != len(tt.input) {
				t.Errorf("maskNonCode(%q) changed length: %d, want %d",
					tt.input, len(got), len(tt.input))
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
