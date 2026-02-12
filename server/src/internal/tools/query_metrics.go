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
	"github.com/pgedge/ai-workbench/server/internal/metrics"
	"github.com/pgedge/ai-workbench/server/internal/tsv"
)

// QueryMetricsTool creates the query_metrics tool for querying collected metrics
func QueryMetricsTool(pool *pgxpool.Pool) Tool {
	return Tool{
		Definition: mcp.Tool{
			Name: "query_metrics",
			Description: `Query collected metrics from the datastore with time-based aggregation.

<database_context>
This tool queries the DATASTORE to retrieve historical metrics collected by the
collector from monitored PostgreSQL servers. Data is aggregated into time buckets
for efficient analysis and visualization.
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
- Analyze performance trends over time (CPU, memory, I/O, queries)
- Identify patterns in database activity
- Investigate historical issues (slow queries, resource spikes)
- Compare metrics across time periods
- Monitor replication lag and other operational metrics
</usecase>

<parameters>
- probe_name: (required) Name of the probe (from list_probes)
- connection_id: ID of the monitored connection. OMIT this parameter to use the currently
  selected connection. Only specify explicitly for cross-connection analysis.
- time_start: Start of time range (ISO 8601 format or relative: "1h", "24h", "7d")
- time_end: End of time range (ISO 8601 or "now", default: now)
- buckets: Number of time buckets for aggregation (default: 150)
- metrics: Comma-separated list of metric columns (default: all numeric columns)
- database_name: Filter by database name (for database-scoped probes)
- schema_name: Filter by schema name (for table/index probes)
- table_name: Filter by table name (for table/index probes)
- aggregation: "avg", "sum", "min", "max", "last" (default: "avg")
</parameters>

<output>
Returns TSV data with:
- bucket_time: Start time of each bucket
- One column per metric with aggregated values
</output>

<examples>
- query_metrics(probe_name="pg_stat_database", time_start="24h") - uses current connection
- query_metrics(probe_name="pg_stat_statements", time_start="1h", metrics="total_exec_time,calls")
- query_metrics(probe_name="pg_sys_cpu_info", connection_id=5, time_start="7d") - specific connection
</examples>

<rate_limit_awareness>
To manage response sizes:
- Use fewer buckets (50-150) for overview
- Filter to specific metrics of interest
- Use filters (database_name, table_name) to reduce data volume
</rate_limit_awareness>`,
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"probe_name": map[string]interface{}{
						"type":        "string",
						"description": "Name of the probe to query (from list_probes output)",
					},
					"connection_id": map[string]interface{}{
						"type":        "integer",
						"description": "ID of the monitored connection to query metrics for. If not specified, uses the currently selected connection.",
					},
					"time_start": map[string]interface{}{
						"type":        "string",
						"description": "Start of time range. ISO 8601 format (2024-01-15T10:00:00Z) or relative duration (1h, 24h, 7d, 30d). Default: 1h",
						"default":     "1h",
					},
					"time_end": map[string]interface{}{
						"type":        "string",
						"description": "End of time range. ISO 8601 format or 'now'. Default: now",
						"default":     "now",
					},
					"buckets": map[string]interface{}{
						"type":        "integer",
						"description": "Number of time buckets for aggregation (1-500). Default: 150",
						"default":     150,
						"minimum":     1,
						"maximum":     500,
					},
					"metrics": map[string]interface{}{
						"type":        "string",
						"description": "Comma-separated list of metric columns to include. Default: all numeric columns",
					},
					"database_name": map[string]interface{}{
						"type":        "string",
						"description": "Filter by database name (for database-scoped probes)",
					},
					"schema_name": map[string]interface{}{
						"type":        "string",
						"description": "Filter by schema name (for table/index probes)",
					},
					"table_name": map[string]interface{}{
						"type":        "string",
						"description": "Filter by table name (for table/index probes)",
					},
					"aggregation": map[string]interface{}{
						"type":        "string",
						"description": "Aggregation function: avg, sum, min, max, last. Default: avg",
						"default":     "avg",
						"enum":        []string{"avg", "sum", "min", "max", "last"},
					},
				},
				Required: []string{"probe_name"},
			},
		},
		Handler: func(args map[string]interface{}) (mcp.ToolResponse, error) {
			if pool == nil {
				return mcp.NewToolError("Datastore not configured. The query_metrics tool requires a datastore connection.")
			}

			// Extract context from args (injected by registry.Execute)
			ctx, ok := args["__context"].(context.Context)
			if !ok {
				ctx = context.Background()
			}

			// Parse required parameters
			probeName, pok := args["probe_name"].(string)
			if !pok || probeName == "" {
				return mcp.NewToolError("Missing or invalid 'probe_name' parameter")
			}

			if !metrics.IsValidIdentifier(probeName) {
				return mcp.NewToolError("Invalid probe name. Probe names must contain only letters, numbers, and underscores.")
			}

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

			// Parse time range
			timeStart, timeEnd, err := parseTimeRange(args)
			if err != nil {
				return mcp.NewToolError("Invalid time range: " + err.Error())
			}

			// Parse buckets (default: 150)
			buckets := 150
			if bucketsVal, ok := args["buckets"]; ok {
				b, err := parseIntArg(args, "buckets")
				if err == nil && b > 0 && b <= 500 {
					buckets = b
				} else if bucketsVal != nil {
					return mcp.NewToolError("Invalid 'buckets' parameter: must be between 1 and 500")
				}
			}

			// Parse aggregation function (default: avg)
			aggregation := "avg"
			if aggVal, ok := args["aggregation"].(string); ok && aggVal != "" {
				aggVal = strings.ToLower(aggVal)
				validAggs := map[string]bool{"avg": true, "sum": true, "min": true, "max": true, "last": true}
				if !validAggs[aggVal] {
					return mcp.NewToolError("Invalid 'aggregation' parameter: must be one of avg, sum, min, max, last")
				}
				aggregation = aggVal
			}

			// Verify probe exists
			existsQuery := `
				SELECT COUNT(*) FROM information_schema.tables
				WHERE table_schema = 'metrics'
					AND table_name = $1
					AND table_type = 'BASE TABLE'
			`
			var count int
			if err := pool.QueryRow(ctx, existsQuery, probeName).Scan(&count); err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to verify probe: %v", err))
			}
			if count == 0 {
				return mcp.NewToolError(fmt.Sprintf("Probe '%s' not found. Use list_probes to see available probes.", probeName))
			}

			// Get metric columns from the probe
			metricCols, err := metrics.GetProbeMetricColumns(ctx, pool, probeName)
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to get probe columns: %v", err))
			}

			// Filter to requested metrics if specified
			if metricsArg, ok := args["metrics"].(string); ok && metricsArg != "" {
				requestedMetrics := strings.Split(metricsArg, ",")
				filteredCols := []string{}
				for _, m := range requestedMetrics {
					m = strings.TrimSpace(m)
					// Validate metric name
					if !metrics.IsValidIdentifier(m) {
						return mcp.NewToolError(fmt.Sprintf("Invalid metric name: %s", m))
					}
					// Check if metric exists
					found := false
					for _, col := range metricCols {
						if col == m {
							filteredCols = append(filteredCols, m)
							found = true
							break
						}
					}
					if !found {
						return mcp.NewToolError(fmt.Sprintf("Metric '%s' not found in probe '%s'. Use describe_probe to see available metrics.", m, probeName))
					}
				}
				metricCols = filteredCols
			}

			if len(metricCols) == 0 {
				return mcp.NewToolError(fmt.Sprintf("No numeric metrics found in probe '%s'. Use describe_probe to see available columns.", probeName))
			}

			// Build the aggregated query using shared package
			filters := metrics.MetricFilters{}
			if dbName, fok := args["database_name"].(string); fok && dbName != "" {
				filters.DatabaseName = dbName
			}
			if schemaName, fok := args["schema_name"].(string); fok && schemaName != "" {
				filters.SchemaName = schemaName
			}
			if tableName, fok := args["table_name"].(string); fok && tableName != "" {
				filters.TableName = tableName
			}
			query, queryArgs, err := metrics.BuildMetricsQuery(probeName, metricCols, connectionID, timeStart, timeEnd, buckets, aggregation, filters)
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to build query: %v", err))
			}

			// Execute query
			rows, err := pool.Query(ctx, query, queryArgs...)
			if err != nil {
				return mcp.NewToolError(fmt.Sprintf("Failed to query metrics: %v", err))
			}
			defer rows.Close()

			// Build TSV output
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("Probe: %s | Connection: %d | Time: %s to %s | Buckets: %d | Aggregation: %s\n\n",
				probeName, connectionID,
				timeStart.Format(time.RFC3339),
				timeEnd.Format(time.RFC3339),
				buckets, aggregation))

			// Header
			sb.WriteString("bucket_time")
			for _, col := range metricCols {
				sb.WriteString("\t")
				sb.WriteString(col)
			}
			sb.WriteString("\n")

			// Data rows
			rowCount := 0
			for rows.Next() {
				// Scan bucket time and all metrics
				values := make([]interface{}, len(metricCols)+1)
				valuePtrs := make([]interface{}, len(metricCols)+1)
				var bucketTime time.Time
				valuePtrs[0] = &bucketTime
				for i := range metricCols {
					var v interface{}
					values[i+1] = &v
					valuePtrs[i+1] = &values[i+1]
				}

				if err := rows.Scan(valuePtrs...); err != nil {
					return mcp.NewToolError(fmt.Sprintf("Failed to scan row: %v", err))
				}

				// Format row
				sb.WriteString(bucketTime.Format(time.RFC3339))
				for i := range metricCols {
					sb.WriteString("\t")
					sb.WriteString(formatMetricValue(values[i+1]))
				}
				sb.WriteString("\n")
				rowCount++
			}

			if err := rows.Err(); err != nil {
				return mcp.NewToolError(fmt.Sprintf("Error iterating results: %v", err))
			}

			if rowCount == 0 {
				return mcp.NewToolSuccess(fmt.Sprintf("No metrics data found for probe '%s' in the specified time range.", probeName))
			}

			sb.WriteString(fmt.Sprintf("\n(%d rows)\n", rowCount))

			return mcp.NewToolSuccess(sb.String())
		},
	}
}

