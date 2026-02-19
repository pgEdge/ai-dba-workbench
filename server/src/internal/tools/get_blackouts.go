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
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
	"github.com/pgedge/ai-workbench/server/internal/tsv"
)

// GetBlackoutsTool creates the get_blackouts tool for querying blackout periods
func GetBlackoutsTool(pool *pgxpool.Pool) Tool {
	return Tool{
		Definition: mcp.Tool{
			Name: "get_blackouts",
			Description: `Query blackout (maintenance window) periods for a monitored connection.

<database_context>
This tool queries the DATASTORE to retrieve blackout periods that affect a
monitored PostgreSQL server. Blackouts suppress alerts during planned
maintenance. The tool checks all scope levels (estate, group, cluster,
server) to find blackouts that affect the given connection.
</database_context>

<important_behavior>
ALWAYS check pg://connection_info first to find the current connection.

If a connection IS selected (connected: true):
- Omit connection_id to use the current connection automatically
- "My database" or "the database" means the currently selected connection

If NO connection is selected (connected: false):
- DO NOT arbitrarily pick connections to analyze
- ASK the user which connection they want: "You don't have a database selected. Which would you like me to analyze?"
- Only proceed after the user specifies which connection(s) to query

NEVER silently query multiple connections without explicit user consent.
</important_behavior>

<parameters>
- connection_id: ID of the monitored connection. OMIT to use the currently selected connection.
- active_only: If true, only return currently active blackouts. Default: false.
- include_schedules: If true, also return recurring blackout schedules. Default: false.
- limit: Maximum results to return (1-50). Default: 20.
</parameters>

<output>
Returns TSV data with blackout periods affecting the connection at any scope level.

Blackout columns:
- id: Blackout ID
- scope: Scope level (estate, group, cluster, server)
- reason: Reason for the blackout
- start_time: When the blackout starts
- end_time: When the blackout ends
- is_active: Whether the blackout is currently active (true/false)

If include_schedules is true, a second section shows recurring schedules:
- id: Schedule ID
- scope: Scope level (estate, group, cluster, server)
- name: Schedule name
- cron_expression: Cron expression for recurrence
- duration_minutes: Duration of each blackout window
- timezone: Timezone for the cron expression
- reason: Reason for the scheduled blackout
- enabled: Whether the schedule is active (true/false)
</output>

<examples>
- get_blackouts() - blackouts for the current connection (all, including past)
- get_blackouts(active_only=true) - only currently active blackouts
- get_blackouts(include_schedules=true) - blackouts and recurring schedules
- get_blackouts(connection_id=5, active_only=true) - active blackouts for connection 5
</examples>`,
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"connection_id": map[string]interface{}{
						"type":        "integer",
						"description": "ID of the monitored connection. If not specified, uses the currently selected connection.",
					},
					"active_only": map[string]interface{}{
						"type":        "boolean",
						"description": "If true, only return currently active blackouts. Default: false.",
						"default":     false,
					},
					"include_schedules": map[string]interface{}{
						"type":        "boolean",
						"description": "If true, also return recurring blackout schedules. Default: false.",
						"default":     false,
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results to return (1-50). Default: 20",
						"default":     20,
						"minimum":     1,
						"maximum":     50,
					},
				},
				Required: []string{},
			},
		},
		Handler: func(args map[string]interface{}) (mcp.ToolResponse, error) {
			if pool == nil {
				return mcp.NewToolError("Datastore not configured. The get_blackouts tool requires a datastore connection.")
			}

			// Extract context from args (injected by registry.Execute)
			ctx, ok := args["__context"].(context.Context)
			if !ok {
				ctx = context.Background()
			}

			// Parse connection_id (required after injection)
			connectionID, err := parseIntArg(args, "connection_id")
			if err != nil {
				return mcp.NewToolError("Missing or invalid 'connection_id' parameter. If you haven't selected a database connection, use list_connections to find available connection IDs, then specify connection_id explicitly.")
			}

			// Verify the connection_id exists in the connections table
			var connName string
			err = pool.QueryRow(ctx, "SELECT name FROM connections WHERE id = $1", connectionID).Scan(&connName)
			if err != nil {
				// Connection doesn't exist - provide helpful error with valid IDs
				rows, qerr := pool.Query(ctx, "SELECT id, name FROM connections ORDER BY id LIMIT 20")
				if qerr == nil {
					defer rows.Close()
					var validIDs []string
					for rows.Next() {
						var id int
						var name string
						if rows.Scan(&id, &name) == nil {
							validIDs = append(validIDs, fmt.Sprintf("%d (%s)", id, name))
						}
					}
					if len(validIDs) > 0 {
						return mcp.NewToolError(fmt.Sprintf(
							"Connection ID %d does not exist. Valid connection IDs are: %s. "+
								"Use list_connections to see all available connections.",
							connectionID, strings.Join(validIDs, ", ")))
					}
				}
				return mcp.NewToolError(fmt.Sprintf("Connection ID %d does not exist. Use list_connections to see available connections.", connectionID))
			}

			// Parse active_only (default: false)
			activeOnly := false
			if v, ok := args["active_only"].(bool); ok {
				activeOnly = v
			}

			// Parse include_schedules (default: false)
			includeSchedules := false
			if v, ok := args["include_schedules"].(bool); ok {
				includeSchedules = v
			}

			// Parse limit (default: 20, max: 50)
			limit := 20
			if limitVal, ok := args["limit"]; ok {
				l, err := parseIntArg(args, "limit")
				if err == nil && l > 0 && l <= 50 {
					limit = l
				} else if limitVal != nil {
					return mcp.NewToolError("Invalid 'limit' parameter: must be between 1 and 50")
				}
			}

			// Query blackouts affecting this connection at all scope levels.
			// A connection belongs to a cluster (connections.cluster_id),
			// and a cluster belongs to a group (clusters.group_id).
			query := `
                SELECT b.id, b.scope, b.reason, b.start_time, b.end_time,
                       b.created_by,
                       (b.start_time <= NOW() AND b.end_time >= NOW()) AS is_active
                FROM blackouts b
                WHERE (
                    (b.scope = 'estate')
                    OR (b.scope = 'server' AND b.connection_id = $1)
                    OR (b.scope = 'cluster' AND b.cluster_id = (
                        SELECT cluster_id FROM connections WHERE id = $1
                    ))
                    OR (b.scope = 'group' AND b.group_id = (
                        SELECT cl.group_id FROM connections c
                        JOIN clusters cl ON cl.id = c.cluster_id
                        WHERE c.id = $1
                    ))
                )
                AND ($2::boolean = false OR (b.start_time <= NOW() AND b.end_time >= NOW()))
                ORDER BY b.start_time DESC
                LIMIT $3
            `

			rows, err := pool.Query(ctx, query, connectionID, activeOnly, limit)
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to query blackouts: %v", err))
			}
			defer rows.Close()

			// Build TSV output
			var sb strings.Builder
			activeLabel := "all"
			if activeOnly {
				activeLabel = "active only"
			}
			sb.WriteString(fmt.Sprintf("Blackouts | Connection: %d (%s) | Filter: %s | Limit: %d\n\n",
				connectionID, connName, activeLabel, limit))

			// Header
			sb.WriteString("id\tscope\treason\tstart_time\tend_time\tis_active\n")

			// Data rows
			rowCount := 0
			for rows.Next() {
				var (
					id        int64
					scope     string
					reason    string
					startTime time.Time
					endTime   time.Time
					createdBy string
					isActive  bool
				)

				if err := rows.Scan(&id, &scope, &reason, &startTime, &endTime, &createdBy, &isActive); err != nil {
					return mcp.NewToolError(fmt.Sprintf("Failed to scan row: %v", err))
				}

				sb.WriteString(fmt.Sprintf("%d\t%s\t%s\t%s\t%s\t%t\n",
					id,
					scope,
					tsv.FormatValue(reason),
					startTime.Format(time.RFC3339),
					endTime.Format(time.RFC3339),
					isActive,
				))
				rowCount++
			}

			if err := rows.Err(); err != nil {
				return mcp.NewToolError(fmt.Sprintf("Error iterating results: %v", err))
			}

			if rowCount == 0 {
				if activeOnly {
					sb.WriteString("(no active blackouts)\n")
				} else {
					sb.WriteString("(no blackouts found)\n")
				}
			} else {
				sb.WriteString(fmt.Sprintf("\n(%d rows)\n", rowCount))
			}

			// Optionally include recurring blackout schedules
			if includeSchedules {
				scheduleQuery := `
                    SELECT s.id, s.scope, s.name, s.cron_expression,
                           s.duration_minutes, s.timezone, s.reason, s.enabled
                    FROM blackout_schedules s
                    WHERE (
                        (s.scope = 'estate')
                        OR (s.scope = 'server' AND s.connection_id = $1)
                        OR (s.scope = 'cluster' AND s.cluster_id = (
                            SELECT cluster_id FROM connections WHERE id = $1
                        ))
                        OR (s.scope = 'group' AND s.group_id = (
                            SELECT cl.group_id FROM connections c
                            JOIN clusters cl ON cl.id = c.cluster_id
                            WHERE c.id = $1
                        ))
                    )
                    ORDER BY s.created_at DESC
                    LIMIT $2
                `

				schedRows, err := pool.Query(ctx, scheduleQuery, connectionID, limit)
				if err != nil {
					return mcp.NewToolError(fmt.Sprintf("Failed to query blackout schedules: %v", err))
				}
				defer schedRows.Close()

				sb.WriteString("\n--- Blackout Schedules ---\n\n")
				sb.WriteString("id\tscope\tname\tcron_expression\tduration_minutes\ttimezone\treason\tenabled\n")

				schedCount := 0
				for schedRows.Next() {
					var (
						id              int64
						scope           string
						name            string
						cronExpr        string
						durationMinutes int
						timezone        string
						reason          string
						enabled         bool
					)

					if err := schedRows.Scan(&id, &scope, &name, &cronExpr, &durationMinutes, &timezone, &reason, &enabled); err != nil {
						return mcp.NewToolError(fmt.Sprintf("Failed to scan schedule row: %v", err))
					}

					sb.WriteString(fmt.Sprintf("%d\t%s\t%s\t%s\t%d\t%s\t%s\t%t\n",
						id,
						scope,
						tsv.FormatValue(name),
						cronExpr,
						durationMinutes,
						timezone,
						tsv.FormatValue(reason),
						enabled,
					))
					schedCount++
				}

				if err := schedRows.Err(); err != nil {
					return mcp.NewToolError(fmt.Sprintf("Error iterating schedule results: %v", err))
				}

				if schedCount == 0 {
					sb.WriteString("(no blackout schedules found)\n")
				} else {
					sb.WriteString(fmt.Sprintf("\n(%d rows)\n", schedCount))
				}
			}

			return mcp.NewToolSuccess(sb.String())
		},
	}
}
