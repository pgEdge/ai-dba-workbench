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

// GetAlertHistoryTool creates the get_alert_history tool for querying historic alerts.
//
// The visibilityLister argument is used to resolve the set of connections
// the caller may see. It may be nil in unit tests or when no datastore is
// configured; auth.RBACChecker.VisibleConnectionIDs tolerates a nil lister
// by falling back to group/token-granted IDs only.
func GetAlertHistoryTool(pool *pgxpool.Pool, rbacChecker *auth.RBACChecker, visibilityLister auth.ConnectionVisibilityLister) Tool {
	return Tool{
		Definition: mcp.Tool{
			Name: "get_alert_history",
			Description: `Query alerts for monitored connections.

<database_context>
This tool queries the DATASTORE to retrieve alerts that have been triggered
for monitored PostgreSQL servers. Use this to check current alert status or
analyze historical alert patterns.
</database_context>

<important_behavior>
ALWAYS check pg://connection_info first to find the current connection.

If a connection IS selected (connected: true):
- Specify connection_id to filter alerts for that connection
- "My database" or "the database" means the currently selected connection

If NO connection is selected (connected: false):
- Omit connection_id to see alerts across ALL accessible connections
- The user can also specify a connection_id to filter to one connection

When connection_id is omitted, returns alerts across all connections the
user has access to. Each row includes connection_id and connection_name
so you can identify which connection each alert belongs to.
</important_behavior>

<critical_status_behavior>
When the user asks about "active alerts", "current alerts", or "alerting servers":
- ALWAYS use status="active" - this returns ALL active alerts regardless of age
- Do NOT apply a time filter for active alerts

When the user asks about "alert history", "past alerts", or "recent alerts":
- Use status="all" or omit status to see all alerts
- Apply time_start filter as needed (default: 7 days)

The time_start parameter is IGNORED when status="active" because active alerts
should always be shown regardless of when they were triggered.
</critical_status_behavior>

<parameters>
- connection_id: (optional) ID of a monitored connection. Omit to return alerts across all accessible connections.
- status: Filter by alert status. Values: "active", "cleared", "acknowledged", "all". Default: "all". IMPORTANT: Use "active" when checking for current/active alerts.
- rule_id: (optional) Filter to alerts from a specific alert rule
- metric_name: (optional) Filter to alerts for a specific metric
- time_start: Start of time range (default: "7d"). IGNORED when status="active".
- limit: Maximum results to return (default: 50, max: 100)
- offset: Pagination offset (default: 0)
</parameters>

<output>
Returns TSV data with:
- connection_id: Connection ID (included when querying across all connections)
- connection_name: Connection name (included when querying across all connections)
- id: Alert ID
- triggered_at: When the alert was triggered
- severity: Alert severity (info, warning, critical)
- title: Alert title
- metric_value: The metric value that triggered the alert
- threshold_value: The threshold that was exceeded
- status: Current status (active, cleared, acknowledged)
- cleared_at: When the alert was cleared (if applicable)
- false_positive: Whether the alert was marked as a false positive (true/false, empty if not acknowledged)
- acknowledged_by: Username of who acknowledged the alert (empty if not acknowledged)
- notes: Acknowledgment notes/message (empty if not acknowledged)
</output>

<examples>
- get_alert_history(status="active") - ALL active alerts across all connections (no time filter)
- get_alert_history() - all alerts across all connections from last 7 days
- get_alert_history(time_start="24h") - all alerts from last 24 hours across all connections
- get_alert_history(status="active", connection_id=33) - active alerts for specific connection
- get_alert_history(rule_id=5, time_start="30d") - specific rule, last 30 days
</examples>`,
			CompactDescription: `Query alert history for monitored connections. Omit connection_id to see alerts across all accessible connections. Filter by status (active/cleared/acknowledged), time range, metric name, or rule ID. Defaults to last 7 days.`,
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]any{
					"connection_id": map[string]any{
						"type":        "integer",
						"description": "ID of a monitored connection. Omit to return alerts across all accessible connections.",
					},
					"status": map[string]any{
						"type":        "string",
						"description": "Filter by alert status. Use 'active' to see all currently active alerts (ignores time_start). Values: active, cleared, acknowledged, all. Default: all",
						"enum":        []string{"active", "cleared", "acknowledged", "all"},
						"default":     "all",
					},
					"rule_id": map[string]any{
						"type":        "integer",
						"description": "Filter to alerts from a specific alert rule ID.",
					},
					"metric_name": map[string]any{
						"type":        "string",
						"description": "Filter to alerts for a specific metric name.",
					},
					"time_start": map[string]any{
						"type":        "string",
						"description": "Start of time range. Relative duration (24h, 7d, 30d) or ISO 8601 format. Default: 7d. IGNORED when status='active'.",
						"default":     "7d",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results to return (1-100). Default: 50",
						"default":     50,
						"minimum":     1,
						"maximum":     100,
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "Pagination offset. Default: 0",
						"default":     0,
						"minimum":     0,
					},
				},
				Required: []string{},
			},
		},
		Handler: func(args map[string]any) (mcp.ToolResponse, error) {
			if pool == nil {
				return mcp.NewToolError("Datastore not configured. The get_alert_history tool requires a datastore connection.")
			}

			// Extract context from args (injected by registry.Execute)
			ctx, ok := args["__context"].(context.Context)
			if !ok {
				ctx = context.Background()
			}

			// Determine whether we are querying a single connection or all accessible connections
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

			// Build accessible connection filter for multi-connection mode.
			// VisibleConnectionIDs honors ownership and sharing in addition
			// to group/token grants; unlike GetAccessibleConnections its
			// return values are unambiguous.
			var accessibleIDs []int
			allConnections := true
			if !singleConnection && rbacChecker != nil {
				ids, all, err := rbacChecker.VisibleConnectionIDs(ctx, visibilityLister)
				if err != nil {
					return mcp.NewToolError(fmt.Sprintf("Failed to resolve accessible connections: %v", err))
				}
				accessibleIDs = ids
				allConnections = all
				if !allConnections && len(accessibleIDs) == 0 {
					return mcp.NewToolSuccess("No alerts found. You do not have access to any connections.")
				}
			}

			// Parse optional rule_id
			var ruleID *int
			if _, ok := args["rule_id"]; ok {
				rid, err := parseIntArg(args, "rule_id")
				if err != nil {
					return mcp.NewToolError("Invalid 'rule_id' parameter: must be an integer")
				}
				ruleID = &rid
			}

			// Parse optional metric_name
			var metricName *string
			if mn, ok := args["metric_name"].(string); ok && mn != "" {
				metricName = &mn
			}

			// Parse status filter (default: "all")
			statusFilter := "all"
			if s, ok := args["status"].(string); ok && s != "" {
				s = strings.ToLower(s)
				validStatuses := map[string]bool{"active": true, "cleared": true, "acknowledged": true, "all": true}
				if !validStatuses[s] {
					return mcp.NewToolError("Invalid 'status' parameter: must be one of 'active', 'cleared', 'acknowledged', or 'all'")
				}
				statusFilter = s
			}

			// Parse time_start (default: 7 days) - IGNORED when status="active"
			var timeStart *time.Time
			if statusFilter != "active" {
				// Only apply time filter for non-active status queries
				t := time.Now().UTC().Add(-7 * 24 * time.Hour)
				if startStr, ok := args["time_start"].(string); ok && startStr != "" {
					if dur, err := parseRelativeDuration(startStr); err == nil {
						t = time.Now().UTC().Add(-dur)
					} else if parsed, err := parseTimeArg(startStr); err == nil {
						t = parsed
					} else {
						return mcp.NewToolError("Invalid 'time_start' parameter: use relative duration (e.g., '24h', '7d') or ISO 8601 format")
					}
				}
				timeStart = &t
			}

			// Parse limit (default: 50, max: 100)
			limit := 50
			if limitVal, ok := args["limit"]; ok {
				l, err := parseIntArg(args, "limit")
				if err == nil && l > 0 && l <= 100 {
					limit = l
				} else if limitVal != nil {
					return mcp.NewToolError("Invalid 'limit' parameter: must be between 1 and 100")
				}
			}

			// Parse offset (default: 0)
			offset := 0
			if offsetVal, ok := args["offset"]; ok {
				o, err := parseIntArg(args, "offset")
				if err == nil && o >= 0 {
					offset = o
				} else if offsetVal != nil {
					return mcp.NewToolError("Invalid 'offset' parameter: must be a non-negative integer")
				}
			}

			// Convert statusFilter for query - use nil for "all" to skip status check
			var statusParam *string
			if statusFilter != "all" {
				statusParam = &statusFilter
			}

			if singleConnection {
				return alertHistorySingleConnection(ctx, pool, connectionID, connName,
					timeStart, statusParam, statusFilter, ruleID, metricName, limit, offset)
			}
			return alertHistoryAllConnections(ctx, pool, allConnections, accessibleIDs,
				timeStart, statusParam, statusFilter, ruleID, metricName, limit, offset)
		},
	}
}

