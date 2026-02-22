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
			CompactDescription: `Execute read-only SQL queries against the DATASTORE database (stores configuration, metrics, alerts, connections). Use for querying monitoring data, alert history, or system configuration. For monitored database queries, use query_database instead.`,
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
			limit, offset := parseLimitOffset(args)
			sqlQuery, hadExistingLimit, _ := injectLimitOffset(sqlQuery, limit, offset)

			// Extract context from args (injected by registry.Execute)
			ctx, ok := args["__context"].(context.Context)
			if !ok {
				ctx = context.Background()
			}

			qr, errResp := executeReadOnlyQuery(ctx, pool, sqlQuery, limit, hadExistingLimit, "")
			if errResp != nil {
				return *errResp, nil
			}

			var sb strings.Builder
			sb.WriteString("Datastore Query\n\n")

			formatPaginatedResults(&sb, qr, sqlQuery, limit, offset, " or increase limit")

			// Log execution metrics
			logging.Info("query_datastore_executed",
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
