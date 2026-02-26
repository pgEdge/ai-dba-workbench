/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/logging"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// TestQueryTool creates the test_query tool for validating SQL query correctness
func TestQueryTool(dbClient *database.Client, resolver *ConnectionResolver) Tool {
	return Tool{
		Definition: mcp.Tool{
			Name: "test_query",
			Description: `Validate a SQL query for correctness without executing it.

<database_context>
Specify connection_id to target a particular monitored database.
Use list_connections to discover available connection IDs and
their default databases. Optionally provide database_name to
override the default database for the connection. If
connection_id is omitted, the tool uses the currently selected
connection.
</database_context>

<usecase>
Use when:
- You have generated a SQL query and need to verify it is valid before
  presenting it to the user
- You want to check that table names, column names, and types are correct
- You need to validate multi-statement SQL scripts
</usecase>

<what_it_returns>
Returns one of two results:
- A success message confirming the query is valid
- The PostgreSQL error message explaining why the query is invalid
</what_it_returns>

<when_not_to_use>
DO NOT use for:
- Performance analysis (use execute_explain instead)
- Actually running the query (use query_database instead)
</when_not_to_use>

<important>
- This tool validates ANY SQL statement type (SELECT, INSERT, UPDATE, DELETE, DDL)
- The query is never executed; EXPLAIN is used inside a read-only transaction
- Multi-statement queries are supported; each statement is validated individually
- Always use this tool to validate SQL you generate before presenting it to the user
</important>`,
			CompactDescription: `Validate SQL query correctness without executing it. Specify connection_id to target a database; use list_connections to discover IDs. Uses EXPLAIN in a read-only transaction.`,
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"connection_id": map[string]interface{}{
						"type":        "integer",
						"description": "ID of the monitored database connection to use. Use list_connections to discover available IDs. If omitted, uses the currently selected connection.",
					},
					"database_name": map[string]interface{}{
						"type":        "string",
						"description": "Database name to connect to. If omitted, uses the connection's default database.",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "The SQL query to validate",
					},
				},
				Required: []string{"query"},
			},
		},
		Handler: func(args map[string]interface{}) (mcp.ToolResponse, error) {
			// Extract and validate parameters
			query, ok := args["query"].(string)
			if !ok || query == "" {
				return mcp.NewToolError("Parameter 'query' is required and must be a non-empty string")
			}

			// Extract context from args (injected by registry.Execute)
			ctx, ok := args["__context"].(context.Context)
			if !ok {
				ctx = context.Background()
			}

			// Resolve connection (explicit connection_id or fallback)
			resolved, errResp := resolver.Resolve(ctx, args, dbClient)
			if errResp != nil {
				return *errResp, nil
			}
			pool := resolved.Pool

			// Open a read-only transaction
			rot, errResp, cleanup := BeginReadOnlyTx(ctx, pool)
			if errResp != nil {
				return *errResp, nil
			}
			defer cleanup()

			// Try EXPLAIN on the full query first
			explainQuery := "EXPLAIN " + query
			rows, err := rot.Tx.Query(ctx, explainQuery)
			if err != nil {
				// Check if the error is about multiple statements
				if isMultipleStatementError(err) {
					// Split and validate each statement individually
					statements := splitStatements(query)
					if len(statements) == 0 {
						return mcp.NewToolError("Query contains no valid SQL statements")
					}

					for i, stmt := range statements {
						stmtExplain := "EXPLAIN " + stmt
						stmtRows, stmtErr := rot.Tx.Query(ctx, stmtExplain)
						if stmtErr != nil {
							return mcp.NewToolError(fmt.Sprintf(
								"Statement %d is invalid: %v\n\nStatement: %s",
								i+1, stmtErr, stmt))
						}
						stmtRows.Close()
					}
				} else {
					return mcp.NewToolError(fmt.Sprintf("Query validation failed: %v", err))
				}
			} else {
				rows.Close()
			}

			// Log the validation
			logging.Info("test_query_executed",
				"query_length", len(query),
				"result", "valid",
			)

			return mcp.NewToolSuccess("Query is valid.")
		},
	}
}

// isMultipleStatementError checks if a PostgreSQL error indicates that
// multiple commands were provided where only one was expected.
func isMultipleStatementError(err error) bool {
	var pgErr *pgconn.PgError
	if ok := isPgError(err, &pgErr); ok {
		// 42601 is syntax_error; PostgreSQL uses this code for
		// "cannot insert multiple commands into a prepared statement"
		if pgErr.Code == "42601" && strings.Contains(pgErr.Message, "multiple commands") {
			return true
		}
	}
	// Fallback: check the error message string
	return strings.Contains(err.Error(), "multiple commands") ||
		strings.Contains(err.Error(), "cannot insert multiple commands")
}

