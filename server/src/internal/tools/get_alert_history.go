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
			Description: `Query historic alerts for a monitored connection.

<database_context>
This tool queries the DATASTORE to retrieve historical alerts that have been
triggered for monitored PostgreSQL servers. Use this to understand alert
patterns and recurring issues.
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

<usecase>
Use this tool to:
- View recent alerts for a connection
- Analyze alert patterns for specific rules or metrics
- Investigate recurring issues over time
- Understand the frequency and severity of alerts
</usecase>

<parameters>
- connection_id: ID of the monitored connection. OMIT to use the currently selected connection.
- rule_id: (optional) Filter to alerts from a specific alert rule
- metric_name: (optional) Filter to alerts for a specific metric
- time_start: Start of time range (default: "7d", supports relative like "24h", "7d", "30d")
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
- get_alert_history() - uses current connection, last 7 days
- get_alert_history(time_start="24h") - last 24 hours
- get_alert_history(rule_id=5, time_start="30d") - specific rule, last 30 days
- get_alert_history(metric_name="cpu_usage", limit=20) - CPU alerts, max 20 results
</examples>`,
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"connection_id": map[string]interface{}{
						"type":        "integer",
						"description": "ID of the monitored connection. If not specified, uses the currently selected connection.",
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
						"description": "Start of time range. Relative duration (24h, 7d, 30d) or ISO 8601 format. Default: 7d",
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

			// Parse connection_id (required after injection)
			connectionID, err := parseIntArg(args, "connection_id")
			if err != nil {
				return mcp.NewToolError("Missing or invalid 'connection_id' parameter. If you haven't selected a database connection, use list_connections to find available connection IDs, then specify connection_id explicitly.")
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

			// Parse time_start (default: 7 days)
			timeStart := time.Now().UTC().Add(-7 * 24 * time.Hour)
			if startStr, ok := args["time_start"].(string); ok && startStr != "" {
				if dur, err := parseRelativeDuration(startStr); err == nil {
					timeStart = time.Now().UTC().Add(-dur)
				} else if parsed, err := parseTimeArg(startStr); err == nil {
					timeStart = parsed
				} else {
					return mcp.NewToolError("Invalid 'time_start' parameter: use relative duration (e.g., '24h', '7d') or ISO 8601 format")
				}
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

			// Extract context from args (injected by registry.Execute)
			ctx, ok := args["__context"].(context.Context)
			if !ok {
				ctx = context.Background()
			}

			// Build the query
			query := `
                SELECT id, triggered_at, severity, title, description, metric_name,
                       metric_value, threshold_value, operator, status, cleared_at
                FROM alerts
                WHERE connection_id = $1
                  AND triggered_at >= $2
                  AND ($3::bigint IS NULL OR rule_id = $3)
                  AND ($4::text IS NULL OR metric_name = $4)
                ORDER BY triggered_at DESC
                LIMIT $5 OFFSET $6
            `

			rows, err := pool.Query(ctx, query, connectionID, timeStart, ruleID, metricName, limit, offset)
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to query alerts: %v", err))
			}
			defer rows.Close()

			// Build TSV output
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("Alert History | Connection: %d | Since: %s | Limit: %d | Offset: %d\n\n",
				connectionID, timeStart.Format(time.RFC3339), limit, offset))

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
				return mcp.NewToolSuccess(fmt.Sprintf("No alerts found for connection %d in the specified time range.", connectionID))
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
