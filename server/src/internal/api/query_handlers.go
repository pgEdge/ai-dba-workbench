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

// scanDollarTag checks whether sql[i] starts a dollar-quote tag. If a
// valid tag is found (either $$ or $identifier$), it returns the full
// tag string. Otherwise it returns an empty string.
//
// A dollar sign that continues an identifier, as in foo$bar$ (PostgreSQL
// allows $ after the first character of an unquoted identifier, and
// treats any byte from 0x80 up as an identifier character), is part of
// that identifier rather than the start of a dollar quote, so it yields
// no tag. Reading one there would let a later $bar$ hide the code in
// between from the classifier.
func scanDollarTag(sql string, i int) string {
	if i >= len(sql) || sql[i] != '$' {
		return ""
	}
	if i > 0 {
		if prev := sql[i-1]; isIdentChar(prev) || prev == '$' || prev >= 0x80 {
			return ""
		}
	}
	// Check for $$ (empty tag)
	if i+1 < len(sql) && sql[i+1] == '$' {
		return "$$"
	}
	// Check for $identifier$ where identifier is [A-Za-z_][A-Za-z0-9_]*
	j := i + 1
	if j >= len(sql) {
		return ""
	}
	ch := sql[j]
	if (ch < 'A' || ch > 'Z') && (ch < 'a' || ch > 'z') && ch != '_' {
		return ""
	}
	j++
	for j < len(sql) {
		ch = sql[j]
		if ch == '$' {
			return sql[i : j+1]
		}
		if (ch < 'A' || ch > 'Z') && (ch < 'a' || ch > 'z') &&
			(ch < '0' || ch > '9') && ch != '_' {
			return ""
		}
		j++
	}
	return ""
}

// hasOnlyComments returns true when the string contains only SQL
// comments (line and block) and whitespace but no real SQL content.
func hasOnlyComments(s string) bool {
	i := 0
	for i < len(s) {
		ch := s[i]
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			i++
			continue
		}
		if ch == '-' && i+1 < len(s) && s[i+1] == '-' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		if ch == '/' && i+1 < len(s) && s[i+1] == '*' {
			depth := 1
			i += 2
			for i < len(s) && depth > 0 {
				if s[i] == '/' && i+1 < len(s) && s[i+1] == '*' {
					depth++
					i += 2
				} else if s[i] == '*' && i+1 < len(s) && s[i+1] == '/' {
					depth--
					i += 2
				} else {
					i++
				}
			}
			continue
		}
		return false
	}
	return true
}

