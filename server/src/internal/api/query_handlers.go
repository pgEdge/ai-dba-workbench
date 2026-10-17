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
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/pkg/rollback"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/sqllex"
	"github.com/pgedge/ai-workbench/server/internal/tsv"
)

// queryRequest is the JSON request body for executing a query
type queryRequest struct {
	Query        string `json:"query"`
	DatabaseName string `json:"database_name,omitempty"`
	Confirmed    bool   `json:"confirmed,omitempty"`
}

// statementResult holds the result of executing a single SQL statement.
// The Columns, Rows, RowCount, and Truncated fields are present for
// successful results and absent for error results.
type statementResult struct {
	Columns   []string   `json:"columns"`
	Rows      [][]string `json:"rows"`
	RowCount  int        `json:"row_count"`
	Truncated bool       `json:"truncated"`
	Query     string     `json:"query"`
	Error     string     `json:"error,omitempty"`
}

// multiQueryResponse is the JSON response for query execution containing
// results from one or more SQL statements
type multiQueryResponse struct {
	Results              []statementResult `json:"results,omitempty"`
	TotalStatements      int               `json:"total_statements,omitempty"`
	RequiresConfirmation bool              `json:"requires_confirmation,omitempty"`
	WriteStatements      []string          `json:"write_statements,omitempty"`
	ConfirmationMessage  string            `json:"confirmation_message,omitempty"`
}

// defaultRowLimit is the default maximum number of rows returned
const defaultRowLimit = 500

// maxRowLimit is the absolute maximum number of rows returned
const maxRowLimit = 1000

// queryTimeout is the context timeout for query execution
const queryTimeout = 30 * time.Second

// hasOnlyComments returns true when the string contains only SQL
// comments (line and block) and whitespace but no real SQL content.
func hasOnlyComments(s string) bool {
	return sqllex.HasOnlyComments(s)
}

// splitStatements splits a SQL string into individual statements by
// scanning for semicolons that are outside of quoted strings, quoted
// identifiers, dollar-quoted strings, line comments, and block comments
// (with nesting). It trims whitespace and filters out empty or
// comment-only statements. It reads a plain '...' literal as a standard
// string, which is how the statements are split for execution; the
// classifier splits under both readings (see isReadOnlyStatement).
func splitStatements(sql string) []string {
	return splitStatementsAs(sql, sqllex.StandardStrings)
}

// splitStatementsAs is splitStatements with the given reading of a plain
// '...' literal.
func splitStatementsAs(sql string, reading sqllex.StringReading) []string {
	return sqllex.SplitStatements(sql, reading)
}

// executeQuery handles POST /api/v1/connections/{id}/query
func (h *ConnectionHandler) executeQuery(w http.ResponseWriter, r *http.Request, connectionID int) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check RBAC access to this connection
	canAccess, _ := h.rbacChecker.CanAccessConnection(r.Context(), connectionID)
	if !canAccess {
		h.denialRecorder.refuseConnection(w, r, deniedActionConnectionQuery,
			connectionID, connectionAccessDenied)
		return
	}

	// Parse request body
	var req queryRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	// Validate query is present
	query := strings.TrimSpace(req.Query)
	if query == "" {
		RespondError(w, http.StatusBadRequest, "Query is required")
		return
	}

	// Validate the optional database override before it reaches the
	// connection string, and before any datastore work.
	if req.DatabaseName != "" {
		if err := database.ValidateDatabaseName(req.DatabaseName); err != nil {
			RespondError(w, http.StatusBadRequest,
				"Invalid database name: "+err.Error())
			return
		}
	}

	// Split into individual statements early so we can classify them
	// before opening a database connection.
	statements := splitStatements(query)
	if len(statements) == 0 {
		RespondError(w, http.StatusBadRequest, "Query is required")
		return
	}

	// Determine whether any statement is a write operation
	var writeStatements []string
	allReadOnly := true
	for _, stmt := range statements {
		if !isReadOnlyStatement(stmt) {
			allReadOnly = false
			writeStatements = append(writeStatements, stmt)
		}
	}

	// If write statements are present but not confirmed, return a
	// confirmation prompt so the frontend can ask the user to proceed.
	if !allReadOnly && !req.Confirmed {
		resp := multiQueryResponse{
			RequiresConfirmation: true,
			WriteStatements:      writeStatements,
			ConfirmationMessage: fmt.Sprintf(
				"This request contains %d write statement(s) that will "+
					"modify the database. Please confirm to proceed.",
				len(writeStatements)),
		}
		RespondJSON(w, http.StatusOK, resp)
		return
	}

	// For write statements, enforce RBAC write access
	if !allReadOnly {
		if !h.rbacChecker.HasWriteAccess(r.Context(), connectionID) {
			h.denialRecorder.refuseTarget(w, r, deniedActionConnectionQueryWrite,
				connectionDenialTarget(connectionID), connectionWriteDenied,
				connectionWriteDenied)
			return
		}
	}

	// Create a context with timeout for the entire operation
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()

	// Get connection details with decrypted password
	conn, password, err := h.datastore.GetConnectionWithPassword(ctx, connectionID)
	if err != nil {
		log.Printf("[ERROR] Connection not found for query (id=%d): %v", connectionID, err)
		RespondError(w, http.StatusNotFound, "Connection not found")
		return
	}

	// Build connection string, using optional database override
	databaseName := req.DatabaseName
	connStr := h.datastore.BuildConnectionString(conn, password, databaseName)

	// Create a temporary pool for this query
	pool, err := openQueryPool(ctx, connStr, "pgEdge AI DBA Workbench - Query")
	if err != nil {
		log.Printf("[ERROR] Failed to connect for query (connection=%d): %v", connectionID, err)
		RespondError(w, http.StatusInternalServerError,
			"Failed to connect to database")
		return
	}
	defer pool.Close()

	limit := defaultRowLimit

	// For EXPLAIN queries with $N parameter placeholders, bypass the
	// standard query path and use pgconn's simple protocol directly.
	// pgx.Conn.Query always parses $N as bind parameters even in
	// simple protocol mode, but pgconn.Exec sends SQL text as-is.
	//
	// The confirmation prompt and the write-access gate above have
	// already run for these statements, and runSimpleStatements applies
	// the same read-only transaction the pgx path uses, so this branch
	// is no shortcut past either check (issue #530). The classification
	// above is what decides whether a statement is treated as a write;
	// the read-only transaction narrows what a misclassified statement
	// can do, but it does not catch every case (see
	// runSimpleStatements).
	if needsSimpleProtocol(statements) {
		poolConn, err := pool.Acquire(ctx)
		if err != nil {
			log.Printf("[ERROR] Failed to acquire connection for EXPLAIN: %v", err)
			RespondError(w, http.StatusInternalServerError,
				"Failed to connect to database")
			return
		}
		defer poolConn.Release()

		pgConn := poolConn.Conn().PgConn()
		results := runSimpleStatements(ctx, pgConn, statements, limit,
			connectionID, allReadOnly)
		if !allReadOnly {
			results = endOpenTransaction(ctx, pgConn, results, connectionID)
		}

		RespondJSON(w, http.StatusOK, multiQueryResponse{
			Results:         results,
			TotalStatements: len(statements),
		})
		return
	}

	results := make([]statementResult, 0, len(statements))

	if allReadOnly {
		// Read-only path: execute inside a read-only transaction
		tx, err := pool.Begin(ctx)
		if err != nil {
			log.Printf("[ERROR] Failed to begin transaction for query: %v", err)
			RespondError(w, http.StatusInternalServerError,
				"Failed to execute query")
			return
		}

		committed := false
		defer func() {
			if !committed {
				_ = rollback.Tx(ctx, tx) //nolint:errcheck // see pkg/rollback
			}
		}()

		// Enforce read-only transaction
		_, err = tx.Exec(ctx, "SET TRANSACTION READ ONLY")
		if err != nil {
			log.Printf("[ERROR] Failed to set transaction read-only: %v", err)
			RespondError(w, http.StatusInternalServerError,
				"Failed to execute query")
			return
		}

		for _, stmt := range statements {
			result := runStatement(ctx, tx, stmt, limit, connectionID)
			result = requireUTF8(tx.Conn().PgConn(), result, connectionID)
			results = append(results, result)

			// Stop on first error
			if result.Error != "" {
				break
			}
		}

		// If any statement errored, the transaction is aborted in
		// PostgreSQL so we must rollback.  Otherwise commit cleanly.
		hasError := false
		for _, r := range results {
			if r.Error != "" {
				hasError = true
				break
			}
		}

		if !hasError {
			if err := tx.Commit(ctx); err != nil {
				log.Printf("[ERROR] Failed to commit read-only transaction: %v", err)
				RespondError(w, http.StatusInternalServerError,
					"Failed to execute query")
				return
			}
			committed = true
		}
	} else {
		// Write path: execute each statement individually outside a
		// transaction so that statements like ALTER SYSTEM work. Every
		// statement runs on the one acquired connection so that
		// requireUTF8 can read the client_encoding it left behind.
		poolConn, err := pool.Acquire(ctx)
		if err != nil {
			log.Printf("[ERROR] Failed to acquire connection for query: %v", err)
			RespondError(w, http.StatusInternalServerError,
				"Failed to connect to database")
			return
		}
		defer poolConn.Release()
		pgConn := poolConn.Conn().PgConn()

		for _, stmt := range statements {
			var result statementResult
			if isReadOnlyStatement(stmt) {
				result = runStatement(ctx, poolConn, stmt, limit, connectionID)
			} else if _, err := poolConn.Exec(ctx, stmt); err != nil {
				log.Printf("[ERROR] Write statement failed (connection=%d): %v",
					connectionID, err)
				result = statementResult{
					Query: stmt,
					Error: safeQueryError("Execution error", err),
				}
			} else {
				result = statementResult{
					Query:    stmt,
					Columns:  []string{"result"},
					Rows:     [][]string{{"Statement executed successfully"}},
					RowCount: 1,
				}
			}
			result = requireUTF8(pgConn, result, connectionID)
			results = append(results, result)
			if result.Error != "" {
				break
			}
		}
		results = endOpenTransaction(ctx, pgConn, results, connectionID)
	}

	resp := multiQueryResponse{
		Results:         results,
		TotalStatements: len(statements),
	}

	RespondJSON(w, http.StatusOK, resp)
}