// parseIntArg parses an integer argument from the args map
func parseIntArg(args map[string]interface{}, name string) (int, error) {
	val, ok := args[name]
	if !ok {
		return 0, fmt.Errorf("parameter not found")
	}

	switch v := val.(type) {
	case float64:
		return int(v), nil
	case int:
		return v, nil
	case int64:
		return int(v), nil
	default:
		return 0, fmt.Errorf("invalid type: expected number")
	}
}

// parseTimeRange parses time_start and time_end from args
func parseTimeRange(args map[string]interface{}) (time.Time, time.Time, error) {
	now := time.Now().UTC()
	timeEnd := now

	// Parse time_end
	if endStr, ok := args["time_end"].(string); ok && endStr != "" && endStr != "now" {
		parsed, err := parseTimeArg(endStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid time_end: %w", err)
		}
		timeEnd = parsed
	}

	// Parse time_start (default: 1 hour ago)
	timeStart := timeEnd.Add(-1 * time.Hour)
	if startStr, ok := args["time_start"].(string); ok && startStr != "" {
		// Check for relative duration (e.g., "1h", "24h", "7d")
		if dur, err := parseRelativeDuration(startStr); err == nil {
			timeStart = timeEnd.Add(-dur)
		} else {
			// Try parsing as absolute time
			parsed, err := parseTimeArg(startStr)
			if err != nil {
				return time.Time{}, time.Time{}, fmt.Errorf("invalid time_start: %w", err)
			}
			timeStart = parsed
		}
	}

	if timeStart.After(timeEnd) {
		return time.Time{}, time.Time{}, fmt.Errorf("time_start must be before time_end")
	}

	return timeStart, timeEnd, nil
}