// splitStatements splits a SQL string into individual statements by
// scanning for semicolons that are outside of quoted strings, quoted
// identifiers, dollar-quoted strings, line comments, and block comments
// (with nesting). It trims whitespace and filters out empty or
// comment-only statements.
func splitStatements(sql string) []string {
	var statements []string
	start := 0
	i := 0

	for i < len(sql) {
		ch := sql[i]

		// Quoted string or quoted identifier, including the prefixed
		// forms E'...', U&'...' and U&"...".
		if isQuoteStart(sql, i) {
			i = skipQuoted(sql, i)
			continue
		}

		// Dollar-quoted string
		if ch == '$' {
			tag := scanDollarTag(sql, i)
			if tag != "" {
				i += len(tag)
				closeIdx := strings.Index(sql[i:], tag)
				if closeIdx < 0 {
					i = len(sql)
				} else {
					i += closeIdx + len(tag)
				}
				continue
			}
			i++
			continue
		}

		// Line comment
		if ch == '-' && i+1 < len(sql) && sql[i+1] == '-' {
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			continue
		}

		// Block comment (with nesting)
		if ch == '/' && i+1 < len(sql) && sql[i+1] == '*' {
			depth := 1
			i += 2
			for i < len(sql) && depth > 0 {
				if sql[i] == '/' && i+1 < len(sql) && sql[i+1] == '*' {
					depth++
					i += 2
				} else if sql[i] == '*' && i+1 < len(sql) && sql[i+1] == '/' {
					depth--
					i += 2
				} else {
					i++
				}
			}
			continue
		}

		// Semicolon: split point
		if ch == ';' {
			part := strings.TrimSpace(sql[start:i])
			if part != "" && !hasOnlyComments(part) {
				statements = append(statements, part)
			}
			i++
			start = i
			continue
		}

		i++
	}

	// Handle trailing statement (no final semicolon)
	if start < len(sql) {
		part := strings.TrimSpace(sql[start:])
		if part != "" && !hasOnlyComments(part) {
			statements = append(statements, part)
		}
	}

	return statements
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
		RespondError(w, http.StatusForbidden,
			"Permission denied: you do not have access to this connection")
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
			RespondError(w, http.StatusForbidden,
				"Permission denied: you do not have write access to this connection")
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
	poolConfig, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		log.Printf("[ERROR] Failed to parse connection string for query: %v", err)
		RespondError(w, http.StatusInternalServerError,
			"Failed to connect to database")
		return
	}

	// Configure the pool for single-use with minimal resources
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 0
	if poolConfig.ConnConfig.RuntimeParams == nil {
		poolConfig.ConnConfig.RuntimeParams = make(map[string]string)
	}
	poolConfig.ConnConfig.RuntimeParams["application_name"] = "pgEdge AI DBA Workbench - Query"

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
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

		results := runSimpleStatements(ctx, poolConn.Conn().PgConn(),
			statements, limit, connectionID, allReadOnly)

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
		// transaction so that statements like ALTER SYSTEM work.
		for _, stmt := range statements {
			if isReadOnlyStatement(stmt) {
				result := runStatement(ctx, pool, stmt, limit, connectionID)
				results = append(results, result)
				if result.Error != "" {
					break
				}
			} else {
				_, err := pool.Exec(ctx, stmt)
				if err != nil {
					log.Printf("[ERROR] Write statement failed (connection=%d): %v",
						connectionID, err)
					results = append(results, statementResult{
						Query: stmt,
						Error: safeQueryError("Execution error", err),
					})
					break
				}
				results = append(results, statementResult{
					Query:    stmt,
					Columns:  []string{"result"},
					Rows:     [][]string{{"Statement executed successfully"}},
					RowCount: 1,
				})
			}
		}
	}

	resp := multiQueryResponse{
		Results:         results,
		TotalStatements: len(statements),
	}

	RespondJSON(w, http.StatusOK, resp)
}

// stripLeadingComments removes leading SQL line comments (-- ...),
// block comments (/* ... */ with nesting), and blank lines from a SQL
// string, returning the remaining statement body. This allows
// detection of the first SQL keyword even when the statement begins
// with comments.
func stripLeadingComments(sql string) string {
	i := 0
	for i < len(sql) {
		ch := sql[i]

		// Skip whitespace
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			i++
			continue
		}

		// Line comment: skip to end of line
		if ch == '-' && i+1 < len(sql) && sql[i+1] == '-' {
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			continue
		}

		// Block comment with nesting
		if ch == '/' && i+1 < len(sql) && sql[i+1] == '*' {
			depth := 1
			i += 2
			for i < len(sql) && depth > 0 {
				if sql[i] == '/' && i+1 < len(sql) && sql[i+1] == '*' {
					depth++
					i += 2
				} else if sql[i] == '*' && i+1 < len(sql) && sql[i+1] == '/' {
					depth--
					i += 2
				} else {
					i++
				}
			}
			continue
		}

		// Found a non-comment, non-whitespace character
		return sql[i:]
	}
	return ""
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
// (see hasWritingSelectClause) and an EXPLAIN that may execute a writing
// inner statement (issue #530).
func isReadOnlyStatement(sql string) bool {
	return isReadOnlyStatementAtDepth(sql, 0)
}