// stripLeadingComments removes leading whitespace and SQL comments from
// sql, returning the remaining statement body (see
// sqllex.StripLeadingComments).
func stripLeadingComments(sql string) string {
	return sqllex.StripLeadingComments(sql)
}

// maxExplainDepth bounds the recursion isReadOnlyStatement performs
// through nested EXPLAIN statements. PostgreSQL rejects EXPLAIN EXPLAIN
// ..., so anything approaching this depth is malformed or hostile and
// the classifier fails closed by calling it a write.
const maxExplainDepth = 8

// isReadOnlyStatement returns true if the SQL statement (after stripping
// leading comments) begins with a read-only keyword: SELECT, WITH, SHOW,
// EXPLAIN, or TABLE. Writable CTEs (WITH ... INSERT/UPDATE/DELETE/MERGE)
// are classified as non-read-only, as are a query with a writing clause
// (see hasWritingSelectClause), a query that calls a side-effecting
// function such as pg_terminate_backend or set_config (see
// sqllex.CallsSideEffectingFunction, issue #646) and an EXPLAIN that may
// execute a writing inner statement (issue #530).
//
// PostgreSQL lexes a plain '...' literal as a standard string when
// standard_conforming_strings is on and as an escape string, in which a
// backslash escapes the next character, when it is off. The setting can
// be off for the database or the role, and an earlier statement in the
// same request can turn it off, as set_config does from inside a
// SELECT, so the reading in force when sql runs cannot be known here.
// A literal can end in a different place under each reading, and the
// one the classifier does not use can then hide the code that follows:
//
//	WITH x AS (SELECT '\'' AS c), d AS (DELETE FROM t RETURNING *)
//	SELECT * FROM d --'
//
// is one literal running to the final quote under a standard reading,
// which hides the DELETE, whereas under an escape reading the literal
// ends after the escaped quote and the DELETE is code. sql is therefore
// classified under both readings, and split into statements under each
// so that a semicolon only one reading treats as code is respected too;
// it is read-only only when every statement is read-only under both.
func isReadOnlyStatement(sql string) bool {
	for _, reading := range sqllex.StringReadings {
		pieces := splitStatementsAs(sql, reading)
		if len(pieces) == 0 {
			pieces = []string{sql}
		}
		for _, piece := range pieces {
			if !isReadOnlyStatementAtDepth(piece, 0, reading) {
				return false
			}
		}
	}
	return true
}

// isReadOnlyStatementAtDepth is isReadOnlyStatement for a single
// statement, under one reading of a plain '...' literal, with the
// EXPLAIN nesting depth reached so far.
func isReadOnlyStatementAtDepth(sql string, depth int, reading sqllex.StringReading) bool {
	body := strings.TrimSpace(stripLeadingComments(sql))

	// The EXPLAIN test runs against the original text rather than an
	// uppercased copy, because strings.ToUpper is not length-preserving
	// for every input (U+0131 gets shorter), so an offset measured on
	// the copy cannot safely slice the original.
	if hasKeywordPrefix(body, "EXPLAIN") {
		return isReadOnlyExplain(body, depth, reading)
	}

	// Mask the quoted literals, quoted identifiers and comments before
	// uppercasing, so the keyword scans below read code only and a
	// statement such as SELECT * FROM audit WHERE msg LIKE '%signed
	// into%' is not classified as a write. The masking is for
	// classification alone; the text that is executed is untouched.
	upper := strings.ToUpper(sqllex.MaskNonCode(body, reading))

	if strings.HasPrefix(upper, "WITH") {
		// Writable CTEs can perform data modification, e.g.
		// WITH deleted AS (DELETE FROM t RETURNING *) SELECT * FROM deleted.
		// Check for DML keywords as standalone words in the body.
		dmlKeywords := []string{"INSERT", "UPDATE", "DELETE", "MERGE"}
		for _, kw := range dmlKeywords {
			if containsSQLKeyword(upper, kw) {
				return false
			}
		}
		return !hasWritingSelectClause(upper) &&
			!sqllex.CallsSideEffectingFunction(body, reading)
	}

	if strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "TABLE ") {
		return !hasWritingSelectClause(upper) &&
			!sqllex.CallsSideEffectingFunction(body, reading)
	}

	return strings.HasPrefix(upper, "SHOW")
}

