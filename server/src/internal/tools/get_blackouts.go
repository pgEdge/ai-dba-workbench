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
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
	"github.com/pgedge/ai-workbench/server/internal/tsv"
)

// GetBlackoutsTool creates the get_blackouts tool for querying blackout periods
func GetBlackoutsTool(pool *pgxpool.Pool, rbacChecker *auth.RBACChecker) Tool {
	return Tool{
		Definition: mcp.Tool{
			Name: "get_blackouts",
			Description: `Query blackout (maintenance window) periods for monitored connections.

<database_context>
This tool queries the DATASTORE to retrieve blackout periods that affect
monitored PostgreSQL servers. Blackouts suppress alerts during planned
maintenance. The tool checks all scope levels (estate, group, cluster,
server) to find relevant blackouts.
</database_context>

<important_behavior>
ALWAYS check pg://connection_info first to find the current connection.

If a connection IS selected (connected: true):
- Specify connection_id to filter blackouts affecting that connection
- "My database" or "the database" means the currently selected connection

If NO connection is selected (connected: false):
- Omit connection_id to see blackouts across ALL accessible connections
- The user can also specify a connection_id to filter to one connection

When connection_id is omitted, returns all blackouts across the estate.
Each row includes the scope and relevant connection or group information
so you can identify what each blackout covers.
</important_behavior>

<parameters>
- connection_id: (optional) ID of a monitored connection. Omit to return blackouts across all accessible connections and scopes.
- active_only: If true, only return currently active blackouts. Default: false.
- include_schedules: If true, also return recurring blackout schedules. Default: false.
- limit: Maximum results to return (1-50). Default: 20.
</parameters>

<output>
Returns TSV data with blackout periods.

When connection_id is specified, returns blackouts affecting that connection
at any scope level (estate, group, cluster, server).

When connection_id is omitted, returns all blackouts with:
- connection_id: The specific connection ID (for server-scoped blackouts, empty otherwise)
- connection_name: The connection name (for server-scoped blackouts, empty otherwise)

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
- get_blackouts() - all blackouts across the estate
- get_blackouts(active_only=true) - only currently active blackouts across all connections
- get_blackouts(include_schedules=true) - blackouts and recurring schedules
- get_blackouts(connection_id=5, active_only=true) - active blackouts for connection 5
</examples>`,
			CompactDescription: `Query blackout (maintenance window) periods. Omit connection_id to see blackouts across all accessible connections and scopes. Filter by active_only=true for current blackouts. Set include_schedules=true to see recurring schedules.`,
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"connection_id": map[string]interface{}{
						"type":        "integer",
						"description": "ID of a monitored connection. Omit to return blackouts across all accessible connections and scopes.",
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

			// Determine single-connection vs multi-connection mode
			singleConnection := false
			var connectionID int
			var connName string
			if _, hasConnID := args["connection_id"]; hasConnID {
				var err error
				connectionID, err = parseIntArg(args, "connection_id")
				if err != nil {
					return mcp.NewToolError("Invalid 'connection_id' parameter: must be an integer. Use list_connections to find available connection IDs.")
				}
				singleConnection = true

				// Verify the connection_id exists
				err = pool.QueryRow(ctx, "SELECT name FROM connections WHERE id = $1", connectionID).Scan(&connName)
				if err != nil {
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

				// RBAC: verify access to the specified connection
				if rbacChecker != nil {
					canAccess, _ := rbacChecker.CanAccessConnection(ctx, connectionID)
					if !canAccess {
						return mcp.NewToolError(fmt.Sprintf("Access denied: you do not have permission to access connection ID %d.", connectionID))
					}
				}
			}

			// Build accessible connection filter for multi-connection mode
			var accessibleIDs []int
			if !singleConnection && rbacChecker != nil {
				accessibleIDs = rbacChecker.GetAccessibleConnections(ctx)
				if accessibleIDs != nil && len(accessibleIDs) == 0 {
					return mcp.NewToolSuccess("No blackouts found. You do not have access to any connections.")
				}
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

			if singleConnection {
				return blackoutsSingleConnection(ctx, pool, connectionID, connName, activeOnly, includeSchedules, limit)
			}
			return blackoutsAllConnections(ctx, pool, accessibleIDs, activeOnly, includeSchedules, limit)
		},
	}
}

// blackoutsSingleConnection queries blackouts for a single connection (original behavior)
func blackoutsSingleConnection(
	ctx context.Context, pool *pgxpool.Pool,
	connectionID int, connName string,
	activeOnly, includeSchedules bool, limit int,
) (mcp.ToolResponse, error) {
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
		scheduleResult, err := blackoutSchedulesSingleConnection(ctx, pool, connectionID, limit)
		if err != nil {
			return mcp.ToolResponse{}, err
		}
		sb.WriteString(scheduleResult)
	}

	return mcp.NewToolSuccess(sb.String())
}

// blackoutSchedulesSingleConnection queries recurring schedules for a single connection
func blackoutSchedulesSingleConnection(
	ctx context.Context, pool *pgxpool.Pool,
	connectionID int, limit int,
) (string, error) {
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
		return "", fmt.Errorf("failed to query blackout schedules: %w", err)
	}
	defer schedRows.Close()

	var sb strings.Builder
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
			return "", fmt.Errorf("failed to scan schedule row: %w", err)
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
		return "", fmt.Errorf("error iterating schedule results: %w", err)
	}

	if schedCount == 0 {
		sb.WriteString("(no blackout schedules found)\n")
	} else {
		sb.WriteString(fmt.Sprintf("\n(%d rows)\n", schedCount))
	}

	return sb.String(), nil
}

