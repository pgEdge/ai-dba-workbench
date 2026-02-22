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

// QueryDatabaseTool creates the query_database tool
func QueryDatabaseTool(dbClient *database.Client) Tool {
	return Tool{
		Definition: mcp.Tool{
			Name: "query_database",
			Description: `Execute SQL queries for STRUCTURED, EXACT data retrieval.

<database_context>
This tool operates on the CURRENTLY SELECTED monitored database connection.
If no database is selected, ask the user to select a database connection
before proceeding. The user can select a connection using their client
interface (CLI or web client).
</database_context>

<usecase>
Use query_database when you need:
- Exact matches by ID, status, date ranges, or specific column values
- Aggregations: COUNT, SUM, AVG, GROUP BY, HAVING
- Joins across tables using foreign keys
- Sorting or filtering by structured columns
- Transaction data, user records, system logs with known schema
- Checking existence, counts, or specific field values
</usecase>

<when_not_to_use>
DO NOT use for:
- Natural language content search → use similarity_search instead
- Finding topics, themes, or concepts in text → use similarity_search
- "Documents about X" queries → use similarity_search
- Semantic similarity or meaning-based queries → use similarity_search
</when_not_to_use>

<examples>
✓ "How many orders were placed last week?"
✓ "Show all users with status = 'active' and created_at > '2024-01-01'"
✓ "Average order value grouped by region"
✓ "Get user details for ID 12345"
✗ "Find documents about database performance" → use similarity_search
✗ "Show tickets related to connection issues" → use similarity_search
</examples>

<important>
- All queries run in READ-ONLY transactions (no data modifications possible)
- Results are limited to prevent excessive token usage
- Results are returned in TSV (tab-separated values) format for efficiency
</important>

<rate_limit_awareness>
To avoid rate limits (30,000 input tokens/minute):
- ALWAYS use the 'limit' parameter - it defaults to 100 rows
- Start with limit=10 for exploration queries, increase only if needed
- Filter results in WHERE clauses rather than fetching everything
- Use get_schema_info(schema_name="specific") to reduce metadata size
- If rate limited, wait 60 seconds before retrying
</rate_limit_awareness>`,
			CompactDescription: `Execute read-only SQL queries against the currently selected monitored database. Returns results in TSV format. Use for exact matches, aggregations, joins, and filtering by structured columns. For natural language or concept-based searches, use similarity_search instead.`,
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "SQL query to execute against the database. All queries run in read-only transactions.",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of rows to return (default: 100, max: 1000). Automatically appended to query if not already present. Use higher limits only when necessary to avoid excessive token usage.",
						"default":     100,
						"minimum":     1,
						"maximum":     1000,
					},
					"offset": map[string]interface{}{
						"type":        "integer",
						"description": "Number of rows to skip before returning results (for pagination). Use with limit to page through large result sets. Example: offset=100 with limit=100 returns rows 101-200.",
						"default":     0,
						"minimum":     0,
					},
				},
				Required: []string{"query"},
			},
		},
		Handler: func(args map[string]interface{}) (mcp.ToolResponse, error) {
			query, ok := args["query"].(string)
			if !ok {
				return mcp.NewToolError("Missing or invalid 'query' parameter")
			}

			// Parse query for connection string and intent
			queryCtx := database.ParseQueryForConnection(query)

			// Determine which connection to use
			connStr := dbClient.GetDefaultConnection()
			var connectionMessage string

			// Handle connection string changes
			if queryCtx.ConnectionString != "" {
				if queryCtx.SetAsDefault {
					// User wants to set a new default connection
					err := dbClient.SetDefaultConnection(queryCtx.ConnectionString)
					if err != nil {
						return mcp.NewToolError(fmt.Sprintf("Failed to set default connection to %s: %v", database.SanitizeConnStr(queryCtx.ConnectionString), err))
					}

					return mcp.NewToolSuccess(fmt.Sprintf("Successfully set default database connection to:\n%s\n\nMetadata loaded: %d tables/views available.",
						database.SanitizeConnStr(queryCtx.ConnectionString),
						len(dbClient.GetMetadata())))
				} else {
					// Temporary connection for this query only
					err := dbClient.ConnectTo(queryCtx.ConnectionString)
					if err != nil {
						return mcp.NewToolError(fmt.Sprintf("Failed to connect to %s: %v", database.SanitizeConnStr(queryCtx.ConnectionString), err))
					}

					// Load metadata if needed
					if !dbClient.IsMetadataLoadedFor(queryCtx.ConnectionString) {
						err = dbClient.LoadMetadataFor(queryCtx.ConnectionString)
						if err != nil {
							return mcp.NewToolError(fmt.Sprintf("Failed to load metadata from %s: %v", database.SanitizeConnStr(queryCtx.ConnectionString), err))
						}
					}

					connStr = queryCtx.ConnectionString
					connectionMessage = fmt.Sprintf("Using connection: %s\n\n", database.SanitizeConnStr(connStr))
				}
			}

			// If the cleaned query is empty (e.g., just a connection command), we're done
			if strings.TrimSpace(queryCtx.CleanedQuery) == "" {
				return mcp.NewToolSuccess("Connection command executed successfully. No query to run.")
			}

			// Check if metadata is loaded for the target connection
			if !dbClient.IsMetadataLoadedFor(connStr) {
				return mcp.NewToolError(mcp.DatabaseNotReadyError)
			}

			sqlQuery := strings.TrimSpace(queryCtx.CleanedQuery)
			limit, offset := parseLimitOffset(args)
			sqlQuery, hadExistingLimit, _ := injectLimitOffset(sqlQuery, limit, offset)

			// Extract context from args (injected by registry.Execute)
			ctx, ok := args["__context"].(context.Context)
			if !ok {
				ctx = context.Background()
			}
			pool := dbClient.GetPoolFor(connStr)
			if pool == nil {
				return mcp.NewToolError(fmt.Sprintf("Connection pool not found for: %s", database.SanitizeConnStr(connStr)))
			}

			qr, errResp := executeReadOnlyQuery(ctx, pool, sqlQuery, limit, hadExistingLimit, connectionMessage)
			if errResp != nil {
				return *errResp, nil
			}

			var sb strings.Builder

			// Always show current database context (unless already shown via connection message)
			if connectionMessage == "" {
				sanitizedConn := database.SanitizeConnStr(connStr)
				sb.WriteString(fmt.Sprintf("Database: %s\n\n", sanitizedConn))
			} else {
				sb.WriteString(connectionMessage)
			}

			formatPaginatedResults(&sb, qr, sqlQuery, limit, offset, " or count_rows for total")

			// Log execution metrics
			logging.Info("query_database_executed",
				"query_length", len(sqlQuery),
				"rows_returned", len(qr.Rows),
				"offset", offset,
				"was_truncated", qr.WasTruncated,
				"estimated_tokens", len(qr.ResultsTSV)/4,
			)

			return mcp.NewToolSuccess(sb.String())
		},
	}
}
