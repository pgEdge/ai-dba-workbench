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
	"fmt"
	"strings"

	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/logging"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// CountRowsTool creates the count_rows tool for lightweight row counting
func CountRowsTool(dbClient *database.Client) Tool {
	return Tool{
		Definition: mcp.Tool{
			Name: "count_rows",
			Description: `Get the row count of a table with optional filtering.

<database_context>
This tool operates on the CURRENTLY SELECTED monitored database connection.
If no database is selected, ask the user to select a database connection
before proceeding. The user can select a connection using their client
interface (CLI or web client).
</database_context>

<usecase>
Use count_rows to efficiently determine data volume:
- Check total row count before querying large tables
- Verify filter conditions match expected number of rows
- Plan query strategies based on data size
- Validate data existence without fetching rows
</usecase>

<examples>
✓ count_rows(table="orders") → Total orders in database
✓ count_rows(table="orders", schema="sales") → Orders in sales schema
✓ count_rows(table="orders", where="status = 'pending'") → Pending orders only
✓ count_rows(table="users", where="created_at > '2024-01-01'") → Recent users
</examples>

<important>
- Much more efficient than SELECT * with LIMIT for checking data volume
- Use this before query_database to plan appropriate LIMIT values
- WHERE clause is optional - omit for total count
- Returns a single integer count - minimal token usage
</important>`,
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"table": map[string]interface{}{
						"type":        "string",
						"description": "Name of the table to count rows from",
					},
					"schema": map[string]interface{}{
						"type":        "string",
						"description": "Schema name (default: public)",
						"default":     "public",
					},
					"where": map[string]interface{}{
						"type":        "string",
						"description": "Optional WHERE clause condition (without the WHERE keyword). Example: \"status = 'active' AND created_at > '2024-01-01'\"",
					},
				},
				Required: []string{"table"},
			},
		},
		Handler: func(args map[string]interface{}) (mcp.ToolResponse, error) {
			table, ok := args["table"].(string)
			if !ok || table == "" {
				return mcp.NewToolError("Missing or invalid 'table' parameter")
			}

			// Validate table name to prevent SQL injection
			if err := ValidateIdentifier(table); err != nil {
				return mcp.NewToolError(fmt.Sprintf("Invalid table name: %v", err))
			}

			// Get schema, default to public
			schema := "public"
			if s, ok := args["schema"].(string); ok && s != "" {
				schema = s
			}

			// Validate schema name to prevent SQL injection
			if err := ValidateIdentifier(schema); err != nil {
				return mcp.NewToolError(fmt.Sprintf("Invalid schema name: %v", err))
			}

			// Get optional WHERE clause
			// Note: WHERE clause is user-provided SQL and cannot be fully
			// parameterized. This is intentional for MCP tools that allow
			// arbitrary SQL queries. The table and schema names are validated
			// and quoted to prevent injection in those parts.
			// Additional safeguards block dangerous SQL patterns.
			whereClause := ""
			if w, ok := args["where"].(string); ok && w != "" {
				if err := validateWhereClause(w); err != nil {
					return mcp.NewToolError(fmt.Sprintf("Invalid WHERE clause: %v", err))
				}
				whereClause = w
			}

			// Get connection
			connStr := dbClient.GetDefaultConnection()
			if !dbClient.IsMetadataLoadedFor(connStr) {
				return mcp.NewToolError(mcp.DatabaseNotReadyError)
			}

			pool := dbClient.GetPoolFor(connStr)
			if pool == nil {
				return mcp.NewToolError(fmt.Sprintf("Connection pool not found for: %s", database.SanitizeConnStr(connStr)))
			}

			// Build the COUNT query with proper quoting
			var sqlQuery string
			if whereClause != "" {
				sqlQuery = fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s WHERE %s`,
					quoteIdentifier(schema),
					quoteIdentifier(table),
					whereClause)
			} else {
				sqlQuery = fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s`,
					quoteIdentifier(schema),
					quoteIdentifier(table))
			}

			// Extract context from args (injected by registry.Execute)
			ctx, ok := args["__context"].(context.Context)
			if !ok {
				ctx = context.Background()
			}

			// Execute in a read-only transaction
			tx, err := pool.Begin(ctx)
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to begin transaction: %v", err))
			}

			committed := false
			defer func() {
				if r := recover(); r != nil {
					_ = tx.Rollback(ctx) //nolint:errcheck // Best effort cleanup on panic
					panic(r)
				}
				if !committed {
					_ = tx.Rollback(ctx) //nolint:errcheck // rollback in defer after commit is expected to fail
				}
			}()

			// Set transaction to read-only
			_, err = tx.Exec(ctx, "SET TRANSACTION READ ONLY")
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to set transaction read-only: %v", err))
			}

			var count int64
			err = tx.QueryRow(ctx, sqlQuery).Scan(&count)
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("SQL Query:\n%s\n\nError: %v", sqlQuery, err))
			}

			if err := tx.Commit(ctx); err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to commit transaction: %v", err))
			}
			committed = true

			// Log execution
			logging.Info("count_rows_executed",
				"schema", schema,
				"table", table,
				"has_where", whereClause != "",
				"count", count,
			)

			// Build response
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("Database: %s\n\n", database.SanitizeConnStr(connStr)))
			sb.WriteString(fmt.Sprintf("SQL Query:\n%s\n\n", sqlQuery))
			sb.WriteString(fmt.Sprintf("Count: %d", count))

			return mcp.NewToolSuccess(sb.String())
		},
	}
}

