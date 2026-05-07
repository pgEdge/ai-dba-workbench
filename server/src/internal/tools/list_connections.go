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
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// MonitoredConnectionInfo represents a connection from the datastore
type MonitoredConnectionInfo struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	DatabaseName    string `json:"database_name"`
	IsMonitored     bool   `json:"is_monitored"`
	Status          string `json:"status"`
	ConnectionError string `json:"connection_error,omitempty"`
}

// ListConnectionsTool creates the list_connections tool for listing monitored connections.
// When rbacChecker and visibilityLister are non-nil, the returned list is
// filtered to connections the caller can see. Pass nil for both in tests
// or when RBAC is not configured.
func ListConnectionsTool(pool *pgxpool.Pool, rbacChecker *auth.RBACChecker, visibilityLister auth.ConnectionVisibilityLister) Tool {
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
			CompactDescription: `List all monitored database connections in the datastore. Returns connection IDs, names, hostnames, ports, and status. Use to discover available connections.`,
			InputSchema: mcp.InputSchema{
				Type:       "object",
				Properties: map[string]any{},
				Required:   []string{},
			},
		},
		Handler: func(args map[string]any) (mcp.ToolResponse, error) {
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
                WITH latest_connectivity AS (
                    SELECT DISTINCT ON (connection_id)
                        connection_id, collected_at
                    FROM metrics.pg_connectivity
                    WHERE collected_at > NOW() - INTERVAL '5 minutes'
                    ORDER BY connection_id, collected_at DESC
                )
                SELECT
                    c.id,
                    c.name,
                    c.host,
                    c.port,
                    c.database_name,
                    c.is_monitored,
                    CASE
                        WHEN c.is_monitored AND c.connection_error IS NOT NULL
                        THEN 'offline'
                        WHEN c.is_monitored AND lc.connection_id IS NULL
                        THEN 'initialising'
                        WHEN lc.collected_at > NOW() - INTERVAL '60 seconds' THEN 'online'
                        WHEN lc.collected_at > NOW() - INTERVAL '150 seconds' THEN 'warning'
                        WHEN lc.collected_at IS NOT NULL THEN 'offline'
                        ELSE 'unknown'
                    END as status,
                    COALESCE(c.connection_error, '') as connection_error
                FROM public.connections c
                LEFT JOIN latest_connectivity lc ON c.id = lc.connection_id
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

			// Track total connections before RBAC filtering to distinguish
			// between "no connections exist" and "user has no access".
			totalConnectionsBeforeFilter := len(connections)

			// RBAC: filter connections to the caller's visible set.
			if rbacChecker != nil {
				visible, allConns, visErr := rbacChecker.VisibleConnectionIDs(ctx, visibilityLister)
				if visErr != nil {
					fmt.Fprintf(os.Stderr, "ERROR: list_connections: failed to resolve visible connections: %v\n", visErr)
				} else if !allConns {
					visibleSet := make(map[int]bool, len(visible))
					for _, id := range visible {
						visibleSet[id] = true
					}
					filtered := make([]MonitoredConnectionInfo, 0, len(connections))
					for _, conn := range connections {
						if visibleSet[conn.ID] {
							filtered = append(filtered, conn)
						}
					}
					connections = filtered
				}
			}

			if len(connections) == 0 {
				// Distinguish between "no connections exist" and "user has no access"
				if totalConnectionsBeforeFilter > 0 {
					return mcp.NewToolSuccess("You do not have access to any connections.")
				}
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
			fmt.Fprintf(&sb, "Found %d connections (%d monitored):\n\n", len(connections), monitoredCount)
			sb.WriteString("id\tname\thost\tport\tdatabase_name\tis_monitored\tstatus\terror\n")
			for _, conn := range connections {
				fmt.Fprintf(&sb, "%d\t%s\t%s\t%d\t%s\t%t\t%s\t%s\n",
					conn.ID,
					sanitizeTSVField(conn.Name),
					sanitizeTSVField(conn.Host),
					conn.Port,
					sanitizeTSVField(conn.DatabaseName),
					conn.IsMonitored,
					sanitizeTSVField(conn.Status),
					sanitizeTSVField(conn.ConnectionError))
			}

			sb.WriteString("\nNote: Use the 'id' column value as the connection_id parameter in query_metrics and all monitored-database tools.\n")
			sb.WriteString("Monitored-database tools (query_database, get_schema_info, execute_explain, similarity_search, count_rows, test_query) accept connection_id and an optional database_name parameter.\n")
			sb.WriteString("The database_name column shows the default database for each connection; specify database_name to override it.\n")
			sb.WriteString("Only monitored connections (is_monitored=true) will have metrics data available.\n")

			return mcp.NewToolSuccess(sb.String())
		},
	}
}
