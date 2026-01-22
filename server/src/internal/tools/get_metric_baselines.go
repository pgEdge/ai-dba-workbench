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
)

// GetMetricBaselinesTool creates the get_metric_baselines tool for querying statistical baselines
func GetMetricBaselinesTool(pool *pgxpool.Pool) Tool {
	return Tool{
		Definition: mcp.Tool{
			Name: "get_metric_baselines",
			Description: `Query statistical baselines for metrics used in anomaly detection.

<database_context>
This tool queries the DATASTORE to retrieve statistical baselines that have
been calculated from historical metric data. These baselines are used for
anomaly detection to identify unusual metric values.
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
- View established baselines for metrics
- Understand normal ranges for different time periods
- Provide context for anomaly alerts
- Compare current values against historical norms
</usecase>

<parameters>
- connection_id: ID of the monitored connection. OMIT to use the currently selected connection.
- metric_name: (optional) Filter to a specific metric name
</parameters>

<output>
Returns TSV data with:
- metric_name: Name of the metric
- period_type: Baseline period (hourly, daily, weekly)
- day_of_week: Day of week for weekly baselines (0=Sunday, 6=Saturday)
- hour_of_day: Hour for hourly baselines (0-23)
- mean: Average value
- stddev: Standard deviation
- min: Minimum observed value
- max: Maximum observed value
- sample_count: Number of samples in the baseline
</output>

<examples>
- get_metric_baselines() - uses current connection
- get_metric_baselines(metric_name="cpu_usage") - baselines for CPU usage
- get_metric_baselines(connection_id=5, metric_name="xact_commit") - transaction baselines for specific connection
</examples>`,
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"connection_id": map[string]interface{}{
						"type":        "integer",
						"description": "ID of the monitored connection. If not specified, uses the currently selected connection.",
					},
					"metric_name": map[string]interface{}{
						"type":        "string",
						"description": "Filter to a specific metric name.",
					},
				},
				Required: []string{},
			},
		},
		Handler: func(args map[string]interface{}) (mcp.ToolResponse, error) {
			if pool == nil {
				return mcp.NewToolError("Datastore not configured. The get_metric_baselines tool requires a datastore connection.")
			}

			// Parse connection_id (required after injection)
			connectionID, err := parseIntArg(args, "connection_id")
			if err != nil {
				return mcp.NewToolError("Missing or invalid 'connection_id' parameter. If you haven't selected a database connection, use list_connections to find available connection IDs, then specify connection_id explicitly.")
			}

			// Parse optional metric_name
			var metricName *string
			if mn, ok := args["metric_name"].(string); ok && mn != "" {
				metricName = &mn
			}

			// Extract context from args (injected by registry.Execute)
			ctx, ok := args["__context"].(context.Context)
			if !ok {
				ctx = context.Background()
			}

			// Build the query
			query := `
                SELECT metric_name, period_type, day_of_week, hour_of_day,
                       mean, stddev, min, max, sample_count, last_calculated
                FROM metric_baselines
                WHERE connection_id = $1
                  AND ($2::text IS NULL OR metric_name = $2)
                ORDER BY metric_name, period_type, day_of_week, hour_of_day
            `

			rows, err := pool.Query(ctx, query, connectionID, metricName)
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to query metric baselines: %v", err))
			}
			defer rows.Close()

			// Build TSV output
			var sb strings.Builder
			metricInfo := "all metrics"
			if metricName != nil {
				metricInfo = fmt.Sprintf("metric: %s", *metricName)
			}
			sb.WriteString(fmt.Sprintf("Metric Baselines | Connection: %d | %s\n\n",
				connectionID, metricInfo))

			// Header
			sb.WriteString("metric_name\tperiod_type\tday_of_week\thour_of_day\tmean\tstddev\tmin\tmax\tsample_count\n")

			// Data rows
			rowCount := 0
			for rows.Next() {
				var (
					metricNameVal  string
					periodType     string
					dayOfWeek      *int
					hourOfDay      *int
					mean           float64
					stddev         float64
					minVal         float64
					maxVal         float64
					sampleCount    int64
					lastCalculated time.Time
				)

				if err := rows.Scan(&metricNameVal, &periodType, &dayOfWeek, &hourOfDay,
					&mean, &stddev, &minVal, &maxVal, &sampleCount, &lastCalculated); err != nil {
					return mcp.NewToolError(fmt.Sprintf("Failed to scan row: %v", err))
				}

				// Format row
				sb.WriteString(fmt.Sprintf("%s\t%s\t%s\t%s\t%.4f\t%.4f\t%.4f\t%.4f\t%d\n",
					metricNameVal,
					periodType,
					formatOptionalInt(dayOfWeek),
					formatOptionalInt(hourOfDay),
					mean,
					stddev,
					minVal,
					maxVal,
					sampleCount,
				))
				rowCount++
			}

			if err := rows.Err(); err != nil {
				return mcp.NewToolError(fmt.Sprintf("Error iterating results: %v", err))
			}

			if rowCount == 0 {
				return mcp.NewToolSuccess(fmt.Sprintf("No metric baselines found for connection %d. Baselines are calculated after sufficient historical data is collected.", connectionID))
			}

			sb.WriteString(fmt.Sprintf("\n(%d baselines)\n", rowCount))

			return mcp.NewToolSuccess(sb.String())
		},
	}
}

// formatOptionalInt formats an optional int pointer for TSV output
func formatOptionalInt(i *int) string {
	if i == nil {
		return ""
	}
	return fmt.Sprintf("%d", *i)
}