// hasWritingSelectClause reports whether an otherwise reading query
// carries a clause that writes: SELECT ... INTO, which creates a table,
// or a row-locking clause (FOR UPDATE, FOR NO KEY UPDATE, FOR SHARE,
// FOR KEY SHARE), which stamps the lock onto every row it returns.
// PostgreSQL refuses both inside a read-only transaction, so classifying
// them as writes makes this gate agree with the database rather than
// leaving the transaction to reject them later. upperSQL must already
// have had its non-code regions masked (see sqllex.MaskNonCode) and been
// uppercased. Like the writable-CTE scan, this reads a keyword wherever
// it appears in the remaining code, so it can only over-report, which is
// the safe direction.
func hasWritingSelectClause(upperSQL string) bool {
	return containsSQLKeyword(upperSQL, "INTO") ||
		containsLockingClause(upperSQL)
}

// containsLockingClause reports whether upperSQL contains a row-locking
// clause: the word FOR followed by UPDATE or SHARE, with the optional NO
// and KEY qualifiers in between.
func containsLockingClause(upperSQL string) bool {
	words := sqlWords(upperSQL)
	for i, word := range words {
		if word != "FOR" {
			continue
		}
		for j := i + 1; j < len(words); j++ {
			switch words[j] {
			case "NO", "KEY":
				continue
			case "UPDATE", "SHARE":
				return true
			}
			break
		}
	}
	return false
}

// sqlWords splits s into its runs of identifier characters, discarding
// everything else.
func sqlWords(s string) []string {
	var words []string
	i := 0
	for i < len(s) {
		if !sqllex.IsIdentChar(s[i]) {
			i++
			continue
		}
		start := i
		for i < len(s) && sqllex.IsIdentChar(s[i]) {
			i++
		}
		words = append(words, s[start:i])
	}
	return words
}

// explainNonExecutingOptions are the EXPLAIN option names that only
// change how the plan is reported. Every other name, ANALYZE included,
// means the inner statement may really run.
var explainNonExecutingOptions = map[string]bool{
	"VERBOSE":      true,
	"COSTS":        true,
	"SETTINGS":     true,
	"GENERIC_PLAN": true,
	"BUFFERS":      true,
	"WAL":          true,
	"TIMING":       true,
	"SUMMARY":      true,
	"MEMORY":       true,
	"SERIALIZE":    true,
	"FORMAT":       true,
}

// isReadOnlyExplain classifies an EXPLAIN statement by what it explains.
// body must already start with the EXPLAIN keyword and have had its
// leading comments stripped. Without ANALYZE, EXPLAIN only plans the
// inner statement and executes nothing, so it is read-only; with ANALYZE
// the inner statement really runs and decides the answer.
//
// In the parenthesised option list, ANALYZE is detected by an
// allow-list that fails closed rather than by searching for the word,
// because an option name there is a ColId: PostgreSQL accepts it as a
// quoted identifier and decodes its Unicode escapes before comparing,
// so EXPLAIN (U&"\0061nalyze") DELETE ... turns ANALYZE on without the
// text ever appearing. Only a bare, unquoted, recognized option name
// counts as non-executing; anything quoted, escaped or unrecognized is
// taken as an execution, and the inner statement then decides the
// classification. The legacy form needs no such care, because its
// option words are keywords that cannot be quoted or escaped.
func isReadOnlyExplain(body string, depth int, reading sqllex.StringReading) bool {
	executes, rest, ok := parseExplainHead(body, reading)
	if !ok {
		// Malformed option list: fail closed rather than guess at it.
		return false
	}
	if !executes {
		return true
	}
	if rest == "" || depth >= maxExplainDepth {
		return false
	}
	return isReadOnlyStatementAtDepth(rest, depth+1, reading)
}

// parseExplainHead reads the options of an EXPLAIN statement, under one
// reading of a plain '...' literal, and reports whether they make the
// inner statement run, along with the inner statement's text. body must
// already start with the EXPLAIN keyword. ok is false when the option
// list's parentheses do not balance. See isReadOnlyExplain for why the
// two option forms are read as they are.
func parseExplainHead(body string, reading sqllex.StringReading) (executes bool, rest string, ok bool) {
	rest = strings.TrimSpace(stripLeadingComments(body[len("EXPLAIN"):]))

	if strings.HasPrefix(rest, "(") {
		// Parenthesised form: EXPLAIN ( option [, ...] ) statement.
		options, remainder, balanced := splitExplainOptions(rest, reading)
		if !balanced {
			// Unbalanced parentheses: the statement is malformed.
			return false, "", false
		}
		executes = explainOptionsExecute(options, reading)
		rest = strings.TrimSpace(stripLeadingComments(remainder))
	} else {
		// Legacy form: EXPLAIN [ANALYZE] [VERBOSE] statement, in either
		// order. This grammar is closed: ANALYZE, its British
		// spelling and VERBOSE are keywords rather than a ColId, so a
		// quoted or escaped spelling of one is a syntax error rather
		// than an option, and the first word that is none of them
		// really is the start of the inner statement. A plain EXPLAIN
		// only plans it.
	keywords:
		for {
			word, remainder := nextSQLWord(rest)
			switch strings.ToUpper(word) {
			case "ANALYZE", "ANALYSE": //nolint:misspell // British spelling accepted by PostgreSQL
				executes = true
			case "VERBOSE":
			default:
				break keywords
			}
			rest = strings.TrimSpace(stripLeadingComments(remainder))
		}
	}

	return executes, rest, true
}

// explainExecutes reports whether an EXPLAIN statement would run the
// statement it explains, under either reading of a plain '...' literal.
// body must already start with the EXPLAIN keyword and have had its
// leading comments stripped. A malformed option list counts as an
// execution, so a caller that refuses executing EXPLAINs fails closed.
func explainExecutes(body string) bool {
	for _, reading := range sqllex.StringReadings {
		executes, _, ok := parseExplainHead(body, reading)
		if executes || !ok {
			return true
		}
	}
	return false
}

// explainOptionsExecute reports whether a parenthesised EXPLAIN option
// list may cause the inner statement to run. It inspects option names
// only, never their values, and treats a name it cannot read as a bare
// recognized identifier as an execution.
func explainOptionsExecute(options string, reading sqllex.StringReading) bool {
	entries := splitExplainOptionEntries(options, reading)
	if len(entries) == 0 {
		// EXPLAIN () is not valid SQL, so fail closed.
		return true
	}
	for _, entry := range entries {
		name, _ := nextSQLWord(strings.TrimSpace(stripLeadingComments(entry)))
		if !explainNonExecutingOptions[strings.ToUpper(name)] {
			return true
		}
	}
	return false
}