// quoteIdentifier quotes a SQL identifier to prevent injection
func quoteIdentifier(name string) string {
	// Double any existing double quotes and wrap in double quotes
	escaped := strings.ReplaceAll(name, `"`, `""`)
	return `"` + escaped + `"`
}

// quoteQualifiedTableName quotes a table name that may include a schema prefix.
// For "schema.table", it quotes each part separately: "schema"."table"
// For simple "table", it just quotes the table name: "table"
func quoteQualifiedTableName(name string) string {
	parts := strings.Split(name, ".")
	if len(parts) == 2 {
		return quoteIdentifier(parts[0]) + "." + quoteIdentifier(parts[1])
	}
	return quoteIdentifier(name)
}

// validateWhereClause checks a WHERE clause for dangerous SQL patterns.
// While this tool intentionally allows arbitrary SQL for DBA use cases,
// we block patterns that could be used for denial of service or file access.
// The query already runs in a read-only transaction, but these additional
// safeguards prevent time-based attacks and access to PostgreSQL internals.
func validateWhereClause(clause string) error {
	lower := strings.ToLower(clause)

	// Dangerous patterns that could enable attacks even in read-only mode
	dangerousPatterns := []struct {
		pattern string
		reason  string
	}{
		{"pg_sleep", "time-based denial of service"},
		{"pg_cancel_backend", "can cancel other queries"},
		{"pg_terminate_backend", "can terminate connections"},
		{"dblink", "can connect to external databases"},
		{"copy ", "file system access"},
		{"lo_import", "large object file import"},
		{"lo_export", "large object file export"},
		{"pg_read_file", "file system read access"},
		{"pg_read_binary_file", "file system read access"},
		{"pg_ls_dir", "directory listing"},
		{"pg_stat_file", "file system stat access"},
	}

	for _, dp := range dangerousPatterns {
		if strings.Contains(lower, dp.pattern) {
			return fmt.Errorf("WHERE clause contains disallowed pattern '%s' (%s)", dp.pattern, dp.reason)
		}
	}

	// Block statement terminators and comment markers that could enable injection
	// Note: Single statements without these are allowed for legitimate filtering
	injectionPatterns := []struct {
		pattern string
		reason  string
	}{
		{";", "multiple statements not allowed"},
		{"--", "SQL comments not allowed"},
		{"/*", "SQL block comments not allowed"},
	}

	for _, ip := range injectionPatterns {
		if strings.Contains(clause, ip.pattern) {
			return fmt.Errorf("WHERE clause contains disallowed pattern '%s' (%s)", ip.pattern, ip.reason)
		}
	}

	return nil
}