// blackoutsAllConnections queries blackouts across all accessible connections
func blackoutsAllConnections(
	ctx context.Context, pool *pgxpool.Pool,
	accessibleIDs []int,
	activeOnly, includeSchedules bool, limit int,
) (mcp.ToolResponse, error) {
	// For multi-connection mode we query all blackouts and filter by accessible
	// connections for server-scoped blackouts. Estate-scoped blackouts are always
	// included. Group and cluster scoped blackouts are included when the user has
	// access to at least one connection in that group/cluster.
	connFilter, connArgs := buildConnectionFilter("b.connection_id", accessibleIDs)

	paramIdx := len(connArgs) + 1
	query := fmt.Sprintf(`
        SELECT b.id, b.scope, b.reason, b.start_time, b.end_time,
               b.created_by,
               (b.start_time <= NOW() AND b.end_time >= NOW()) AS is_active,
               b.connection_id, c.name AS connection_name
        FROM blackouts b
        LEFT JOIN connections c ON c.id = b.connection_id
        WHERE (
            (b.scope = 'estate')
            OR (b.scope = 'server' AND %s)
            OR (b.scope = 'cluster' AND b.cluster_id IN (
                SELECT DISTINCT cluster_id FROM connections WHERE %s
            ))
            OR (b.scope = 'group' AND b.group_id IN (
                SELECT DISTINCT cl.group_id FROM connections cn
                JOIN clusters cl ON cl.id = cn.cluster_id
                WHERE %s
            ))
        )
        AND ($%d::boolean = false OR (b.start_time <= NOW() AND b.end_time >= NOW()))
        ORDER BY b.start_time DESC
        LIMIT $%d
    `,
		connFilter,
		replaceColumnInFilter(connFilter, "b.connection_id", "connections.id"),
		replaceColumnInFilter(connFilter, "b.connection_id", "cn.id"),
		paramIdx, paramIdx+1,
	)

	// For the cluster/group sub-selects we reuse the same parameter values
	// since they reference the same $1..$N placeholders. Append activeOnly and limit.
	queryArgs := make([]interface{}, 0, len(connArgs)+2)
	queryArgs = append(queryArgs, connArgs...)
	queryArgs = append(queryArgs, activeOnly, limit)

	rows, err := pool.Query(ctx, query, queryArgs...)
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
	sb.WriteString(fmt.Sprintf("Blackouts | All accessible connections | Filter: %s | Limit: %d\n\n",
		activeLabel, limit))

	// Header - includes connection identification columns
	sb.WriteString("id\tscope\tconnection_id\tconnection_name\treason\tstart_time\tend_time\tis_active\n")

	// Data rows
	rowCount := 0
	for rows.Next() {
		var (
			id          int64
			scope       string
			reason      string
			startTime   time.Time
			endTime     time.Time
			createdBy   string
			isActive    bool
			connID      *int
			connNameVal *string
		)

		if err := rows.Scan(&id, &scope, &reason, &startTime, &endTime, &createdBy, &isActive, &connID, &connNameVal); err != nil {
			return mcp.NewToolError(fmt.Sprintf("Failed to scan row: %v", err))
		}

		connIDStr := ""
		connNameStr := ""
		if connID != nil {
			connIDStr = fmt.Sprintf("%d", *connID)
		}
		if connNameVal != nil {
			connNameStr = tsv.FormatValue(*connNameVal)
		}

		sb.WriteString(fmt.Sprintf("%d\t%s\t%s\t%s\t%s\t%s\t%s\t%t\n",
			id,
			scope,
			connIDStr,
			connNameStr,
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
		scheduleResult, err := blackoutSchedulesAllConnections(ctx, pool, accessibleIDs, limit)
		if err != nil {
			return mcp.NewToolError(fmt.Sprintf("Failed to query blackout schedules: %v", err))
		}
		sb.WriteString(scheduleResult)
	}

	return mcp.NewToolSuccess(sb.String())
}

// blackoutSchedulesAllConnections queries recurring schedules across all accessible connections
func blackoutSchedulesAllConnections(
	ctx context.Context, pool *pgxpool.Pool,
	accessibleIDs []int, limit int,
) (string, error) {
	connFilter, connArgs := buildConnectionFilter("s.connection_id", accessibleIDs)

	paramIdx := len(connArgs) + 1
	query := fmt.Sprintf(`
        SELECT s.id, s.scope, s.name, s.cron_expression,
               s.duration_minutes, s.timezone, s.reason, s.enabled,
               s.connection_id, c.name AS connection_name
        FROM blackout_schedules s
        LEFT JOIN connections c ON c.id = s.connection_id
        WHERE (
            (s.scope = 'estate')
            OR (s.scope = 'server' AND %s)
            OR (s.scope = 'cluster' AND s.cluster_id IN (
                SELECT DISTINCT cluster_id FROM connections WHERE %s
            ))
            OR (s.scope = 'group' AND s.group_id IN (
                SELECT DISTINCT cl.group_id FROM connections cn
                JOIN clusters cl ON cl.id = cn.cluster_id
                WHERE %s
            ))
        )
        ORDER BY s.created_at DESC
        LIMIT $%d
    `,
		connFilter,
		replaceColumnInFilter(connFilter, "s.connection_id", "connections.id"),
		replaceColumnInFilter(connFilter, "s.connection_id", "cn.id"),
		paramIdx,
	)

	queryArgs := make([]interface{}, 0, len(connArgs)+1)
	queryArgs = append(queryArgs, connArgs...)
	queryArgs = append(queryArgs, limit)

	schedRows, err := pool.Query(ctx, query, queryArgs...)
	if err != nil {
		return "", fmt.Errorf("failed to query blackout schedules: %w", err)
	}
	defer schedRows.Close()

	var sb strings.Builder
	sb.WriteString("\n--- Blackout Schedules ---\n\n")
	sb.WriteString("id\tscope\tconnection_id\tconnection_name\tname\tcron_expression\tduration_minutes\ttimezone\treason\tenabled\n")

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
			connID          *int
			connNameVal     *string
		)

		if err := schedRows.Scan(&id, &scope, &name, &cronExpr, &durationMinutes, &timezone, &reason, &enabled, &connID, &connNameVal); err != nil {
			return "", fmt.Errorf("failed to scan schedule row: %w", err)
		}

		connIDStr := ""
		connNameStr := ""
		if connID != nil {
			connIDStr = fmt.Sprintf("%d", *connID)
		}
		if connNameVal != nil {
			connNameStr = tsv.FormatValue(*connNameVal)
		}

		sb.WriteString(fmt.Sprintf("%d\t%s\t%s\t%s\t%s\t%s\t%d\t%s\t%s\t%t\n",
			id,
			scope,
			connIDStr,
			connNameStr,
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
		return "", fmt.Errorf("error iterating schedule results: %w", err)
	}

	if schedCount == 0 {
		sb.WriteString("(no blackout schedules found)\n")
	} else {
		sb.WriteString(fmt.Sprintf("\n(%d rows)\n", schedCount))
	}

	return sb.String(), nil
}

// replaceColumnInFilter replaces the column name in a connection filter clause.
// This is used when the same filter logic needs to apply to a different table alias
// in a sub-select.
func replaceColumnInFilter(filter, oldColumn, newColumn string) string {
	return strings.Replace(filter, oldColumn, newColumn, 1)
}