// splitExplainOptionEntries splits an EXPLAIN option list on the commas
// that separate its entries. Quoted strings, quoted identifiers,
// comments and nested parentheses are skipped so that a comma inside one
// does not start an entry. The result is nil for an empty list.
func splitExplainOptionEntries(options string, reading sqllex.StringReading) []string {
	var entries []string
	depth := 0
	start := 0
	i := 0

	for i < len(options) {
		if j := sqllex.SkipNonCode(options, i, reading); j != i {
			i = j
			continue
		}

		ch := options[i]
		switch {
		case ch == '(':
			depth++
		case ch == ')':
			depth--
		case ch == ',' && depth == 0:
			entries = append(entries, options[start:i])
			start = i + 1
		}
		i++
	}
	entries = append(entries, options[start:])

	if len(entries) == 1 &&
		strings.TrimSpace(stripLeadingComments(entries[0])) == "" {
		return nil
	}
	return entries
}

// splitExplainOptions splits an EXPLAIN option list from the statement
// that follows it. s must start with the opening parenthesis. It
// returns the option text, the remainder after the matching closing
// parenthesis, and whether a matching parenthesis was found. Quoted
// strings, quoted identifiers and comments are skipped so that a
// parenthesis inside one does not end the list early.
func splitExplainOptions(s string, reading sqllex.StringReading) (options, remainder string, ok bool) {
	depth := 0
	i := 0
	for i < len(s) {
		if j := sqllex.SkipNonCode(s, i, reading); j != i {
			i = j
			continue
		}

		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[1:i], s[i+1:], true
			}
		}
		i++
	}
	return "", "", false
}

// nextSQLWord splits the leading identifier-character run off s,
// returning that word and the rest of the string. The word is empty
// when s does not start with an identifier character.
func nextSQLWord(s string) (word, rest string) {
	i := 0
	for i < len(s) && sqllex.IsIdentChar(s[i]) {
		i++
	}
	return s[:i], s[i:]
}

// hasKeywordPrefix reports whether sql starts with keyword, ignoring
// case, followed by a non-identifier character, so that EXPLAINABLE does
// not match EXPLAIN. The comparison is made on the caller's own string
// rather than an uppercased copy, so that the keyword's length is a
// valid offset into it.
func hasKeywordPrefix(sql, keyword string) bool {
	if len(sql) < len(keyword) ||
		!strings.EqualFold(sql[:len(keyword)], keyword) {
		return false
	}
	return len(sql) == len(keyword) || !sqllex.IsIdentChar(sql[len(keyword)])
}

// containsSQLKeyword checks whether a SQL keyword appears as a standalone
// word in the given uppercase SQL string. This prevents false positives
// from identifiers that contain a keyword as a substring (e.g.,
// "updated_at" should not match "UPDATE").
func containsSQLKeyword(upperSQL, keyword string) bool {
	idx := 0
	for {
		pos := strings.Index(upperSQL[idx:], keyword)
		if pos < 0 {
			return false
		}
		pos += idx
		end := pos + len(keyword)

		startOK := pos == 0 || !sqllex.IsIdentChar(upperSQL[pos-1])
		endOK := end >= len(upperSQL) || !sqllex.IsIdentChar(upperSQL[end])

		if startOK && endOK {
			return true
		}
		idx = end
	}
}

// needsSimpleProtocol returns true when any statement is an EXPLAIN
// that contains $N parameter placeholders.  These queries require the
// simple query protocol so that pgx does not interpret the
// placeholders as bind parameters.
func needsSimpleProtocol(statements []string) bool {
	for _, stmt := range statements {
		body := strings.TrimSpace(stripLeadingComments(stmt))
		if hasKeywordPrefix(body, "EXPLAIN") && containsDollarParam(stmt) {
			return true
		}
	}
	return false
}

// containsDollarParam checks whether the string contains a $N parameter
// placeholder (e.g. $1, $2) as real SQL. A $N inside a single-quoted
// literal (including the prefixed E'...' and U&'...' forms), a
// dollar-quoted literal, a double-quoted identifier, a line comment or a
// block comment is text rather than a placeholder and does not count
// (issue #530). An unterminated literal or comment swallows
// the rest of the string, so the scan ends and reports no placeholder
// rather than reading the quoted text as SQL. A placeholder found under
// either reading of a plain '...' literal counts, since this only routes
// the statement onto the simple-protocol path, which applies the same
// read-only transaction and row limit as the pgx path.
func containsDollarParam(s string) bool {
	for _, reading := range sqllex.StringReadings {
		if containsDollarParamAs(s, reading) {
			return true
		}
	}
	return false
}

// containsDollarParamAs is containsDollarParam under one reading of a
// plain '...' literal.
func containsDollarParamAs(s string, reading sqllex.StringReading) bool {
	i := 0
	for i < len(s) {
		if j := sqllex.SkipNonCode(s, i, reading); j != i {
			i = j
			continue
		}

		// sqllex.SkipNonCode has already stepped over any dollar-quoted
		// string, so a dollar sign here is a placeholder or a bare one.
		if s[i] != '$' {
			i++
			continue
		}
		if i+1 < len(s) && s[i+1] >= '1' && s[i+1] <= '9' {
			return true
		}
		i++
	}
	return false
}

// containsBindPlaceholder reports whether s contains a $N bind
// parameter placeholder in its code, for the validate endpoint. It reads
// s as containsDollarParam does, under both readings of a plain '...'
// literal, except that a dollar-digit sequence continuing an identifier,
// such as col$1, is not a placeholder: PostgreSQL allows $ in an
// identifier after its first character, so it is part of the name.
func containsBindPlaceholder(s string) bool {
	for _, reading := range sqllex.StringReadings {
		i := 0
		for i < len(s) {
			if j := sqllex.SkipNonCode(s, i, reading); j != i {
				i = j
				continue
			}
			if s[i] == '$' && i+1 < len(s) && s[i+1] >= '1' && s[i+1] <= '9' &&
				!sqllex.ContinuesIdentifier(s, i) {
				return true
			}
			i++
		}
	}
	return false
}

// safeQueryError extracts a user-facing error message from a query error.
// For PostgreSQL errors it returns only the database message; for known
// safe error types it returns a descriptive message; for other errors it
// returns a generic message to avoid leaking Go internals.
func safeQueryError(prefix string, err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Detail != "" {
			return fmt.Sprintf("%s: %s (%s)", prefix, pgErr.Message, pgErr.Detail)
		}
		return fmt.Sprintf("%s: %s", prefix, pgErr.Message)
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Sprintf("%s: query timed out", prefix)
	}

	if errors.Is(err, context.Canceled) {
		return fmt.Sprintf("%s: query was canceled", prefix)
	}

	msg := err.Error()
	if isParameterPlaceholderError(msg) {
		return fmt.Sprintf(
			"%s: query contains parameter placeholders ($1, $2, ...) "+
				"that require values; these cannot be executed directly",
			prefix)
	}

	if isConnectionError(msg) {
		return fmt.Sprintf("%s: connection error; the database may be unreachable", prefix)
	}

	log.Printf("Query error (non-PgError): %v", err)
	return fmt.Sprintf("%s: an internal error occurred", prefix)
}