// isPgError attempts to extract a *pgconn.PgError from the given error.
func isPgError(err error, target **pgconn.PgError) bool {
	var pgErr *pgconn.PgError
	if ok := errors.As(err, &pgErr); ok { //nolint:errorlint // using errors.As
		*target = pgErr
		return true
	}
	return false
}

// splitStatements splits a SQL string into individual statements using a
// SQL-aware tokeniser. The function correctly handles semicolons inside
// single-quoted strings, double-quoted identifiers, dollar-quoted strings,
// block comments, and line comments.
func splitStatements(sql string) []string {
	var statements []string
	var current strings.Builder
	i := 0
	n := len(sql)

	for i < n {
		ch := sql[i]

		// Line comment: -- until end of line
		if ch == '-' && i+1 < n && sql[i+1] == '-' {
			current.WriteByte(ch)
			i++
			current.WriteByte(sql[i])
			i++
			for i < n && sql[i] != '\n' {
				current.WriteByte(sql[i])
				i++
			}
			continue
		}

		// Block comment: /* ... */
		if ch == '/' && i+1 < n && sql[i+1] == '*' {
			current.WriteByte(ch)
			i++
			current.WriteByte(sql[i])
			i++
			depth := 1
			for i < n && depth > 0 {
				if sql[i] == '/' && i+1 < n && sql[i+1] == '*' {
					depth++
					current.WriteByte(sql[i])
					i++
					current.WriteByte(sql[i])
					i++
				} else if sql[i] == '*' && i+1 < n && sql[i+1] == '/' {
					depth--
					current.WriteByte(sql[i])
					i++
					current.WriteByte(sql[i])
					i++
				} else {
					current.WriteByte(sql[i])
					i++
				}
			}
			continue
		}

		// Single-quoted string
		if ch == '\'' {
			current.WriteByte(ch)
			i++
			for i < n {
				if sql[i] == '\'' {
					current.WriteByte(sql[i])
					i++
					// Escaped quote ('')
					if i < n && sql[i] == '\'' {
						current.WriteByte(sql[i])
						i++
						continue
					}
					break
				}
				current.WriteByte(sql[i])
				i++
			}
			continue
		}

		// Double-quoted identifier
		if ch == '"' {
			current.WriteByte(ch)
			i++
			for i < n {
				if sql[i] == '"' {
					current.WriteByte(sql[i])
					i++
					// Escaped quote ("")
					if i < n && sql[i] == '"' {
						current.WriteByte(sql[i])
						i++
						continue
					}
					break
				}
				current.WriteByte(sql[i])
				i++
			}
			continue
		}

		// Dollar-quoted string: $tag$...$tag$ or $$...$$
		if ch == '$' {
			tag := parseDollarTag(sql, i)
			if tag != "" {
				// Write the opening tag
				current.WriteString(tag)
				i += len(tag)
				// Find the closing tag
				for i < n {
					if sql[i] == '$' && strings.HasPrefix(sql[i:], tag) {
						current.WriteString(tag)
						i += len(tag)
						break
					}
					current.WriteByte(sql[i])
					i++
				}
				continue
			}
			// Not a dollar-quote tag; treat $ as normal character
			current.WriteByte(ch)
			i++
			continue
		}

		// Semicolon: statement boundary
		if ch == ';' {
			stmt := strings.TrimSpace(current.String())
			if stmt != "" {
				statements = append(statements, stmt)
			}
			current.Reset()
			i++
			continue
		}

		// Normal character
		current.WriteByte(ch)
		i++
	}

	// Remaining text after the last semicolon (or if no semicolons)
	stmt := strings.TrimSpace(current.String())
	if stmt != "" {
		statements = append(statements, stmt)
	}

	return statements
}

// parseDollarTag checks if the string at position i starts with a valid
// dollar-quote tag (e.g. "$$" or "$tag$"). Returns the full tag including
// both dollar signs, or an empty string if no valid tag is found.
func parseDollarTag(sql string, i int) string {
	n := len(sql)
	if i >= n || sql[i] != '$' {
		return ""
	}

	// Try to find the closing $ of the tag
	j := i + 1
	// $$ is valid (empty tag)
	if j < n && sql[j] == '$' {
		return "$$"
	}

	// Tag must start with a letter or underscore
	if j >= n || (!isTagStart(sql[j])) {
		return ""
	}

	// Scan tag body: letters, digits, underscores
	for j < n && isTagBody(sql[j]) {
		j++
	}

	// Must end with $
	if j < n && sql[j] == '$' {
		return sql[i : j+1]
	}

	return ""
}

// isTagStart returns true if the byte can start a dollar-quote tag name.
func isTagStart(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_'
}

// isTagBody returns true if the byte can appear in a dollar-quote tag name.
func isTagBody(b byte) bool {
	return isTagStart(b) || (b >= '0' && b <= '9')
}