// alertHistorySingleConnection queries alerts for a single connection (original behavior)
func alertHistorySingleConnection(
	ctx context.Context, pool *pgxpool.Pool,
	connectionID int, connName string,
	timeStart *time.Time, statusParam *string, statusFilter string,
	ruleID *int, metricName *string, limit, offset int,
) (mcp.ToolResponse, error) {
	query := `
        SELECT a.id, a.triggered_at, a.severity, a.title, a.description, a.metric_name,
               a.metric_value, a.threshold_value, a.operator, a.status, a.cleared_at,
               ack.false_positive, ack.acknowledged_by, ack.message
        FROM alerts a
        LEFT JOIN LATERAL (
            SELECT acknowledged_at, acknowledged_by, message, false_positive
            FROM alert_acknowledgments
            WHERE alert_id = a.id
            ORDER BY acknowledged_at DESC
            LIMIT 1
        ) ack ON true
        WHERE a.connection_id = $1
          AND ($2::timestamp IS NULL OR a.triggered_at >= $2)
          AND ($3::text IS NULL OR $3 = 'all' OR a.status = $3)
          AND ($4::bigint IS NULL OR a.rule_id = $4)
          AND ($5::text IS NULL OR a.metric_name = $5)
        ORDER BY a.triggered_at DESC
        LIMIT $6 OFFSET $7
    `

	rows, err := pool.Query(ctx, query, connectionID, timeStart, statusParam, ruleID, metricName, limit, offset)
	if err != nil {
		return mcp.NewToolError(fmt.Sprintf("Failed to query alerts: %v", err))
	}
	defer rows.Close()

	// Build TSV output
	var sb strings.Builder
	timeInfo := "all time"
	if timeStart != nil {
		timeInfo = "since " + timeStart.Format(time.RFC3339)
	}
	sb.WriteString(fmt.Sprintf("Alerts | Connection: %d (%s) | Status: %s | Time: %s | Limit: %d\n\n",
		connectionID, connName, statusFilter, timeInfo, limit))

	// Header
	sb.WriteString("id\ttriggered_at\tseverity\ttitle\tmetric_value\tthreshold_value\tstatus\tcleared_at\tfalse_positive\tacknowledged_by\tnotes\n")

	// Data rows
	rowCount := 0
	for rows.Next() {
		var (
			id             int64
			triggeredAt    time.Time
			severity       string
			title          string
			description    string
			metricNameVal  *string
			metricValue    *float64
			thresholdValue *float64
			operator       *string
			status         string
			clearedAt      *time.Time
			falsePositive  *bool
			acknowledgedBy *string
			ackMessage     *string
		)

		if err := rows.Scan(&id, &triggeredAt, &severity, &title, &description,
			&metricNameVal, &metricValue, &thresholdValue, &operator, &status, &clearedAt,
			&falsePositive, &acknowledgedBy, &ackMessage); err != nil {
			return mcp.NewToolError(fmt.Sprintf("Failed to scan row: %v", err))
		}

		// Format row
		sb.WriteString(fmt.Sprintf("%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			id,
			triggeredAt.Format(time.RFC3339),
			severity,
			tsv.FormatValue(title),
			formatOptionalFloat(metricValue),
			formatOptionalFloat(thresholdValue),
			status,
			formatOptionalTime(clearedAt),
			formatOptionalBool(falsePositive),
			formatOptionalString(acknowledgedBy),
			formatOptionalStringEscaped(ackMessage),
		))
		rowCount++
	}

	if err := rows.Err(); err != nil {
		return mcp.NewToolError(fmt.Sprintf("Error iterating results: %v", err))
	}

	if rowCount == 0 {
		if statusFilter == "active" {
			return mcp.NewToolSuccess(fmt.Sprintf("No active alerts for connection %d (%s).", connectionID, connName))
		}
		return mcp.NewToolSuccess(fmt.Sprintf("No alerts found for connection %d (%s) with status='%s' in the specified time range.", connectionID, connName, statusFilter))
	}

	sb.WriteString(fmt.Sprintf("\n(%d rows)\n", rowCount))

	return mcp.NewToolSuccess(sb.String())
}

