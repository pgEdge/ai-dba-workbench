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

// queryDatabaseReadOnlyError is returned for any statement that
// isSelectQuery does not classify as a read.
const queryDatabaseReadOnlyError = "query_database is read-only: it does not run statements " +
	"that modify the database or server state, such as INSERT, UPDATE, DELETE or DDL. " +
	"Use a SELECT, WITH, SHOW, VALUES, TABLE or EXPLAIN statement instead."

// QueryDatabaseTool creates the query_database tool. The tool is
// read-only: write statements are refused before any connection is
// resolved, and reads run in a read-only transaction.
func QueryDatabaseTool(dbClient *database.Client, resolver *ConnectionResolver) Tool {
	return Tool{
		Definition: mcp.Tool{
			Name: "query_database",
			Description: `Execute SQL queries for STRUCTURED, EXACT data retrieval.

<database_context>
Specify connection_id to target a particular monitored database.
Use list_connections to discover available connection IDs and
their default databases. Optionally provide database_name to
override the default database for the connection. If
connection_id is omitted, the tool uses the currently selected
connection.
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
- SELECT queries run in READ-ONLY transactions for safety
- This tool is read-only: write operations (INSERT, UPDATE, DELETE, DDL) are refused, whatever access you hold on the connection
- A SELECT that calls a side-effecting function (for example pg_terminate_backend, pg_cancel_backend, pg_reload_conf, set_config or nextval) counts as a write operation and is refused
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
			CompactDescription: `Execute SQL queries against a monitored database. Read-only: SELECT queries run in a read-only transaction and write operations are refused. Specify connection_id to target a database; use list_connections to discover IDs. Returns results in TSV format.`,
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]any{
					"connection_id": map[string]any{
						"type":        "integer",
						"description": "ID of the monitored database connection to use. Use list_connections to discover available IDs. If omitted, uses the currently selected connection.",
					},
					"database_name": map[string]any{
						"type":        "string",
						"description": "Database name to connect to. If omitted, uses the connection's default database.",
					},
					"query": map[string]any{
						"type":        "string",
						"description": "SQL query to execute against the database. SELECT queries run in read-only transactions; write operations are refused.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of rows to return (default: 100, max: 1000). Automatically appended to query if not already present. Use higher limits only when necessary to avoid excessive token usage.",
						"default":     100,
						"minimum":     1,
						"maximum":     1000,
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "Number of rows to skip before returning results (for pagination). Use with limit to page through large result sets. Example: offset=100 with limit=100 returns rows 101-200.",
						"default":     0,
						"minimum":     0,
					},
				},
				Required: []string{"query"},
			},
		},
		Handler: func(args map[string]any) (mcp.ToolResponse, error) {
			query, ok := args["query"].(string)
			if !ok {
				return mcp.NewToolError("Missing or invalid 'query' parameter")
			}

			// Extract context from args (injected by registry.Execute)
			ctx, ok := args["__context"].(context.Context)
			if !ok {
				ctx = context.Background()
			}

			sqlQuery := strings.TrimSpace(query)
			if sqlQuery == "" {
				return mcp.NewToolError("Missing or invalid 'query' parameter")
			}

			// query_database is read-only by design (issue #663): refuse
			// anything isSelectQuery does not classify as a read before
			// resolving or opening a connection, whatever access the
			// caller holds on the target.
			if !isSelectQuery(sqlQuery) {
				return mcp.NewToolError(queryDatabaseReadOnlyError)
			}

			// The target is the explicit connection_id, resolved with its
			// RBAC check, or else the caller's selected connection. The
			// query text never chooses the target (issue #662).
			resolved, errResp := resolver.Resolve(ctx, args, dbClient)
			if errResp != nil {
				return *errResp, nil
			}
			connStr := resolved.ConnStr

			// Check if metadata is loaded for the target connection
			if !resolved.Client.IsMetadataLoadedFor(connStr) {
				return mcp.NewToolError(mcp.DatabaseNotReadyError)
			}

			limit, offset := parseLimitOffset(args)
			sqlQuery, _, _ = injectLimitOffset(sqlQuery, limit, offset)

			qr, errResp := executeReadOnlyQuery(ctx, resolved.Pool, sqlQuery, limit, "")
			if errResp != nil {
				return *errResp, nil
			}

			var sb strings.Builder
			fmt.Fprintf(&sb, "Database: %s\n\n", database.SanitizeConnStr(connStr))

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
