/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// MonitoredConnectionInfo represents a connection from the datastore
type MonitoredConnectionInfo struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	DatabaseName string `json:"database_name"`
	IsMonitored     bool   `json:"is_monitored"`
	Status          string `json:"status"`
	ConnectionError string `json:"connection_error,omitempty"`
}

// ListConnectionsTool creates the list_connections tool for listing monitored connections
func ListConnectionsTool(pool *pgxpool.Pool) Tool {
	return Tool{
		Definition: mcp.Tool{
			Name: "list_connections",
			Description: `List all database connections stored in the datastore.

<database_context>
This tool queries the DATASTORE (not monitored databases) to list all database
connections that have been configured. These connections can be monitored by
the collector for metrics collection.
</database_context>

<when_to_use>
Use this tool ONLY when you need to:
- See ALL available connections for fleet-wide analysis (when user explicitly asks)
- Find a specific connection by name when the user asks about a different server
- Help the user select a connection when none is currently selected

Do NOT use this tool if:
- User asks about "my database" or "the current database" - read pg://connection_info instead
- User already has a connection selected - just use query_metrics without connection_id

To find the CURRENT connection: read the pg://connection_info resource (not this tool).
</when_to_use>

<provided_info>
Returns a TSV table with:
- id: Connection ID (use this with query_metrics connection_id parameter)
- name: User-friendly name for the connection
- host: PostgreSQL server hostname
- port: PostgreSQL server port
- database_name: Default database name
- is_monitored: Whether the collector is gathering metrics from this server
- status: Connection status (online, warning, offline, or unknown)
- error: Connection error message if the connection is unavailable
</provided_info>

<examples>
- User asks "show me all monitored servers" → use list_connections
- User asks "analyze the pg16 cluster" → use list_connections to find pg16 connection IDs
- User asks "analyze my database" with connection selected → DON'T use this, just query_metrics
</examples>

<workflow>
For single-database analysis (most common):
- Check pg://connection_info first
- If connected: use query_metrics without connection_id
- If NOT connected: ASK the user which connection to analyze, don't pick arbitrarily

For fleet-wide analysis (only when user explicitly requests):
1. Call list_connections to get available connection IDs
2. ASK user which connections to analyze - NEVER assume "all"
3. Use specific connection_ids with query_metrics

CRITICAL: Never silently analyze multiple connections. Always get explicit user consent.
</workflow>`,
			InputSchema: mcp.InputSchema{
				Type:       "object",
				Properties: map[string]interface{}{},
				Required:   []string{},
			},
		},
		Handler: func(args map[string]interface{}) (mcp.ToolResponse, error) {
			if pool == nil {
				return mcp.NewToolError("Datastore not configured. The list_connections tool requires a datastore connection.")
			}

			// Extract context from args (injected by registry.Execute)
			ctx, ok := args["__context"].(context.Context)
			if !ok {
				ctx = context.Background()
			}

			// Query for all connections (excluding sensitive fields like passwords)
			query := `
                WITH latest_roles AS (
                    SELECT DISTINCT ON (connection_id)
                        connection_id,
                        COALESCE(
                            CASE
                                WHEN collected_at > NOW() - INTERVAL '6 minutes' THEN 'online'
                                WHEN collected_at > NOW() - INTERVAL '12 minutes' THEN 'warning'
                                ELSE 'offline'
                            END, 'unknown'
                        ) as status
                    FROM metrics.pg_node_role
                    WHERE collected_at > NOW() - INTERVAL '15 minutes'
                    ORDER BY connection_id, collected_at DESC
                ),
                latest_conn_error AS (
                    SELECT connection_id,
                           bool_and(is_available) = false AS all_unavailable,
                           (array_agg(unavailable_reason ORDER BY last_checked DESC)
                            FILTER (WHERE unavailable_reason IS NOT NULL))[1]
                                AS error_reason
                    FROM probe_availability
                    WHERE last_checked > NOW() - INTERVAL '15 minutes'
                    GROUP BY connection_id
                )
                SELECT
                    c.id,
                    c.name,
                    c.host,
                    c.port,
                    c.database_name,
                    c.is_monitored,
                    CASE
                        WHEN c.is_monitored AND lr.connection_id IS NULL AND lce.connection_id IS NULL
                        THEN 'initialising'
                        WHEN COALESCE(lce.all_unavailable, false) AND COALESCE(lr.status, 'unknown') = 'unknown'
                        THEN 'offline'
                        ELSE COALESCE(lr.status, 'unknown')
                    END as status,
                    COALESCE(lce.error_reason, '') as connection_error
                FROM public.connections c
                LEFT JOIN latest_roles lr ON c.id = lr.connection_id
                LEFT JOIN latest_conn_error lce ON c.id = lce.connection_id
                ORDER BY c.name, c.host
            `

			rows, err := pool.Query(ctx, query)
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to query connections: %v", err))
			}
			defer rows.Close()

			var connections []MonitoredConnectionInfo
			for rows.Next() {
				var conn MonitoredConnectionInfo
				if err := rows.Scan(&conn.ID, &conn.Name, &conn.Host, &conn.Port, &conn.DatabaseName, &conn.IsMonitored, &conn.Status, &conn.ConnectionError); err != nil {
					return mcp.NewToolError(fmt.Sprintf("Failed to scan connection: %v", err))
				}
				connections = append(connections, conn)
			}

			if err := rows.Err(); err != nil {
				return mcp.NewToolError(fmt.Sprintf("Error iterating connections: %v", err))
			}

			if len(connections) == 0 {
				return mcp.NewToolSuccess("No database connections found in the datastore. Connections must be added before they can be monitored.")
			}

			// Count monitored vs total
			monitoredCount := 0
			for _, c := range connections {
				if c.IsMonitored {
					monitoredCount++
				}
			}

			// Format as TSV
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("Found %d connections (%d monitored):\n\n", len(connections), monitoredCount))
			sb.WriteString("id\tname\thost\tport\tdatabase_name\tis_monitored\tstatus\terror\n")
			for _, conn := range connections {
				sb.WriteString(fmt.Sprintf("%d\t%s\t%s\t%d\t%s\t%t\t%s\t%s\n",
					conn.ID, conn.Name, conn.Host, conn.Port, conn.DatabaseName, conn.IsMonitored, conn.Status, conn.ConnectionError))
			}

			sb.WriteString("\nNote: Use the 'id' column value as the connection_id parameter in query_metrics.\n")
			sb.WriteString("Only monitored connections (is_monitored=true) will have metrics data available.\n")

			return mcp.NewToolSuccess(sb.String())
		},
	}
}
