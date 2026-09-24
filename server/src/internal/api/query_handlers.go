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
func scanDollarTag(sql string, i int) string {
	if i >= len(sql) || sql[i] != '$' {
		return ""
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
// scanning for semicolons that are outside of single-quoted strings,
// dollar-quoted strings, line comments, and block comments (with
// nesting). It trims whitespace and filters out empty or
// comment-only statements.
func splitStatements(sql string) []string {
	var statements []string
	start := 0
	i := 0

	for i < len(sql) {
		ch := sql[i]

		// Single-quoted string
		if ch == '\'' {
			i++
			for i < len(sql) {
				if sql[i] == '\'' {
					if i+1 < len(sql) && sql[i+1] == '\'' {
						i += 2 // escaped quote ''
					} else {
						i++ // closing quote
						break
					}
				} else {
					i++
				}
			}
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
	pool, err := openQueryPool(ctx, connStr, "pgEdge AI DBA Workbench - Query")
	if err != nil {
		log.Printf("[ERROR] Failed to connect for query (connection=%d): %v", connectionID, err)
		RespondError(w, http.StatusInternalServerError,
			"Failed to connect to database")
		return
	}
	defer pool.Close()

	// For EXPLAIN queries with $N parameter placeholders, bypass the
	// standard query path and use pgconn's simple protocol directly.
	// pgx.Conn.Query always parses $N as bind parameters even in
	// simple protocol mode, but pgconn.Exec sends SQL text as-is.
	if needsSimpleProtocol(statements) {
		poolConn, err := pool.Acquire(ctx)
		if err != nil {
			log.Printf("[ERROR] Failed to acquire connection for EXPLAIN: %v", err)
			RespondError(w, http.StatusInternalServerError,
				"Failed to connect to database")
			return
		}
		defer poolConn.Release()

		results := make([]statementResult, 0, len(statements))
		for _, stmt := range statements {
			result := runSimpleStatement(ctx, poolConn.Conn().PgConn(), stmt, connectionID)
			results = append(results, result)
			if result.Error != "" {
				break
			}
		}
		RespondJSON(w, http.StatusOK, multiQueryResponse{
			Results:         results,
			TotalStatements: len(statements),
		})
		return
	}

	limit := defaultRowLimit
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

// isReadOnlyStatement returns true if the SQL statement (after stripping
// leading comments) begins with a read-only keyword: SELECT, WITH, SHOW,
// EXPLAIN, or TABLE. Writable CTEs (WITH ... INSERT/UPDATE/DELETE) are
// classified as non-read-only.
func isReadOnlyStatement(sql string) bool {
	body := strings.ToUpper(strings.TrimSpace(stripLeadingComments(sql)))

	if strings.HasPrefix(body, "WITH") {
		// Writable CTEs can perform data modification, e.g.
		// WITH deleted AS (DELETE FROM t RETURNING *) SELECT * FROM deleted.
		// Check for DML keywords as standalone words in the body.
		dmlKeywords := []string{"INSERT", "UPDATE", "DELETE"}
		for _, kw := range dmlKeywords {
			if containsSQLKeyword(body, kw) {
				return false
			}
		}
		return true
	}

	return strings.HasPrefix(body, "SELECT") ||
		strings.HasPrefix(body, "SHOW") ||
		strings.HasPrefix(body, "EXPLAIN") ||
		strings.HasPrefix(body, "TABLE ")
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
		body := strings.ToUpper(strings.TrimSpace(stripLeadingComments(stmt)))
		if strings.HasPrefix(body, "EXPLAIN") && containsDollarParam(stmt) {
			return true
		}
	}
	return false
}

// containsDollarParam checks whether the string contains a $N
// parameter placeholder (e.g. $1, $2).
func containsDollarParam(s string) bool {
	for i := 0; i < len(s)-1; i++ {
		if s[i] == '$' && s[i+1] >= '1' && s[i+1] <= '9' {
			return true
		}
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

// runSimpleStatement executes a statement using the pgconn simple
// protocol which sends SQL text directly to PostgreSQL without
// interpreting $N as bind parameters.  This is used for EXPLAIN
// queries that contain parameter placeholders from pg_stat_statements.
func runSimpleStatement(ctx context.Context, pgConn *pgconn.PgConn, stmt string, connectionID int) statementResult {
	mrr := pgConn.Exec(ctx, stmt)

	var columns []string
	var resultRows [][]string

	for mrr.NextResult() {
		rr := mrr.ResultReader()

		// Extract column names from field descriptions
		fds := rr.FieldDescriptions()
		if columns == nil {
			columns = make([]string, len(fds))
			for i, fd := range fds {
				columns[i] = string(fd.Name)
			}
		}

		for rr.NextRow() {
			row := rr.Values()
			values := make([]string, len(row))
			for i, col := range row {
				if col == nil {
					values[i] = "NULL"
				} else {
					values[i] = string(col)
				}
			}
			resultRows = append(resultRows, values)
		}

		_, err := rr.Close()
		if err != nil {
			log.Printf("[ERROR] Simple query close failed (connection=%d): %v",
				connectionID, err)
			return statementResult{
				Query: stmt,
				Error: safeQueryError("Query error", err),
			}
		}
	}

	err := mrr.Close()
	if err != nil {
		log.Printf("[ERROR] Simple query multi-result close failed (connection=%d): %v",
			connectionID, err)
		return statementResult{
			Query: stmt,
			Error: safeQueryError("Query error", err),
		}
	}

	if resultRows == nil {
		resultRows = [][]string{}
	}

	return statementResult{
		Columns:  columns,
		Rows:     resultRows,
		RowCount: len(resultRows),
		Query:    stmt,
	}
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
		RespondError(w, http.StatusForbidden,
			"Permission denied: you do not have access to this connection")
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
	if req.DatabaseName != "" {
		if err := database.ValidateDatabaseName(req.DatabaseName); err != nil {
			RespondError(w, http.StatusBadRequest,
				"Invalid database name: "+err.Error())
			return
		}
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
	// EXPLAIN ANALYZE would run the statement it explains.
	if strings.HasPrefix(upper, "EXPLAIN") {
		if containsSQLKeyword(upper, "ANALYZE") ||
			containsSQLKeyword(upper, "ANALYSE") { //nolint:misspell // ANALYSE is a PostgreSQL keyword
			return "", "EXPLAIN ANALYZE runs the statement it explains, " +
				"so it was not validated"
		}
		if containsDollarParam(body) {
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

	if containsDollarParam(body) {
		if !genericPlan {
			return "", "the statement carries parameter placeholders " +
				"($1, $2, ...) and this server predates EXPLAIN " +
				"(GENERIC_PLAN), added in PostgreSQL 16, so it was not validated"
		}
		return "EXPLAIN (GENERIC_PLAN) " + body, ""
	}

	return "EXPLAIN " + body, ""
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
	for end < len(upper) && isIdentChar(upper[end]) {
		end++
	}
	if end == 0 {
		return "unrecognized"
	}
	return upper[:end]
}
