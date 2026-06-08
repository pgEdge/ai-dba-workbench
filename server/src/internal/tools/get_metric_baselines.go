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
- metric_name: (optional) Filter by metric name. Stored names are fully-qualified as view.metric (e.g. pg_stat_database.cache_hit_ratio). Matching is partial and case-insensitive, so a shorthand like "cache_hit_ratio" or "cache_hit" matches "pg_stat_database.cache_hit_ratio".
</parameters>

<metric_naming>
Stored metric names are fully-qualified in the form view.metric, where view
is the source statistics view and metric is the column (for example,
pg_stat_database.cache_hit_ratio or pg_stat_bgwriter.checkpoints_timed).
You do not need the full name: metric_name matching is partial and
case-insensitive, so passing "cache_hit_ratio" matches
"pg_stat_database.cache_hit_ratio". If a filter matches nothing, the tool
returns the list of available metric names so you can retry with a correct
value.
</metric_naming>

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
- get_metric_baselines(metric_name="cache_hit_ratio") - matches the fully-qualified pg_stat_database.cache_hit_ratio across all connections
- get_metric_baselines(connection_id=5, metric_name="xact_commit") - matches pg_stat_database.xact_commit for a specific connection
- get_metric_baselines(metric_name="pg_stat_database.cache_hit_ratio") - the fully-qualified name also works
</examples>`,
			CompactDescription: `Query statistical baselines for metrics used in anomaly detection. Omit connection_id to see baselines across all accessible connections. Returns mean, stddev, min, max, and sample count. Filter by metric_name (partial, case-insensitive match against fully-qualified view.metric names, e.g. pg_stat_database.cache_hit_ratio).`,
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]any{
					"connection_id": map[string]any{
						"type":        "integer",
						"description": "ID of a monitored connection. Omit to return baselines across all accessible connections.",
					},
					"metric_name": map[string]any{
						"type":        "string",
						"description": "Filter by metric name. Stored names are fully-qualified as view.metric (e.g. pg_stat_database.cache_hit_ratio). Matching is partial and case-insensitive, so a shorthand such as \"cache_hit_ratio\" matches \"pg_stat_database.cache_hit_ratio\". If nothing matches, the response lists the available metric names.",
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
			// to group/token grants, and returns an explicit
			// allConnections flag so callers cannot confuse "full access"
			// with "no grants".
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

// escapeLikePattern escapes the LIKE/ILIKE wildcard characters ('%' and '_')
// and the escape character itself ('\') in user-supplied text so the value is
// matched literally as a substring. The result is intended to be wrapped in
// '%' ... '%' and bound as a positional parameter, with the SQL using the
// `ESCAPE '\'` clause. This keeps a literal '%' or '_' in the input from
// broadening the match unexpectedly while remaining fully parameterised.
func escapeLikePattern(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`%`, `\%`,
		`_`, `\_`,
	)
	return replacer.Replace(s)
}

// baselinesSingleConnection queries baselines for a single connection (original behavior)
func baselinesSingleConnection(
	ctx context.Context, pool *pgxpool.Pool,
	connectionID int, connName string, metricName *string,
) (mcp.ToolResponse, error) {
	// metricPattern is the bound parameter for the ILIKE filter. It is nil
	// when no metric_name was supplied, in which case the filter is a no-op.
	// When supplied, the user's text is escaped and wrapped in '%' ... '%'
	// so it matches the fully-qualified stored name as a case-insensitive
	// substring (e.g. "cache_hit_ratio" matches
	// "pg_stat_database.cache_hit_ratio").
	var metricPattern *string
	if metricName != nil {
		p := "%" + escapeLikePattern(*metricName) + "%"
		metricPattern = &p
	}

	query := `
        SELECT metric_name, period_type, day_of_week, hour_of_day,
               mean, stddev, min, max, sample_count, last_calculated
        FROM metric_baselines
        WHERE connection_id = $1
          AND ($2::text IS NULL OR metric_name ILIKE $2 ESCAPE '\')
        ORDER BY metric_name, period_type, day_of_week, hour_of_day
    `

	rows, err := pool.Query(ctx, query, connectionID, metricPattern)
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
	fmt.Fprintf(&sb, "Metric Baselines | Connection: %d | %s\n\n",
		connectionID, metricInfo)

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
		fmt.Fprintf(&sb, "%s\t%s\t%s\t%s\t%.4f\t%.4f\t%.4f\t%.4f\t%d\n",
			metricNameVal,
			periodType,
			formatOptionalInt(dayOfWeek),
			formatOptionalInt(hourOfDay),
			mean,
			stddev,
			minVal,
			maxVal,
			sampleCount)
		rowCount++
	}

	if err := rows.Err(); err != nil {
		return mcp.NewToolError(fmt.Sprintf("Error iterating results: %v", err))
	}

	if rowCount == 0 {
		// When a metric_name filter was supplied but nothing matched, the
		// LLM most likely used a shorthand that does not appear in any
		// stored fully-qualified name. List the available metric names for
		// this connection so it can retry with a correct value.
		if metricName != nil {
			available := availableMetricNamesSingle(ctx, pool, connectionID)
			if len(available) > 0 {
				return mcp.NewToolSuccess(fmt.Sprintf(
					"No metric baselines matched %q for connection %d. "+
						"Stored metric names are fully-qualified (view.metric); "+
						"available metric names for this connection are: %s. "+
						"Retry with one of these (partial, case-insensitive matching is supported).",
					*metricName, connectionID, strings.Join(available, ", ")))
			}
		}
		return mcp.NewToolSuccess(fmt.Sprintf("No metric baselines found for connection %d. Baselines are calculated after sufficient historical data is collected.", connectionID))
	}

	fmt.Fprintf(&sb, "\n(%d baselines)\n", rowCount)

	return mcp.NewToolSuccess(sb.String())
}