// parseRelativeDuration parses relative duration strings like "1h", "24h", "7d", "30d"
func parseRelativeDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}

	// Handle day suffix
	if strings.HasSuffix(s, "d") {
		days := strings.TrimSuffix(s, "d")
		var d int
		if _, err := fmt.Sscanf(days, "%d", &d); err != nil || d <= 0 {
			return 0, fmt.Errorf("invalid duration: %s", s)
		}
		return time.Duration(d) * 24 * time.Hour, nil
	}

	// Use standard Go duration parsing for h, m, s
	return time.ParseDuration(s)
}

// parseTimeArg parses an absolute time argument
func parseTimeArg(s string) (time.Time, error) {
	// Try common formats
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t.UTC(), nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse time: %s", s)
}

// formatMetricValue formats a metric value for TSV output.
// Handles pointer dereferencing and delegates to tsv.FormatValue for type conversion.
func formatMetricValue(v interface{}) string {
	if v == nil {
		return ""
	}

	// Dereference pointer if needed (common when scanning database rows)
	if ptr, ok := v.(*interface{}); ok {
		if ptr == nil || *ptr == nil {
			return ""
		}
		return formatMetricValue(*ptr)
	}

	// Delegate to the centralized TSV formatting function
	return tsv.FormatValue(v)
}
