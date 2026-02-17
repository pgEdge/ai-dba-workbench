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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/logging"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// QueryDatastoreTool creates the query_datastore tool for executing
// read-only SQL queries against the monitoring datastore database.
func QueryDatastoreTool(pool *pgxpool.Pool) Tool {
	return Tool{
		Definition: mcp.Tool{
			Name: "query_datastore",
			Description: `Execute read-only SQL queries against the DATASTORE (monitoring/metrics database).

<database_context>
This tool queries the DATASTORE, which is the internal PostgreSQL database that
stores all monitoring data collected by the AI DBA Workbench. This is NOT a
monitored database; it is the workbench's own storage for metrics, connections,
alerts, and configuration.

The datastore contains the following schemas:
- public: Core configuration tables including connections and cluster hierarchy.
- metrics: Collected monitoring data from all monitored PostgreSQL databases.

Key tables include:
- public.connections: Registered database connections with host, port, and status.
- metrics.pg_stat_activity: Collected session and query activity snapshots.
- metrics.pg_stat_database: Database-level statistics over time.
- metrics.pg_stat_user_tables: Table-level statistics over time.
- metrics.pg_stat_user_indexes: Index usage statistics over time.
- metrics.pg_locks: Lock monitoring data over time.
- metrics.pg_node_role: Node role and replication status history.
</database_context>

<usecase>
Use query_datastore when you need:
- Direct SQL access to monitoring data with custom queries
- Complex joins across metrics tables not supported by query_metrics
- Historical analysis of collected monitoring data
- Querying connection configuration and status from public.connections
- Ad-hoc exploration of the datastore schema
- Aggregations across multiple metric tables or time ranges
</usecase>

<when_not_to_use>
DO NOT use for:
- Querying live monitored databases (use query_database instead)
- Simple metric queries with time ranges (use query_metrics for convenience)
- Listing connections (use list_connections for a formatted view)
- Retrieving alert history (use get_alert_history for structured results)
</when_not_to_use>

<examples>
Use query_datastore:
- "Show the raw metrics schema tables" -> SELECT table_name FROM information_schema.tables WHERE table_schema = 'metrics'
- "What connections are configured?" -> SELECT id, name, host, port FROM public.connections
- "Join activity and lock data for a time range" -> custom JOIN across metrics tables
- "Count metrics rows collected per day" -> aggregation over metrics tables

Use other tools instead:
- "Query the production database" -> use query_database
- "Show CPU metrics for the last hour" -> use query_metrics
- "List all monitored connections" -> use list_connections
</examples>

<important>
- All queries run in READ-ONLY transactions (no data modifications possible)
- Results are limited to prevent excessive token usage
- Results are returned in TSV (tab-separated values) format for efficiency
- This queries the workbench datastore, not any monitored database
</important>

<rate_limit_awareness>
To avoid rate limits (30,000 input tokens/minute):
- ALWAYS use the 'limit' parameter - it defaults to 100 rows
- Start with limit=10 for exploration queries, increase only if needed
- Filter results in WHERE clauses rather than fetching everything
- If rate limited, wait 60 seconds before retrying
</rate_limit_awareness>`,
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "SQL query to execute against the datastore. All queries run in read-only transactions.",
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
			if pool == nil {
				return mcp.NewToolError("Datastore not configured. The query_datastore tool requires a datastore connection.")
			}

			query, ok := args["query"].(string)
			if !ok || strings.TrimSpace(query) == "" {
				return mcp.NewToolError("Missing or invalid 'query' parameter")
			}

			sqlQuery := strings.TrimSpace(query)

			// Determine the limit to use
			limit := 100 // default
			if limitVal, ok := args["limit"]; ok {
				switch v := limitVal.(type) {
				case float64:
					limit = int(v)
				case int:
					limit = v
				}
			}
			if limit < 1 {
				limit = 1
			}
			if limit > 1000 {
				limit = 1000
			}

			// Determine the offset to use
			offset := 0 // default
			if offsetVal, ok := args["offset"]; ok {
				switch v := offsetVal.(type) {
				case float64:
					offset = int(v)
				case int:
					offset = v
				}
			}
			if offset < 0 {
				offset = 0
			}

			// Track if query already had LIMIT/OFFSET clauses
			upperQuery := strings.ToUpper(sqlQuery)
			hasExistingLimit := strings.Contains(upperQuery, "LIMIT")
			hasExistingOffset := strings.Contains(upperQuery, "OFFSET")

			// Only inject LIMIT/OFFSET if query doesn't already have them
			// Fetch limit+1 to detect if more rows exist
			if limit > 0 && !hasExistingLimit {
				sqlQuery = fmt.Sprintf("%s LIMIT %d", sqlQuery, limit+1)
			}
			if offset > 0 && !hasExistingOffset {
				sqlQuery = fmt.Sprintf("%s OFFSET %d", sqlQuery, offset)
			}

			// Extract context from args (injected by registry.Execute)
			ctx, ok := args["__context"].(context.Context)
			if !ok {
				ctx = context.Background()
			}

			// Begin a transaction with read-only protection
			tx, err := pool.Begin(ctx)
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to begin transaction: %v", err))
			}

			// Track whether transaction was committed
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

			// Set transaction to read-only to prevent any data modifications
			_, err = tx.Exec(ctx, "SET TRANSACTION READ ONLY")
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to set transaction read-only: %v", err))
			}

			// Defense-in-depth: limit query execution time
			_, err = tx.Exec(ctx, "SET LOCAL statement_timeout = '10s'")
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to set statement timeout: %v", err))
			}

			rows, err := tx.Query(ctx, sqlQuery)
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("SQL Query:\n%s\n\nError executing query: %v", sqlQuery, err))
			}
			defer rows.Close()

			// Get column names
			fieldDescriptions := rows.FieldDescriptions()
			var columnNames []string
			for _, fd := range fieldDescriptions {
				columnNames = append(columnNames, string(fd.Name))
			}

			// Collect results as array of arrays for TSV formatting
			var results [][]interface{}
			for rows.Next() {
				values, err := rows.Values()
				if err != nil {
					return mcp.NewToolError(fmt.Sprintf("Error reading row: %v", err))
				}
				results = append(results, values)
			}

			if err := rows.Err(); err != nil {
				return mcp.NewToolError(fmt.Sprintf("Error iterating rows: %v", err))
			}

			// Check if results were truncated (we fetched limit+1 to detect this)
			wasTruncated := false
			if !hasExistingLimit && limit > 0 && len(results) > limit {
				wasTruncated = true
				results = results[:limit] // Truncate to requested limit
			}

			// Format results as TSV (tab-separated values)
			resultsTSV := FormatResultsAsTSV(columnNames, results)

			// Commit the read-only transaction
			if err := tx.Commit(ctx); err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to commit transaction: %v", err))
			}
			committed = true

			var sb strings.Builder

			sb.WriteString("Datastore Query\n\n")
			sb.WriteString(fmt.Sprintf("SQL Query:\n%s\n\n", sqlQuery))

			// Build the results header with pagination info
			if offset > 0 {
				startRow := offset + 1
				endRow := offset + len(results)
				if wasTruncated {
					sb.WriteString(fmt.Sprintf("Results (rows %d-%d, more available - use offset=%d for next page):\n%s",
						startRow, endRow, offset+limit, resultsTSV))
				} else {
					sb.WriteString(fmt.Sprintf("Results (rows %d-%d):\n%s", startRow, endRow, resultsTSV))
				}
			} else if wasTruncated {
				sb.WriteString(fmt.Sprintf("Results (%d rows shown, more available - use offset=%d for next page or increase limit):\n%s",
					len(results), limit, resultsTSV))
			} else {
				sb.WriteString(fmt.Sprintf("Results (%d rows):\n%s", len(results), resultsTSV))
			}

			// Log execution metrics
			logging.Info("query_datastore_executed",
				"query_length", len(sqlQuery),
				"rows_returned", len(results),
				"offset", offset,
				"was_truncated", wasTruncated,
				"estimated_tokens", len(resultsTSV)/4,
			)

			return mcp.NewToolSuccess(sb.String())
		},
	}
}
