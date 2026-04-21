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
)

// GetMetricBaselinesTool creates the get_metric_baselines tool for querying statistical baselines.
//
// The visibilityLister argument is used to resolve the set of connections
// the caller may see. It may be nil in unit tests or when no datastore is
// configured; auth.RBACChecker.VisibleConnectionIDs tolerates a nil lister
// by falling back to group/token-granted IDs only.
func GetMetricBaselinesTool(pool *pgxpool.Pool, rbacChecker *auth.RBACChecker, visibilityLister auth.ConnectionVisibilityLister) Tool {
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
- Specify connection_id to filter baselines for that connection
- "My database" or "the database" means the currently selected connection

If NO connection is selected (connected: false):
- Omit connection_id to see baselines across ALL accessible connections
- The user can also specify a connection_id to filter to one connection

When connection_id is omitted, returns baselines across all connections the
user has access to. Each row includes connection_id and connection_name
so you can identify which connection each baseline belongs to.
</important_behavior>

<usecase>
Use this tool to:
- View established baselines for metrics
- Understand normal ranges for different time periods
- Provide context for anomaly alerts
- Compare current values against historical norms
</usecase>

<parameters>
- connection_id: (optional) ID of a monitored connection. Omit to return baselines across all accessible connections.
- metric_name: (optional) Filter to a specific metric name
</parameters>

<output>
Returns TSV data with:
- connection_id: Connection ID (included when querying across all connections)
- connection_name: Connection name (included when querying across all connections)
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
- get_metric_baselines() - baselines across all accessible connections
- get_metric_baselines(metric_name="cpu_usage") - CPU usage baselines across all connections
- get_metric_baselines(connection_id=5, metric_name="xact_commit") - transaction baselines for specific connection
</examples>`,
			CompactDescription: `Query statistical baselines for metrics used in anomaly detection. Omit connection_id to see baselines across all accessible connections. Returns mean, stddev, min, max, and sample count. Filter by metric_name.`,
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]any{
					"connection_id": map[string]any{
						"type":        "integer",
						"description": "ID of a monitored connection. Omit to return baselines across all accessible connections.",
					},
					"metric_name": map[string]any{
						"type":        "string",
						"description": "Filter to a specific metric name.",
					},
				},
				Required: []string{},
			},
		},
		Handler: func(args map[string]any) (mcp.ToolResponse, error) {
			if pool == nil {
				return mcp.NewToolError("Datastore not configured. The get_metric_baselines tool requires a datastore connection.")
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
					return mcp.NewToolSuccess("No metric baselines found. You do not have access to any connections.")
				}
			}

			// Parse optional metric_name
			var metricName *string
			if mn, ok := args["metric_name"].(string); ok && mn != "" {
				metricName = &mn
			}

			if singleConnection {
				return baselinesSingleConnection(ctx, pool, connectionID, connName, metricName)
			}
			return baselinesAllConnections(ctx, pool, allConnections, accessibleIDs, metricName)
		},
	}
}

// baselinesSingleConnection queries baselines for a single connection (original behavior)
func baselinesSingleConnection(
	ctx context.Context, pool *pgxpool.Pool,
	connectionID int, connName string, metricName *string,
) (mcp.ToolResponse, error) {
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
}

// baselinesAllConnections queries baselines across all accessible connections
func baselinesAllConnections(
	ctx context.Context, pool *pgxpool.Pool,
	allConnections bool, accessibleIDs []int, metricName *string,
) (mcp.ToolResponse, error) {
	connFilter, connArgs := buildConnectionFilter("mb.connection_id", allConnections, accessibleIDs)

	paramIdx := len(connArgs) + 1
	query := fmt.Sprintf(`
        SELECT mb.connection_id, c.name AS connection_name,
               mb.metric_name, mb.period_type, mb.day_of_week, mb.hour_of_day,
               mb.mean, mb.stddev, mb.min, mb.max, mb.sample_count, mb.last_calculated
        FROM metric_baselines mb
        JOIN connections c ON c.id = mb.connection_id
        WHERE %s
          AND ($%d::text IS NULL OR mb.metric_name = $%d)
        ORDER BY c.name, mb.metric_name, mb.period_type, mb.day_of_week, mb.hour_of_day
    `, connFilter, paramIdx, paramIdx)

	queryArgs := make([]any, 0, len(connArgs)+1)
	queryArgs = append(queryArgs, connArgs...)
	queryArgs = append(queryArgs, metricName)

	rows, err := pool.Query(ctx, query, queryArgs...) // nosemgrep: go-sql-concat-sqli
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
	sb.WriteString(fmt.Sprintf("Metric Baselines | All accessible connections | %s\n\n", metricInfo))

	// Header - includes connection columns
	sb.WriteString("connection_id\tconnection_name\tmetric_name\tperiod_type\tday_of_week\thour_of_day\tmean\tstddev\tmin\tmax\tsample_count\n")

	// Data rows
	rowCount := 0
	for rows.Next() {
		var (
			connID         int
			connNameVal    string
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

		if err := rows.Scan(&connID, &connNameVal, &metricNameVal, &periodType, &dayOfWeek, &hourOfDay,
			&mean, &stddev, &minVal, &maxVal, &sampleCount, &lastCalculated); err != nil {
			return mcp.NewToolError(fmt.Sprintf("Failed to scan row: %v", err))
		}

		sb.WriteString(fmt.Sprintf("%d\t%s\t%s\t%s\t%s\t%s\t%.4f\t%.4f\t%.4f\t%.4f\t%d\n",
			connID,
			connNameVal,
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
		return mcp.NewToolSuccess("No metric baselines found across accessible connections. Baselines are calculated after sufficient historical data is collected.")
	}

	sb.WriteString(fmt.Sprintf("\n(%d baselines)\n", rowCount))

	return mcp.NewToolSuccess(sb.String())
}

// formatOptionalInt formats an optional int pointer for TSV output
func formatOptionalInt(i *int) string {
	if i == nil {
		return ""
	}
	return fmt.Sprintf("%d", *i)
}
