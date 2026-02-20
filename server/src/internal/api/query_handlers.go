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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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
	if !((ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || ch == '_') {
		return ""
	}
	j++
	for j < len(sql) {
		ch = sql[j]
		if ch == '$' {
			return sql[i : j+1]
		}
		if !((ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') ||
			(ch >= '0' && ch <= '9') || ch == '_') {
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
				_ = tx.Rollback(ctx) //nolint:errcheck // rollback in defer after commit is expected to fail
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
						Error: fmt.Sprintf("Execution error: %v", err),
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

// isIdentChar returns true if the byte is a valid SQL identifier character
// (letter, digit, or underscore).
func isIdentChar(b byte) bool {
	return (b >= 'A' && b <= 'Z') ||
		(b >= 'a' && b <= 'z') ||
		(b >= '0' && b <= '9') ||
		b == '_'
}

// queryable is an interface satisfied by both pgx.Tx and *pgxpool.Pool,
// allowing runStatement to execute queries against either.
type queryable interface {
	Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error)
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
	upperQuery := strings.ToUpper(sqlQuery)
	hasExistingLimit := strings.Contains(upperQuery, "LIMIT")
	if isSelect && !hasExistingLimit {
		sqlQuery = fmt.Sprintf("%s LIMIT %d", sqlQuery, limit+1)
	}

	// Execute the statement
	rows, err := q.Query(ctx, sqlQuery)
	if err != nil {
		log.Printf("[ERROR] Query execution failed (connection=%d): %v", connectionID, err)
		return statementResult{
			Query: stmt,
			Error: fmt.Sprintf("Query error: %v", err),
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
				Error: fmt.Sprintf("Failed to read row: %v", err),
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
			Error: fmt.Sprintf("Failed to read query results: %v", err),
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

// formatValueForJSON converts a database value to a string for JSON
// serialization. This reuses the tsv.FormatValue logic but without TSV
// escaping since JSON handles special characters natively.
func formatValueForJSON(v interface{}) (result string) {
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
