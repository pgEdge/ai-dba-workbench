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
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
	"github.com/pgedge/ai-workbench/server/internal/tsv"
)

// GetAlertHistoryTool creates the get_alert_history tool for querying historic alerts
func GetAlertHistoryTool(pool *pgxpool.Pool) Tool {
	return Tool{
		Definition: mcp.Tool{
			Name: "get_alert_history",
			Description: `Query alerts for a monitored connection.

<database_context>
This tool queries the DATASTORE to retrieve alerts that have been triggered
for monitored PostgreSQL servers. Use this to check current alert status or
analyze historical alert patterns.
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
- connection_id: ID of the monitored connection. OMIT to use the currently selected connection.
- status: Filter by alert status. Values: "active", "cleared", "acknowledged", "all". Default: "all". IMPORTANT: Use "active" when checking for current/active alerts.
- rule_id: (optional) Filter to alerts from a specific alert rule
- metric_name: (optional) Filter to alerts for a specific metric
- time_start: Start of time range (default: "7d"). IGNORED when status="active".
- limit: Maximum results to return (default: 50, max: 100)
- offset: Pagination offset (default: 0)
</parameters>

<output>
Returns TSV data with:
- id: Alert ID
- triggered_at: When the alert was triggered
- severity: Alert severity (info, warning, critical)
- title: Alert title
- metric_value: The metric value that triggered the alert
- threshold_value: The threshold that was exceeded
- status: Current status (active, cleared, acknowledged)
- cleared_at: When the alert was cleared (if applicable)
</output>

<examples>
- get_alert_history(status="active") - ALL active alerts (no time filter)
- get_alert_history() - all alerts from last 7 days
- get_alert_history(time_start="24h") - all alerts from last 24 hours
- get_alert_history(status="active", connection_id=33) - active alerts for specific connection
- get_alert_history(rule_id=5, time_start="30d") - specific rule, last 30 days
</examples>`,
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"connection_id": map[string]interface{}{
						"type":        "integer",
						"description": "ID of the monitored connection. If not specified, uses the currently selected connection.",
					},
					"status": map[string]interface{}{
						"type":        "string",
						"description": "Filter by alert status. Use 'active' to see all currently active alerts (ignores time_start). Values: active, cleared, acknowledged, all. Default: all",
						"enum":        []string{"active", "cleared", "acknowledged", "all"},
						"default":     "all",
					},
					"rule_id": map[string]interface{}{
						"type":        "integer",
						"description": "Filter to alerts from a specific alert rule ID.",
					},
					"metric_name": map[string]interface{}{
						"type":        "string",
						"description": "Filter to alerts for a specific metric name.",
					},
					"time_start": map[string]interface{}{
						"type":        "string",
						"description": "Start of time range. Relative duration (24h, 7d, 30d) or ISO 8601 format. Default: 7d. IGNORED when status='active'.",
						"default":     "7d",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results to return (1-100). Default: 50",
						"default":     50,
						"minimum":     1,
						"maximum":     100,
					},
					"offset": map[string]interface{}{
						"type":        "integer",
						"description": "Pagination offset. Default: 0",
						"default":     0,
						"minimum":     0,
					},
				},
				Required: []string{},
			},
		},
		Handler: func(args map[string]interface{}) (mcp.ToolResponse, error) {
			if pool == nil {
				return mcp.NewToolError("Datastore not configured. The get_alert_history tool requires a datastore connection.")
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

			// Build the query with conditional time and status filters
			query := `
                SELECT id, triggered_at, severity, title, description, metric_name,
                       metric_value, threshold_value, operator, status, cleared_at
                FROM alerts
                WHERE connection_id = $1
                  AND ($2::timestamp IS NULL OR triggered_at >= $2)
                  AND ($3::text IS NULL OR $3 = 'all' OR status = $3)
                  AND ($4::bigint IS NULL OR rule_id = $4)
                  AND ($5::text IS NULL OR metric_name = $5)
                ORDER BY triggered_at DESC
                LIMIT $6 OFFSET $7
            `

			// Convert statusFilter for query - use nil for "all" to skip status check
			var statusParam *string
			if statusFilter != "all" {
				statusParam = &statusFilter
			}

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
			sb.WriteString("id\ttriggered_at\tseverity\ttitle\tmetric_value\tthreshold_value\tstatus\tcleared_at\n")

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
				)

				if err := rows.Scan(&id, &triggeredAt, &severity, &title, &description,
					&metricNameVal, &metricValue, &thresholdValue, &operator, &status, &clearedAt); err != nil {
					return mcp.NewToolError(fmt.Sprintf("Failed to scan row: %v", err))
				}

				// Format row
				sb.WriteString(fmt.Sprintf("%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					id,
					triggeredAt.Format(time.RFC3339),
					severity,
					tsv.FormatValue(title),
					formatOptionalFloat(metricValue),
					formatOptionalFloat(thresholdValue),
					status,
					formatOptionalTime(clearedAt),
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
		},
	}
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
