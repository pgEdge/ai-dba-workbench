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
	IsMonitored  bool   `json:"is_monitored"`
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

			ctx := context.Background()

			// Query for all connections (excluding sensitive fields like passwords)
			query := `
                SELECT
                    id,
                    name,
                    host,
                    port,
                    database_name,
                    is_monitored
                FROM public.connections
                ORDER BY name, host
            `

			rows, err := pool.Query(ctx, query)
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to query connections: %v", err))
			}
			defer rows.Close()

			var connections []MonitoredConnectionInfo
			for rows.Next() {
				var conn MonitoredConnectionInfo
				if err := rows.Scan(&conn.ID, &conn.Name, &conn.Host, &conn.Port, &conn.DatabaseName, &conn.IsMonitored); err != nil {
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
			sb.WriteString("id\tname\thost\tport\tdatabase_name\tis_monitored\n")
			for _, conn := range connections {
				sb.WriteString(fmt.Sprintf("%d\t%s\t%s\t%d\t%s\t%t\n",
					conn.ID, conn.Name, conn.Host, conn.Port, conn.DatabaseName, conn.IsMonitored))
			}

			sb.WriteString("\nNote: Use the 'id' column value as the connection_id parameter in query_metrics.\n")
			sb.WriteString("Only monitored connections (is_monitored=true) will have metrics data available.\n")

			return mcp.NewToolSuccess(sb.String())
		},
	}
}