// isConnectionError checks whether an error message indicates a
// network-level or connection-level failure that is safe to surface.
func isConnectionError(msg string) bool {
	lower := strings.ToLower(msg)
	patterns := []string{
		"connection refused",
		"connection reset",
		"connection timed out",
		"no such host",
		"i/o timeout",
		"broken pipe",
		"closed network connection",
		"failed to connect",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// isParameterPlaceholderError checks whether an error message indicates
// a parameter count mismatch, which occurs when a query containing
// placeholders ($1, $2, ...) is executed without supplying values.
func isParameterPlaceholderError(msg string) bool {
	lower := strings.ToLower(msg)
	return (strings.Contains(lower, "expected") &&
		strings.Contains(lower, "arguments")) ||
		strings.Contains(lower, "arguments, got")
}

// hasLimitClause reports whether sql already contains a LIMIT clause as a
// standalone SQL keyword, avoiding false positives from column names such
// as "credit_limit".
func hasLimitClause(sql string) bool {
	return containsSQLKeyword(strings.ToUpper(sql), "LIMIT")
}

// queryable is an interface satisfied by both pgx.Tx and *pgxpool.Pool,
// allowing runStatement to execute queries against either.
type queryable interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// runStatement executes a single SQL statement against a queryable target
// and returns the result. It injects a LIMIT clause for SELECT and
// WITH queries that do not already contain one.
func runStatement(ctx context.Context, q queryable, stmt string, limit int, connectionID int) statementResult {
	// Only inject LIMIT on SELECT/WITH queries
	sqlQuery := stmt
	stmtBody := stripLeadingComments(sqlQuery)
	upperBody := strings.ToUpper(stmtBody)
	isSelect := strings.HasPrefix(upperBody, "SELECT") ||
		strings.HasPrefix(upperBody, "WITH")
	hasExistingLimit := hasLimitClause(sqlQuery)
	if isSelect && !hasExistingLimit {
		sqlQuery = fmt.Sprintf("%s LIMIT %d", sqlQuery, limit+1)
	}

	// Execute the statement
	rows, err := q.Query(ctx, sqlQuery)
	if err != nil {
		log.Printf("[ERROR] Query execution failed (connection=%d): %v", connectionID, err)
		return statementResult{
			Query: stmt,
			Error: safeQueryError("Query error", err),
		}
	}
	defer rows.Close()

	// Extract column names
	fieldDescriptions := rows.FieldDescriptions()
	columns := make([]string, len(fieldDescriptions))
	for i, fd := range fieldDescriptions {
		columns[i] = string(fd.Name)
	}

	// Collect rows
	var resultRows [][]string
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			log.Printf("[ERROR] Failed to read row: %v", err)
			return statementResult{
				Query: stmt,
				Error: safeQueryError("Failed to read row", err),
			}
		}

		row := make([]string, len(values))
		for i, v := range values {
			row[i] = formatValueForJSON(v)
		}
		resultRows = append(resultRows, row)
	}

	if err := rows.Err(); err != nil {
		log.Printf("[ERROR] Error iterating query rows: %v", err)
		return statementResult{
			Query: stmt,
			Error: safeQueryError("Failed to read query results", err),
		}
	}

	// Detect truncation (only when LIMIT was injected)
	truncated := false
	if isSelect && !hasExistingLimit && len(resultRows) > limit {
		truncated = true
		resultRows = resultRows[:limit]
	}

	// Ensure rows is not nil in JSON output
	if resultRows == nil {
		resultRows = [][]string{}
	}

	return statementResult{
		Columns:   columns,
		Rows:      resultRows,
		RowCount:  len(resultRows),
		Truncated: truncated,
		Query:     stmt,
	}
}

// clientEncodingChangedError is the error reported for a statement that
// left the connection on a client_encoding other than UTF8.
const clientEncodingChangedError = "Query error: client_encoding must " +
	"remain UTF8; the statements after this one were not run"

// requireUTF8 turns result into a failure when the statement that
// produced it left pgConn on a client_encoding other than UTF8, so the
// caller stops before the next statement runs. PostgreSQL converts each
// statement from the client encoding before lexing it, and in SJIS,
// BIG5, GBK, GB18030 and JOHAB the second byte of a multibyte character
// can be one that is a backslash, @, [ or a letter on its own, so the
// server's reading of the text no longer matches the UTF-8 reading the
// classifier gave it. After set_config('client_encoding', 'SJIS',
// false), an E' followed by the bytes 0x95 0x5C and a quote is a
// complete literal to the server, which reads 0x95 0x5C as one
// character, but to the classifier 0x5C is a backslash escaping the
// quote, which hides the code after it. No second reading can cover
// every such encoding, so the setting is held at UTF8 instead. The write
// path applies the same check: its confirmation prompt lists only the
// statements classified as writes, so a write hidden in a statement
// lexed under the new encoding would otherwise run without being listed.
func requireUTF8(pgConn *pgconn.PgConn, result statementResult, connectionID int) statementResult {
	if result.Error != "" || pgConn.ParameterStatus("client_encoding") == "UTF8" {
		return result
	}
	log.Printf("[WARN] Query stopped: client_encoding changed to %q (connection=%d)",
		pgConn.ParameterStatus("client_encoding"), connectionID)
	return statementResult{
		Query: result.Query,
		Error: clientEncodingChangedError,
	}
}

// openTransactionError is the error reported for a confirmed batch that
// left a transaction open.
const openTransactionError = "Transaction error: the statements left " +
	"a transaction open, so it was rolled back and nothing done inside it " +
	"was kept; end the transaction with COMMIT or ROLLBACK in the same request"

// endOpenTransaction rolls back a transaction that a confirmed batch
// opened and did not close, and adds a result saying so. The write path
// runs every statement on one connection, so a BEGIN in the batch
// carries over to the statements after it, and the connection goes back
// to a pool that destroys a connection released mid-transaction, which
// discards that work. Without this result the batch would report each
// statement as having succeeded although none of its work in the
// transaction was kept.
func endOpenTransaction(
	ctx context.Context,
	pgConn *pgconn.PgConn,
	results []statementResult,
	connectionID int,
) []statementResult {
	if pgConn.TxStatus() == 'I' {
		return results
	}
	log.Printf("[WARN] Query left a transaction open; rolling it back (connection=%d)",
		connectionID)
	rollbackSimple(ctx, func(execCtx context.Context, sql string) error {
		return pgConn.Exec(execCtx, sql).Close()
	}, connectionID)
	return append(results, statementResult{
		Query: "ROLLBACK",
		Error: openTransactionError,
	})
}

// runSimpleStatements executes statements over the simple query
// protocol. When readOnly is true they run inside a read-only
// transaction, matching the discipline the pgx path applies (issue
// #530). A statement error rolls the transaction back and a clean run
// commits. A failure of the transaction control itself is reported as a
// result of its own and no statement runs, so a batch classified as
// read-only never runs outside the transaction it asked for.
//
// SET TRANSACTION READ ONLY prevents writes to database objects within
// the transaction, so a misclassified INSERT, UPDATE, DELETE, TRUNCATE,
// CREATE TABLE AS, SELECT ... INTO, SELECT ... FOR UPDATE, sequence
// advance or EXPLAIN ANALYZE of any of those is refused. It is not an
// authorisation boundary and it does not reach side effects a function
// performs outside the transaction's own writes: pg_terminate_backend,
// pg_reload_conf, pg_switch_wal, pg_create_restore_point, pg_read_file,
// the advisory-lock functions and dblink_exec all remain permitted
// inside it. Classification, not this transaction, is what keeps such
// statements behind the write gate.
func runSimpleStatements(
	ctx context.Context,
	pgConn *pgconn.PgConn,
	statements []string,
	limit int,
	connectionID int,
	readOnly bool,
) []statementResult {
	exec := func(execCtx context.Context, sql string) error {
		return pgConn.Exec(execCtx, sql).Close()
	}
	run := func(runCtx context.Context, stmt string) statementResult {
		result := runSimpleStatement(runCtx, pgConn, stmt, limit, connectionID)
		return requireUTF8(pgConn, result, connectionID)
	}
	return runSimpleStatementsWith(ctx, exec, run, statements, connectionID,
		readOnly)
}