// isReadOnlyStatementAtDepth is isReadOnlyStatement with the EXPLAIN
// nesting depth reached so far.
func isReadOnlyStatementAtDepth(sql string, depth int) bool {
	body := strings.TrimSpace(stripLeadingComments(sql))

	// The EXPLAIN test runs against the original text rather than an
	// uppercased copy, because strings.ToUpper is not length-preserving
	// for every input (U+0131 gets shorter), so an offset measured on
	// the copy cannot safely slice the original.
	if hasKeywordPrefix(body, "EXPLAIN") {
		return isReadOnlyExplain(body, depth)
	}

	// Mask the quoted literals, quoted identifiers and comments before
	// uppercasing, so the keyword scans below read code only and a
	// statement such as SELECT * FROM audit WHERE msg LIKE '%signed
	// into%' is not classified as a write. The masking is for
	// classification alone; the text that is executed is untouched.
	upper := strings.ToUpper(maskNonCode(body))

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
		return !hasWritingSelectClause(upper)
	}

	if strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "TABLE ") {
		return !hasWritingSelectClause(upper)
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
// have had its non-code regions masked (see maskNonCode) and been
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
		if !isIdentChar(s[i]) {
			i++
			continue
		}
		start := i
		for i < len(s) && isIdentChar(s[i]) {
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
func isReadOnlyExplain(body string, depth int) bool {
	rest := strings.TrimSpace(stripLeadingComments(body[len("EXPLAIN"):]))

	executes := false
	if strings.HasPrefix(rest, "(") {
		// Parenthesised form: EXPLAIN ( option [, ...] ) statement.
		options, remainder, ok := splitExplainOptions(rest)
		if !ok {
			// Unbalanced parentheses: the statement is malformed, so
			// fail closed rather than guess at the option list.
			return false
		}
		executes = explainOptionsExecute(options)
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

	if !executes {
		return true
	}
	if rest == "" || depth >= maxExplainDepth {
		return false
	}
	return isReadOnlyStatementAtDepth(rest, depth+1)
}

// explainOptionsExecute reports whether a parenthesised EXPLAIN option
// list may cause the inner statement to run. It inspects option names
// only, never their values, and treats a name it cannot read as a bare
// recognized identifier as an execution.
func explainOptionsExecute(options string) bool {
	entries := splitExplainOptionEntries(options)
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
func splitExplainOptionEntries(options string) []string {
	var entries []string
	depth := 0
	start := 0
	i := 0

	for i < len(options) {
		if j := skipNonCode(options, i); j != i {
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
func splitExplainOptions(s string) (options, remainder string, ok bool) {
	depth := 0
	i := 0
	for i < len(s) {
		if j := skipNonCode(s, i); j != i {
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

// literalPrefix reports the length of the string-literal prefix at s[i]
// and whether a backslash escapes the following character inside that
// literal. PostgreSQL accepts E'...' (backslash escapes), U&'...' and
// U&"..." (Unicode escapes), and the B'...', X'...' and N'...' forms,
// each of which introduces a literal that a bare quote scan would start
// one or two bytes late. The length is zero when no prefix starts here.
func literalPrefix(s string, i int) (length int, backslashEscapes bool) {
	if i+1 >= len(s) {
		return 0, false
	}
	switch s[i] {
	case 'E', 'e':
		if s[i+1] == '\'' {
			return 1, true
		}
	case 'B', 'b', 'X', 'x', 'N', 'n':
		if s[i+1] == '\'' {
			return 1, false
		}
	case 'U', 'u':
		if s[i+1] == '&' && i+2 < len(s) &&
			(s[i+2] == '\'' || s[i+2] == '"') {
			return 2, false
		}
	}
	return 0, false
}

// isQuoteStart reports whether a quoted string or quoted identifier
// starts at s[i], either with the quote itself or with one of the
// literal prefixes literalPrefix recognizes. A prefix letter only counts
// when it does not continue an identifier, so the trailing e of a column
// name is not mistaken for an E'...' literal.
func isQuoteStart(s string, i int) bool {
	if s[i] == '\'' || s[i] == '"' {
		return true
	}
	if i > 0 && isIdentChar(s[i-1]) {
		return false
	}
	length, _ := literalPrefix(s, i)
	return length > 0
}

// skipQuoted returns the index just past the quoted string or quoted
// identifier starting at s[i], which must be a single or double quote or
// the first character of a literal prefix (see literalPrefix). A doubled
// quote inside the literal is an escaped quote rather than a terminator,
// and inside an E'...' literal a backslash escapes the character that
// follows it, so a backslash-escaped quote does not terminate it.
// An unterminated literal runs to the end of the string.
func skipQuoted(s string, i int) int {
	escapes := false
	if s[i] != '\'' && s[i] != '"' {
		length, backslashEscapes := literalPrefix(s, i)
		if length == 0 {
			return i + 1
		}
		escapes = backslashEscapes
		i += length
	}

	quote := s[i]
	i++
	for i < len(s) {
		switch {
		case escapes && s[i] == '\\':
			i += 2
		case s[i] == quote:
			if i+1 < len(s) && s[i+1] == quote {
				i += 2
				continue
			}
			return i + 1
		default:
			i++
		}
	}
	return len(s)
}

// skipBlockComment returns the index just past the (possibly nested)
// block comment starting at s[i], which must be the opening slash.
func skipBlockComment(s string, i int) int {
	depth := 1
	i += 2
	for i < len(s) && depth > 0 {
		if s[i] == '/' && i+1 < len(s) && s[i+1] == '*' {
			depth++
			i += 2
		} else if s[i] == '*' && i+1 < len(s) && s[i+1] == '/' {
			depth--
			i += 2
		} else {
			i++
		}
	}
	return i
}

// skipNonCode returns the index just past the quoted literal or comment
// starting at s[i], or i itself when s[i] does not begin one. It is the
// step every scan over statement text shares: a quoted string, a quoted
// identifier (including the prefixed E'...', U&'...' and U&"..." forms),
// a dollar-quoted string, a line comment or a block comment holds text
// rather than SQL, so a comma, parenthesis, keyword or $N inside one
// must not be read as code, and a quote or comment marker inside a
// dollar-quoted string must not open a region that swallows the code
// after it. An unterminated literal or comment runs to the end of the
// string, which ends the caller's scan rather than resuming inside the
// quoted text. A $N placeholder is not a dollar quote (see
// scanDollarTag), so it is left for the caller to see.
func skipNonCode(s string, i int) int {
	if isQuoteStart(s, i) {
		return skipQuoted(s, i)
	}
	if tag := scanDollarTag(s, i); tag != "" {
		end := strings.Index(s[i+len(tag):], tag)
		if end < 0 {
			return len(s)
		}
		return i + len(tag) + end + len(tag)
	}
	if s[i] == '-' && i+1 < len(s) && s[i+1] == '-' {
		j := i
		for j < len(s) && s[j] != '\n' {
			j++
		}
		return j
	}
	if s[i] == '/' && i+1 < len(s) && s[i+1] == '*' {
		return skipBlockComment(s, i)
	}
	return i
}

// maskNonCode replaces every quoted literal, quoted identifier and
// comment in s with spaces, so a keyword scan reads code only. Each
// masked byte becomes one space, leaving the result the same length as
// s, which keeps any offset measured on the mask valid against the
// original. Callers must mask before uppercasing, because
// strings.ToUpper is not length-preserving for every input.
func maskNonCode(s string) string {
	masked := []byte(s)
	for i := 0; i < len(masked); {
		j := skipNonCode(s, i)
		if j == i {
			i++
			continue
		}
		for k := i; k < j; k++ {
			masked[k] = ' '
		}
		i = j
	}
	return string(masked)
}

// nextSQLWord splits the leading identifier-character run off s,
// returning that word and the rest of the string. The word is empty
// when s does not start with an identifier character.
func nextSQLWord(s string) (word, rest string) {
	i := 0
	for i < len(s) && isIdentChar(s[i]) {
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
	return len(sql) == len(keyword) || !isIdentChar(sql[len(keyword)])
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

		startOK := pos == 0 || !isIdentChar(upperSQL[pos-1])
		endOK := end >= len(upperSQL) || !isIdentChar(upperSQL[end])

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
// rather than reading the quoted text as SQL.
func containsDollarParam(s string) bool {
	i := 0
	for i < len(s) {
		if j := skipNonCode(s, i); j != i {
			i = j
			continue
		}

		// skipNonCode has already stepped over any dollar-quoted
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

// isIdentChar returns true if the byte is a valid SQL identifier character
// (letter, digit, or underscore).
func isIdentChar(b byte) bool {
	return (b >= 'A' && b <= 'Z') ||
		(b >= 'a' && b <= 'z') ||
		(b >= '0' && b <= '9') ||
		b == '_'
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
		return runSimpleStatement(runCtx, pgConn, stmt, limit, connectionID)
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