// alertHistoryAllConnections queries alerts across all accessible connections
func alertHistoryAllConnections(
	ctx context.Context, pool *pgxpool.Pool,
	allConnections bool, accessibleIDs []int,
	timeStart *time.Time, statusParam *string, statusFilter string,
	ruleID *int, metricName *string, limit, offset int,
) (mcp.ToolResponse, error) {
	// Build connection filter clause
	connFilter, connArgs := buildConnectionFilter("a.connection_id", allConnections, accessibleIDs)

	// Build the parameterised query; parameter positions start after connection args
	paramIdx := len(connArgs) + 1
	query := fmt.Sprintf(`
        SELECT a.id, a.connection_id, c.name AS connection_name,
               a.triggered_at, a.severity, a.title, a.description, a.metric_name,
               a.metric_value, a.threshold_value, a.operator, a.status, a.cleared_at,
               ack.false_positive, ack.acknowledged_by, ack.message
        FROM alerts a
        JOIN connections c ON c.id = a.connection_id
        LEFT JOIN LATERAL (
            SELECT acknowledged_at, acknowledged_by, message, false_positive
            FROM alert_acknowledgments
            WHERE alert_id = a.id
            ORDER BY acknowledged_at DESC
            LIMIT 1
        ) ack ON true
        WHERE %s
          AND ($%d::timestamp IS NULL OR a.triggered_at >= $%d)
          AND ($%d::text IS NULL OR $%d = 'all' OR a.status = $%d)
          AND ($%d::bigint IS NULL OR a.rule_id = $%d)
          AND ($%d::text IS NULL OR a.metric_name = $%d)
        ORDER BY a.triggered_at DESC
        LIMIT $%d OFFSET $%d
    `,
		connFilter,
		paramIdx, paramIdx,
		paramIdx+1, paramIdx+1, paramIdx+1,
		paramIdx+2, paramIdx+2,
		paramIdx+3, paramIdx+3,
		paramIdx+4, paramIdx+5,
	)

	queryArgs := make([]any, 0, len(connArgs)+6)
	queryArgs = append(queryArgs, connArgs...)
	queryArgs = append(queryArgs, timeStart, statusParam, ruleID, metricName, limit, offset)

	rows, err := pool.Query(ctx, query, queryArgs...)
	if err != nil {
		return mcp.NewToolError(fmt.Sprintf("Failed to query alerts: %v", err))
	}
	defer rows.Close()

	// Build TSV output
	var sb strings.Builder
	timeInfo := "all time"
	if timeStart != nil {
		timeInfo = "since " + timeStart.Format(time.RFC3339)
	}
	sb.WriteString(fmt.Sprintf("Alerts | All accessible connections | Status: %s | Time: %s | Limit: %d\n\n",
		statusFilter, timeInfo, limit))

	// Header - includes connection_id and connection_name columns
	sb.WriteString("connection_id\tconnection_name\tid\ttriggered_at\tseverity\ttitle\tmetric_value\tthreshold_value\tstatus\tcleared_at\tfalse_positive\tacknowledged_by\tnotes\n")

	// Data rows
	rowCount := 0
	for rows.Next() {
		var (
			id             int64
			connID         int
			connNameVal    string
			triggeredAt    time.Time
			severity       string
			title          string
			description    string
			metricNameVal  *string
			metricValue    *float64
			thresholdValue *float64
			operator       *string
			status         string
			clearedAt      *time.Time
			falsePositive  *bool
			acknowledgedBy *string
			ackMessage     *string
		)

		if err := rows.Scan(&id, &connID, &connNameVal, &triggeredAt, &severity, &title, &description,
			&metricNameVal, &metricValue, &thresholdValue, &operator, &status, &clearedAt,
			&falsePositive, &acknowledgedBy, &ackMessage); err != nil {
			return mcp.NewToolError(fmt.Sprintf("Failed to scan row: %v", err))
		}

		sb.WriteString(fmt.Sprintf("%d\t%s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			connID,
			tsv.FormatValue(connNameVal),
			id,
			triggeredAt.Format(time.RFC3339),
			severity,
			tsv.FormatValue(title),
			formatOptionalFloat(metricValue),
			formatOptionalFloat(thresholdValue),
			status,
			formatOptionalTime(clearedAt),
			formatOptionalBool(falsePositive),
			formatOptionalString(acknowledgedBy),
			formatOptionalStringEscaped(ackMessage),
		))
		rowCount++
	}

	if err := rows.Err(); err != nil {
		return mcp.NewToolError(fmt.Sprintf("Error iterating results: %v", err))
	}

	if rowCount == 0 {
		if statusFilter == "active" {
			return mcp.NewToolSuccess("No active alerts across accessible connections.")
		}
		return mcp.NewToolSuccess(fmt.Sprintf("No alerts found across accessible connections with status='%s' in the specified time range.", statusFilter))
	}

	sb.WriteString(fmt.Sprintf("\n(%d rows)\n", rowCount))

	return mcp.NewToolSuccess(sb.String())
}

// formatOptionalFloat formats an optional float64 pointer for TSV output
func formatOptionalFloat(f *float64) string {
	if f == nil {
		return ""
	}
	return fmt.Sprintf("%v", *f)
}

// formatOptionalTime formats an optional time pointer for TSV output
func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}

// formatOptionalBool formats an optional bool pointer for TSV output
func formatOptionalBool(b *bool) string {
	if b == nil {
		return ""
	}
	if *b {
		return "true"
	}
	return "false"
}

// formatOptionalString formats an optional string pointer for TSV output
func formatOptionalString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// formatOptionalStringEscaped formats an optional string pointer for TSV output,
// escaping tabs and newlines using tsv.FormatValue
func formatOptionalStringEscaped(s *string) string {
	if s == nil {
		return ""
	}
	return tsv.FormatValue(*s)
}