// runSimpleStatementsWith is runSimpleStatements over an injected
// statement executor and runner, so that the transaction discipline can
// be tested without a live connection. exec issues the transaction
// control statements and run executes one of the caller's statements.
func runSimpleStatementsWith(
	ctx context.Context,
	exec func(ctx context.Context, sql string) error,
	run func(ctx context.Context, stmt string) statementResult,
	statements []string,
	connectionID int,
	readOnly bool,
) []statementResult {
	if readOnly {
		if err := exec(ctx, "BEGIN"); err != nil {
			// No rollback here: a BEGIN that failed part-way leaves the
			// connection in a transaction that nothing else will use,
			// because the pool is built per request and pgxpool destroys
			// a connection released with a non-idle transaction status.
			return []statementResult{controlFailure("BEGIN", err, connectionID)}
		}
		if err := exec(ctx, "SET TRANSACTION READ ONLY"); err != nil {
			rollbackSimple(ctx, exec, connectionID)
			return []statementResult{
				controlFailure("SET TRANSACTION READ ONLY", err, connectionID),
			}
		}
	}

	results := make([]statementResult, 0, len(statements))
	failed := false
	for _, stmt := range statements {
		result := run(ctx, stmt)
		results = append(results, result)
		if result.Error != "" {
			failed = true
			break
		}
	}

	if readOnly {
		if failed {
			rollbackSimple(ctx, exec, connectionID)
		} else if err := exec(ctx, "COMMIT"); err != nil {
			rollbackSimple(ctx, exec, connectionID)
			results = append(results,
				controlFailure("COMMIT", err, connectionID))
		}
	}

	return results
}

// controlFailure logs a failed transaction-control statement and turns
// it into a result the caller can report alongside the statements'
// own results.
func controlFailure(sql string, err error, connectionID int) statementResult {
	log.Printf("[ERROR] Simple protocol %s failed (connection=%d): %v",
		sql, connectionID, err)
	return statementResult{
		Query: sql,
		Error: safeQueryError("Transaction error", err),
	}
}

// rollbackSimple unwinds a simple-protocol transaction through
// pkg/rollback, so the unwind runs on the bounded, non-cancelable
// context the project requires. A failed rollback is logged: the
// connection is released back to a pool that is closed at the end of
// the request, so there is nothing further to do about it.
func rollbackSimple(
	ctx context.Context,
	exec func(ctx context.Context, sql string) error,
	connectionID int,
) {
	if err := rollback.Simple(ctx, exec); err != nil {
		log.Printf("[ERROR] Simple protocol rollback failed (connection=%d): %v",
			connectionID, err)
	}
}

// runSimpleStatement executes a statement using the pgconn simple
// protocol which sends SQL text directly to PostgreSQL without
// interpreting $N as bind parameters.  This is used for EXPLAIN
// queries that contain parameter placeholders from pg_stat_statements.
//
// No LIMIT can be injected here, because the statement text is sent
// unaltered, so the result set is bounded on this side instead: rows
// past limit are counted but not retained, and Truncated reports that
// the caller is seeing a shortened result, as it does on the pgx path.
// The reader is drained rather than abandoned, so the result reader
// closes cleanly and the connection stays usable for the rest of the
// batch and for the transaction control around it.
func runSimpleStatement(ctx context.Context, pgConn *pgconn.PgConn, stmt string, limit int, connectionID int) statementResult {
	set := &simpleResultSet{limit: limit}
	if err := set.read(pgConn.Exec(ctx, stmt), connectionID); err != nil {
		return statementResult{
			Query: stmt,
			Error: safeQueryError("Query error", err),
		}
	}

	if set.rows == nil {
		set.rows = [][]string{}
	}

	return statementResult{
		Columns:   set.columns,
		Rows:      set.rows,
		RowCount:  len(set.rows),
		Truncated: set.truncated,
		Query:     stmt,
	}
}

// simpleResultSet accumulates the columns and rows of a simple-protocol
// result, bounded by limit.
type simpleResultSet struct {
	limit     int
	columns   []string
	rows      [][]string
	truncated bool
}

// read drains every result of mrr into the set, closing both the
// individual result readers and mrr itself, and returns the first error
// either close reported. Reading stops at the first failing result,
// whose error is the one the caller reports.
func (rs *simpleResultSet) read(
	mrr *pgconn.MultiResultReader,
	connectionID int,
) error {
	var failure error

	for mrr.NextResult() {
		rr := mrr.ResultReader()
		rs.readResult(rr)

		if _, err := rr.Close(); err != nil {
			log.Printf("[ERROR] Simple query close failed (connection=%d): %v",
				connectionID, err)
			failure = err
			break
		}
	}

	// The multi-result reader is always closed, including after a
	// failing result: leaving it open keeps the connection busy, and the
	// caller's ROLLBACK would then fail to reach the server.
	if err := mrr.Close(); err != nil && failure == nil {
		log.Printf("[ERROR] Simple query multi-result close failed (connection=%d): %v",
			connectionID, err)
		failure = err
	}

	return failure
}

// readResult appends one result's rows to the set, taking the column
// names from the first result that carries any. Every row is read so
// that the reader closes cleanly, but rows past the limit are counted
// rather than retained.
func (rs *simpleResultSet) readResult(rr *pgconn.ResultReader) {
	// Extract column names from field descriptions
	if rs.columns == nil {
		fds := rr.FieldDescriptions()
		rs.columns = make([]string, len(fds))
		for i, fd := range fds {
			rs.columns[i] = string(fd.Name)
		}
	}

	for rr.NextRow() {
		if len(rs.rows) >= rs.limit {
			// Keep reading so the reader closes cleanly, but stop
			// buffering: an unbounded EXPLAIN or SELECT would
			// otherwise hold the whole result set in memory.
			rs.truncated = true
			continue
		}
		rs.rows = append(rs.rows, simpleRowValues(rr.Values()))
	}
}

// simpleRowValues renders one simple-protocol row as strings, with a
// NULL column reported as the text NULL, matching the pgx path.
func simpleRowValues(row [][]byte) []string {
	values := make([]string, len(row))
	for i, col := range row {
		if col == nil {
			values[i] = "NULL"
		} else {
			values[i] = string(col)
		}
	}
	return values
}

// formatValueForJSON converts a database value to a string for JSON
// serialization. This reuses the tsv.FormatValue logic but without TSV
// escaping since JSON handles special characters natively.
func formatValueForJSON(v any) (result string) {
	if v == nil {
		return "NULL"
	}
	// Use the TSV formatter which handles pgtype.Numeric, UUID,
	// Timestamp, etc. The TSV escaping of \t and \n is harmless
	// since we are placing the result inside a JSON string.
	// Use a named return with deferred recover so that if
	// FormatValue panics on an unexpected type, we fall back
	// to Go's default formatting.
	defer func() {
		if r := recover(); r != nil {
			result = fmt.Sprintf("%v", v)
		}
	}()
	return tsv.FormatValue(v)
}

