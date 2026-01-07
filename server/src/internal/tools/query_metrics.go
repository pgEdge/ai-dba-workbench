/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
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
- connection_id: (required) ID of the monitored connection
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
- query_metrics(probe_name="pg_stat_database", connection_id=1, time_start="24h")
- query_metrics(probe_name="pg_stat_statements", connection_id=1, time_start="1h", metrics="total_exec_time,calls")
- query_metrics(probe_name="pg_sys_cpu_info", connection_id=1, time_start="7d", buckets=100)
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
						"description": "ID of the monitored connection to query metrics for",
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
				Required: []string{"probe_name", "connection_id"},
			},
		},
		Handler: func(args map[string]interface{}) (mcp.ToolResponse, error) {
			if pool == nil {
				return mcp.NewToolError("Datastore not configured. The query_metrics tool requires a datastore connection.")
			}

			// Parse required parameters
			probeName, ok := args["probe_name"].(string)
			if !ok || probeName == "" {
				return mcp.NewToolError("Missing or invalid 'probe_name' parameter")
			}

			if !isValidIdentifier(probeName) {
				return mcp.NewToolError("Invalid probe name. Probe names must contain only letters, numbers, and underscores.")
			}

			connectionID, err := parseIntArg(args, "connection_id")
			if err != nil {
				return mcp.NewToolError("Missing or invalid 'connection_id' parameter: " + err.Error())
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

			ctx := context.Background()

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
			metricCols, err := getProbeMetricColumns(ctx, pool, probeName)
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
					if !isValidIdentifier(m) {
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

			// Build the aggregated query
			query, queryArgs, err := buildMetricsQuery(probeName, metricCols, connectionID, timeStart, timeEnd, buckets, aggregation, args)
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

// getProbeMetricColumns returns the numeric metric columns for a probe
func getProbeMetricColumns(ctx context.Context, pool *pgxpool.Pool, probeName string) ([]string, error) {
	query := `
		SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = 'metrics'
			AND table_name = $1
		ORDER BY ordinal_position
	`

	rows, err := pool.Query(ctx, query, probeName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metricCols []string
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			return nil, err
		}
		if isMetricColumn(name, dataType) {
			metricCols = append(metricCols, name)
		}
	}

	return metricCols, rows.Err()
}

// buildMetricsQuery builds the aggregated metrics query
func buildMetricsQuery(probeName string, metricCols []string, connectionID int, timeStart, timeEnd time.Time, buckets int, aggregation string, args map[string]interface{}) (string, []interface{}, error) {
	// Calculate bucket width
	duration := timeEnd.Sub(timeStart)
	bucketWidth := duration / time.Duration(buckets)
	if bucketWidth < time.Second {
		bucketWidth = time.Second
	}

	// Build WHERE clause
	var whereClauses []string
	queryArgs := []interface{}{
		fmt.Sprintf("%d seconds", int(bucketWidth.Seconds())),
		connectionID,
		timeStart,
		timeEnd,
	}
	argNum := 5

	whereClauses = append(whereClauses, "connection_id = $2")
	whereClauses = append(whereClauses, "collected_at >= $3")
	whereClauses = append(whereClauses, "collected_at <= $4")

	// Add optional filters
	if dbName, ok := args["database_name"].(string); ok && dbName != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("datname = $%d", argNum))
		queryArgs = append(queryArgs, dbName)
		argNum++
	}

	if schemaName, ok := args["schema_name"].(string); ok && schemaName != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("schemaname = $%d", argNum))
		queryArgs = append(queryArgs, schemaName)
		argNum++
	}

	if tableName, ok := args["table_name"].(string); ok && tableName != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("relname = $%d", argNum))
		queryArgs = append(queryArgs, tableName)
		// argNum not needed after this point
	}

	// Build final query
	// Note: time_bucket is a PostgreSQL function available with pg_partman or timescaledb
	// We'll use a fallback approach with date_trunc for broader compatibility
	query := fmt.Sprintf(`
		WITH buckets AS (
			SELECT
				date_bin($1::interval, collected_at, $3) AS bucket_time,
				%s
			FROM metrics.%s
			WHERE %s
			GROUP BY date_bin($1::interval, collected_at, $3)
		)
		SELECT
			bucket_time,
			%s
		FROM buckets
		ORDER BY bucket_time
	`,
		strings.Join(getAggSelectCols(metricCols, aggregation), ", "),
		probeName,
		strings.Join(whereClauses, " AND "),
		strings.Join(metricCols, ", "),
	)

	return query, queryArgs, nil
}

// getAggSelectCols returns the aggregated select columns
func getAggSelectCols(metricCols []string, aggregation string) []string {
	var cols []string
	for _, col := range metricCols {
		if aggregation == "last" {
			cols = append(cols, fmt.Sprintf("(array_agg(%s ORDER BY collected_at DESC))[1] AS %s", col, col))
		} else {
			cols = append(cols, fmt.Sprintf("%s(%s) AS %s", aggregation, col, col))
		}
	}
	return cols
}

// formatMetricValue formats a metric value for TSV output
func formatMetricValue(v interface{}) string {
	if v == nil {
		return ""
	}

	// Dereference pointer if needed
	switch val := v.(type) {
	case *interface{}:
		if val == nil || *val == nil {
			return ""
		}
		return formatMetricValue(*val)
	case float64:
		// Format floats without unnecessary trailing zeros
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%.6g", val)
	case int64:
		return fmt.Sprintf("%d", val)
	case int:
		return fmt.Sprintf("%d", val)
	default:
		return fmt.Sprintf("%v", val)
	}
}