// baselinesAllConnections queries baselines across all accessible connections
func baselinesAllConnections(
	ctx context.Context, pool *pgxpool.Pool,
	allConnections bool, accessibleIDs []int, metricName *string,
) (mcp.ToolResponse, error) {
	connFilter, connArgs := buildConnectionFilter("mb.connection_id", allConnections, accessibleIDs)

	// metricPattern is the bound parameter for the ILIKE filter. When
	// supplied, the user's text is escaped and wrapped in '%' ... '%' so it
	// matches the fully-qualified stored name as a case-insensitive
	// substring (see escapeLikePattern).
	var metricPattern *string
	if metricName != nil {
		p := "%" + escapeLikePattern(*metricName) + "%"
		metricPattern = &p
	}

	paramIdx := len(connArgs) + 1
	query := fmt.Sprintf(`
        SELECT mb.connection_id, c.name AS connection_name,
               mb.metric_name, mb.period_type, mb.day_of_week, mb.hour_of_day,
               mb.mean, mb.stddev, mb.min, mb.max, mb.sample_count, mb.last_calculated
        FROM metric_baselines mb
        JOIN connections c ON c.id = mb.connection_id
        WHERE %s
          AND ($%d::text IS NULL OR mb.metric_name ILIKE $%d ESCAPE '\')
        ORDER BY c.name, mb.metric_name, mb.period_type, mb.day_of_week, mb.hour_of_day
    `, connFilter, paramIdx, paramIdx)

	queryArgs := make([]any, 0, len(connArgs)+1)
	queryArgs = append(queryArgs, connArgs...)
	queryArgs = append(queryArgs, metricPattern)

	rows, err := pool.Query(ctx, query, queryArgs...)
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
	fmt.Fprintf(&sb, "Metric Baselines | All accessible connections | %s\n\n", metricInfo)

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

		fmt.Fprintf(&sb, "%d\t%s\t%s\t%s\t%s\t%s\t%.4f\t%.4f\t%.4f\t%.4f\t%d\n",
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
			sampleCount)
		rowCount++
	}

	if err := rows.Err(); err != nil {
		return mcp.NewToolError(fmt.Sprintf("Error iterating results: %v", err))
	}

	if rowCount == 0 {
		// When a metric_name filter was supplied but nothing matched,
		// list the available metric names across the accessible
		// connections so the LLM can retry with a correct value.
		if metricName != nil {
			available := availableMetricNamesMulti(ctx, pool, allConnections, accessibleIDs)
			if len(available) > 0 {
				return mcp.NewToolSuccess(fmt.Sprintf(
					"No metric baselines matched %q across accessible connections. "+
						"Stored metric names are fully-qualified (view.metric); "+
						"available metric names are: %s. "+
						"Retry with one of these (partial, case-insensitive matching is supported).",
					*metricName, strings.Join(available, ", ")))
			}
		}
		return mcp.NewToolSuccess("No metric baselines found across accessible connections. Baselines are calculated after sufficient historical data is collected.")
	}

	fmt.Fprintf(&sb, "\n(%d baselines)\n", rowCount)

	return mcp.NewToolSuccess(sb.String())
}

// availableMetricNamesLimit bounds the number of distinct metric names
// returned in a "no match" helper message.
const availableMetricNamesLimit = 50

// availableMetricNamesSingle returns the distinct metric names that have
// baselines for the given connection, bounded by availableMetricNamesLimit.
// It is used only to build a helpful "no match" message; any query error is
// treated as "no names available" so the caller falls back to the generic
// message rather than surfacing an error.
func availableMetricNamesSingle(ctx context.Context, pool *pgxpool.Pool, connectionID int) []string {
	rows, err := pool.Query(ctx, `
        SELECT DISTINCT metric_name
        FROM metric_baselines
        WHERE connection_id = $1
        ORDER BY metric_name
        LIMIT $2
    `, connectionID, availableMetricNamesLimit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return scanMetricNames(rows)
}

// availableMetricNamesMulti returns the distinct metric names that have
// baselines across the accessible connections, bounded by
// availableMetricNamesLimit. It reuses buildConnectionFilter so the lookup
// respects the same RBAC scoping as the main query. Query errors yield nil.
func availableMetricNamesMulti(ctx context.Context, pool *pgxpool.Pool, allConnections bool, accessibleIDs []int) []string {
	connFilter, connArgs := buildConnectionFilter("mb.connection_id", allConnections, accessibleIDs)
	query := fmt.Sprintf(`
        SELECT DISTINCT mb.metric_name
        FROM metric_baselines mb
        WHERE %s
        ORDER BY mb.metric_name
        LIMIT $%d
    `, connFilter, len(connArgs)+1)

	queryArgs := make([]any, 0, len(connArgs)+1)
	queryArgs = append(queryArgs, connArgs...)
	queryArgs = append(queryArgs, availableMetricNamesLimit)

	rows, err := pool.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return scanMetricNames(rows)
}

// scanMetricNames collects metric_name values from a single-column result
// set. Scan errors abort the collection and return what was gathered so far.
func scanMetricNames(rows interface {
	Next() bool
	Scan(...any) error
}) []string {
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			break
		}
		names = append(names, name)
	}
	return names
}

// formatOptionalInt formats an optional int pointer for TSV output
func formatOptionalInt(i *int) string {
	if i == nil {
		return ""
	}
	return fmt.Sprintf("%d", *i)
}