// Statement validation statuses reported by validateQuery.
const (
	// validationValid means EXPLAIN planned the statement.
	validationValid = "valid"
	// validationInvalid means EXPLAIN rejected the statement.
	validationInvalid = "invalid"
	// validationUnsupported means the statement could not be planned at
	// all, so nothing can be said about whether it would run.
	validationUnsupported = "unsupported"
)

// validateTimeout bounds the whole validation request.
const validateTimeout = 15 * time.Second

// validateStatementTimeout bounds each EXPLAIN inside the validation
// transaction, so a statement that plans slowly cannot hold the
// connection for the whole request timeout.
const validateStatementTimeout = "5s"

// genericPlanMinVersionNum is the server_version_num of the first
// release with EXPLAIN (GENERIC_PLAN), which is PostgreSQL 16.
const genericPlanMinVersionNum = 160000

// validateSavepoint is the savepoint each statement is planned under,
// so that a statement PostgreSQL rejects does not abort the
// transaction for the statements that follow it.
const validateSavepoint = "workbench_validate"

// queryValidateRequest is the JSON request body for validating a query
// without executing it.
type queryValidateRequest struct {
	Query        string `json:"query"`
	DatabaseName string `json:"database_name,omitempty"`
}

// statementValidation reports the outcome of validating a single
// statement. Error carries the sanitized PostgreSQL message when
// Status is invalid, and the reason validation was not possible when
// Status is unsupported.
type statementValidation struct {
	Query  string `json:"query"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

// queryValidateResponse is the JSON response for query validation.
// Valid is true when no statement was rejected; an unsupported
// statement does not make the request invalid, because nothing was
// found wrong with it.
type queryValidateResponse struct {
	Valid           bool                  `json:"valid"`
	TotalStatements int                   `json:"total_statements"`
	Statements      []statementValidation `json:"statements"`
}

// checkDatabaseOverride validates an optional database override with
// database.ValidateDatabaseName. An empty name means no override and
// passes. On failure it writes a 400 response and returns false.
func checkDatabaseOverride(w http.ResponseWriter, name string) bool {
	if name == "" {
		return true
	}
	if err := database.ValidateDatabaseName(name); err != nil {
		RespondError(w, http.StatusBadRequest,
			"Invalid database name: "+err.Error())
		return false
	}
	return true
}

// validateQuery handles POST /api/v1/connections/{id}/query/validate.
// It plans each statement with EXPLAIN inside a read-only transaction
// that is always rolled back, so no statement is ever executed. Only
// read access to the connection is required, because nothing is run.
func (h *ConnectionHandler) validateQuery(w http.ResponseWriter, r *http.Request, connectionID int) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check RBAC access to this connection, before the body is decoded.
	canAccess, _ := h.rbacChecker.CanAccessConnection(r.Context(), connectionID)
	if !canAccess {
		h.denialRecorder.refuseConnection(w, r, deniedActionConnectionQueryValidate,
			connectionID, connectionAccessDenied)
		return
	}

	var req queryValidateRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}

	query := strings.TrimSpace(req.Query)
	if query == "" {
		RespondError(w, http.StatusBadRequest, "Query is required")
		return
	}

	statements := splitStatements(query)
	if len(statements) == 0 {
		RespondError(w, http.StatusBadRequest, "Query is required")
		return
	}

	// Validate the optional database override before it reaches the
	// connection string, and before any datastore work, matching the
	// check on the execute endpoint.
	if !checkDatabaseOverride(w, req.DatabaseName) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), validateTimeout)
	defer cancel()

	conn, password, err := h.datastore.GetConnectionWithPassword(ctx, connectionID)
	if err != nil {
		log.Printf("[ERROR] Connection not found for validation (id=%d): %v",
			connectionID, err)
		RespondError(w, http.StatusNotFound, "Connection not found")
		return
	}

	connStr := h.datastore.BuildConnectionString(conn, password, req.DatabaseName)
	pool, err := openQueryPool(ctx, connStr,
		"pgEdge AI DBA Workbench - Validate")
	if err != nil {
		log.Printf("[ERROR] Failed to connect for validation (connection=%d): %v",
			connectionID, err)
		RespondError(w, http.StatusInternalServerError,
			"Failed to connect to database")
		return
	}
	defer pool.Close()

	results, err := validateStatements(ctx, pool, statements)
	if err != nil {
		log.Printf("[ERROR] Failed to validate query (connection=%d): %v",
			connectionID, err)
		RespondError(w, http.StatusInternalServerError,
			"Failed to validate query")
		return
	}

	valid := true
	for _, result := range results {
		if result.Status == validationInvalid {
			valid = false
			break
		}
	}

	RespondJSON(w, http.StatusOK, queryValidateResponse{
		Valid:           valid,
		TotalStatements: len(results),
		Statements:      results,
	})
}

// openQueryPool creates a single-connection pool for one request
// against a monitored database, tagged with the given application
// name so the monitored server can attribute the session.
func openQueryPool(ctx context.Context, connStr, appName string) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("parse connection string: %w", err)
	}

	poolConfig.MaxConns = 1
	poolConfig.MinConns = 0
	if poolConfig.ConnConfig.RuntimeParams == nil {
		poolConfig.ConnConfig.RuntimeParams = make(map[string]string)
	}
	poolConfig.ConnConfig.RuntimeParams["application_name"] = appName
	// The classifier reads the statement text as UTF-8. A startup
	// parameter overrides any client_encoding default set on the role or
	// the database, and requireUTF8 stops a batch if one of its
	// statements changes the setting (see requireUTF8).
	poolConfig.ConnConfig.RuntimeParams["client_encoding"] = "UTF8"

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return pool, nil
}

// validateStatements plans every statement inside one read-only
// transaction that is always rolled back. An error return means the
// transaction could not be set up at all; a statement PostgreSQL
// rejects is reported in the results rather than as an error.
func validateStatements(
	ctx context.Context,
	pool *pgxpool.Pool,
	statements []string,
) ([]statementValidation, error) {
	poolConn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer poolConn.Release()

	tx, err := poolConn.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = rollback.Tx(ctx, tx) //nolint:errcheck // see pkg/rollback
	}()

	if _, err := tx.Exec(ctx, "SET TRANSACTION READ ONLY"); err != nil {
		return nil, fmt.Errorf("set transaction read-only: %w", err)
	}
	if _, err := tx.Exec(ctx,
		"SET LOCAL statement_timeout = '"+validateStatementTimeout+"'"); err != nil {
		return nil, fmt.Errorf("set statement timeout: %w", err)
	}

	genericPlan, err := supportsGenericPlan(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("read server version: %w", err)
	}

	pgConn := poolConn.Conn().PgConn()
	results := make([]statementValidation, 0, len(statements))
	for _, stmt := range statements {
		results = append(results,
			validateStatement(ctx, tx, pgConn, stmt, genericPlan))

		if err := validationInterrupted(ctx, pgConn.TxStatus()); err != nil {
			return nil, err
		}
	}
	return results, nil
}

// validationInterrupted reports why validation cannot carry on after a
// statement, or nil when it can. A request that has run out of time has
// not checked the statements still to come, so it must not go on to
// report them as valid. And every statement must leave the read-only
// transaction open: if one ended it, whatever follows would run
// outside it, so validation fails closed rather than trusting the
// shape of its input.
func validationInterrupted(ctx context.Context, txStatus byte) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("validation did not finish: %w", err)
	}
	if txStatus == txStatusIdle {
		return errors.New("the validation transaction ended " +
			"before validation finished")
	}
	return nil
}

// txStatusIdle is the ReadyForQuery transaction status PostgreSQL
// reports when the session is not inside a transaction block.
const txStatusIdle = 'I'

// sqlStateQueryCanceled is the SQLSTATE PostgreSQL raises when a
// statement is stopped early, including by statement_timeout.
const sqlStateQueryCanceled = "57014"

// supportsGenericPlan reports whether the server is new enough for
// EXPLAIN (GENERIC_PLAN), which arrived in PostgreSQL 16.
func supportsGenericPlan(ctx context.Context, tx pgx.Tx) (bool, error) {
	var versionNum int
	err := tx.QueryRow(ctx,
		"SELECT current_setting('server_version_num')::int").Scan(&versionNum)
	if err != nil {
		return false, err
	}
	return versionNum >= genericPlanMinVersionNum, nil
}

// validateStatement plans one statement under its own savepoint, so
// that a rejected statement leaves the transaction usable for the
// statements that follow it.
func validateStatement(
	ctx context.Context,
	tx pgx.Tx,
	pgConn *pgconn.PgConn,
	stmt string,
	genericPlan bool,
) statementValidation {
	explainSQL, reason := explainCommand(stmt, genericPlan)
	if explainSQL == "" {
		return statementValidation{
			Query:  stmt,
			Status: validationUnsupported,
			Error:  reason,
		}
	}

	if _, err := tx.Exec(ctx, "SAVEPOINT "+validateSavepoint); err != nil {
		log.Printf("[ERROR] Failed to create validation savepoint: %v", err)
		return statementValidation{
			Query:  stmt,
			Status: validationUnsupported,
			Error:  "the database rejected the validation savepoint, so the statement was not validated",
		}
	}

	err := runExplain(ctx, pgConn, explainSQL)

	// Undo any error state and drop the savepoint again. Both are best
	// effort: a failure here shows up as an error on the next statement.
	_ = rollback.ToSavepoint[pgconn.CommandTag](ctx, tx, validateSavepoint) //nolint:errcheck // see pkg/rollback
	_, _ = tx.Exec(ctx, "RELEASE SAVEPOINT "+validateSavepoint)             //nolint:errcheck // best effort, see comment above

	if err == nil {
		return statementValidation{Query: stmt, Status: validationValid}
	}

	// Running out of time says nothing about whether the statement is
	// valid, so it must not be reported as a rejection.
	var pgErr *pgconn.PgError
	if ctx.Err() != nil ||
		(errors.As(err, &pgErr) && pgErr.Code == sqlStateQueryCanceled) {
		return statementValidation{
			Query:  stmt,
			Status: validationUnsupported,
			Error:  "planning the statement timed out, so it was not validated",
		}
	}

	return statementValidation{
		Query:  stmt,
		Status: validationInvalid,
		Error:  safeQueryError("Validation error", err),
	}
}

// runExplain runs one EXPLAIN on the transaction's own connection
// over the extended query protocol, binding every $N placeholder to
// NULL; EXPLAIN (GENERIC_PLAN) ignores the values, and a statement
// without placeholders binds none. The extended protocol matters for
// safety: PostgreSQL refuses to prepare more than one command, so SQL
// the splitter failed to divide is rejected rather than run, whereas
// the simple protocol would execute every command in it, including a
// COMMIT that ends the read-only transaction.
func runExplain(ctx context.Context, pgConn *pgconn.PgConn, explainSQL string) error {
	sd, err := pgConn.Prepare(ctx, "", explainSQL, nil)
	if err != nil {
		return err
	}
	params := make([][]byte, len(sd.ParamOIDs))
	// The plan itself is discarded; only the error matters.
	return pgConn.ExecPrepared(ctx, "", params, nil, nil).Read().Err
}

// explainCommand returns the EXPLAIN command that validates stmt, or
// an empty string and the reason validation is not possible. The
// command is built from the statement with its leading comments
// stripped, so that a leading line comment cannot comment out the
// EXPLAIN keyword.
func explainCommand(stmt string, genericPlan bool) (string, string) {
	body := strings.TrimSpace(stripLeadingComments(stmt))
	if body == "" {
		return "", "the statement contains no SQL, so it was not validated"
	}
	upper := strings.ToUpper(body)

	// An EXPLAIN the caller wrote is planned as it stands, except that
	// EXPLAIN ANALYZE would run the statement it explains. explainExecutes
	// also fails closed on an option list it cannot read, so the reason
	// names both causes rather than blaming an ANALYZE that may be absent.
	if strings.HasPrefix(upper, "EXPLAIN") {
		if explainExecutes(body) {
			return "", "the EXPLAIN names ANALYZE, or an option the " +
				"check cannot read, so it may run the statement it " +
				"explains and was not validated"
		}
		if containsBindPlaceholder(body) {
			return "", "the statement is an EXPLAIN carrying parameter " +
				"placeholders ($1, $2, ...), so it was not validated"
		}
		return body, ""
	}

	if !isExplainableStatement(upper) {
		return "", fmt.Sprintf(
			"PostgreSQL cannot plan a %s statement with EXPLAIN, "+
				"so it was not validated", firstSQLWord(upper))
	}

	if containsBindPlaceholder(body) {
		if !genericPlan {
			return "", "the statement carries parameter placeholders " +
				"($1, $2, ...) and this server predates EXPLAIN " +
				"(GENERIC_PLAN), added in PostgreSQL 16, so it was not validated"
		}
		return "EXPLAIN (GENERIC_PLAN) " + body, ""
	}

	return sqllex.PlainExplainPrefix + body, ""
}

// explainableStatements are the statement kinds this endpoint plans.
// PostgreSQL will also EXPLAIN a handful of others (EXECUTE, DECLARE,
// CREATE TABLE AS, REFRESH MATERIALIZED VIEW), which are deliberately
// left out: each depends on session or catalog state that
// validation cannot assume, so reporting them as unsupported is more
// honest than planning them.
var explainableStatements = []string{
	"SELECT",
	"WITH",
	"INSERT",
	"UPDATE",
	"DELETE",
	"MERGE",
	"VALUES",
	"TABLE",
}

// isExplainableStatement reports whether an upper-cased statement body
// begins with a keyword EXPLAIN can plan.
func isExplainableStatement(upper string) bool {
	word := firstSQLWord(upper)
	for _, kw := range explainableStatements {
		if word == kw {
			return true
		}
	}
	return false
}

// firstSQLWord returns the leading run of identifier characters in an
// upper-cased statement body, or "unrecognized" when the body starts
// with something else, such as an opening parenthesis.
func firstSQLWord(upper string) string {
	end := 0
	for end < len(upper) && sqllex.IsIdentChar(upper[end]) {
		end++
	}
	if end == 0 {
		return "unrecognized"
	}
	return upper[:end]
}
