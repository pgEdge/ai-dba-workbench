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
	"math"
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
func GetMetricBaselinesTool(pool *pgxpool.Pool, rbacChecker *auth.RBACChecker, visibilityLister auth.ConnectionVisibilityLister, record DenialRecorder) Tool {
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
- limit: (optional) Maximum rows to return (default: 100, max: 300).
- offset: (optional) Number of rows to skip, for paging (default: 0).
</parameters>

<paging>
Each metric has up to 32 baselines (one 'all', 24 hourly and 7 daily), so a
connection with many metrics has hundreds of rows. The line after the title
states how many baselines match and how many are shown; when more exist it
names the offset to pass for the next page. Filter by metric_name to narrow
the result instead of paging through every metric.
</paging>

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
Returns a count line, then TSV data with:
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
- get_metric_baselines(connection_id=5, offset=100) - the second page of 100 baselines for a connection
</examples>`,
			CompactDescription: `Query statistical baselines for metrics used in anomaly detection. Omit connection_id to see baselines across all accessible connections. Returns mean, stddev, min, max, and sample count. Filter by metric_name (partial, case-insensitive match against fully-qualified view.metric names, e.g. pg_stat_database.cache_hit_ratio). Returns 100 rows by default (limit, max 300); the line after the title gives the total and the offset for the next page.`,
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
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of baselines to return. Default: 100, max: 300.",
						"minimum":     1,
						"maximum":     baselinesMaxLimit,
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "Number of baselines to skip, for paging. Default: 0. The line after the title names the offset for the next page.",
						"minimum":     0,
					},
				},
				Required: []string{},
			},
		},
		Handler: func(args map[string]any) (mcp.ToolResponse, error) {
			if pool == nil {
				return mcp.NewToolError("Datastore not configured. The get_metric_baselines tool requires a datastore connection.")
			}

			page, errMsg := parseBaselinesPage(args)
			if errMsg != "" {
				return mcp.NewToolError(errMsg)
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
			if hasConnectionID(args) {
				var valid bool
				connectionID, valid = connectionIDValue(args["connection_id"])
				if !valid {
					return mcp.NewToolError("Invalid 'connection_id' parameter: must be a positive integer. Use list_connections to find available connection IDs.")
				}
				singleConnection = true

				// RBAC: check access before looking the connection up, so
				// missing and forbidden IDs give the same response.
				name, resp := resolveAccessibleConnection(ctx, pool, rbacChecker, visibilityLister, record, deniedActionMetricsRead, connectionID)
				if resp != nil {
					return *resp, nil
				}
				connName = name
			}

			// Build accessible connection filter for multi-connection mode.
			// VisibleConnectionIDs honors ownership and sharing in addition
			// to group/token grants, and returns an explicit
			// allConnections flag so callers cannot confuse "full access"
			// with "no grants".
			var accessibleIDs []int
			allConnections := true
			if !singleConnection {
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
				return baselinesSingleConnection(ctx, pool, connectionID, connName, metricName, page)
			}
			return baselinesAllConnections(ctx, pool, allConnections, accessibleIDs, metricName, page)
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

// Row limits for one page of get_metric_baselines output. A default page
// runs to about 10,000 characters and a maximum page stays under 50,000,
// above which some MCP clients move a result out of the model's view.
const (
	baselinesDefaultLimit = 100
	baselinesMaxLimit     = 300
)

// baselinesPage is one requested page of baseline rows.
type baselinesPage struct {
	limit  int
	offset int
}

// parseBaselinesPage reads the optional limit and offset arguments. It
// returns the error text for the caller when either is invalid, and an
// empty string otherwise.
func parseBaselinesPage(args map[string]any) (baselinesPage, string) {
	page := baselinesPage{limit: baselinesDefaultLimit}
	if v, ok := args["limit"]; ok && v != nil {
		n, ok := integerValue(v)
		if !ok || n < 1 || n > baselinesMaxLimit {
			return page, fmt.Sprintf("Invalid 'limit' parameter: must be an integer between 1 and %d", baselinesMaxLimit)
		}
		page.limit = int(n)
	}
	if v, ok := args["offset"]; ok && v != nil {
		n, ok := integerValue(v)
		if !ok || n < 0 || n > math.MaxInt32 {
			return page, fmt.Sprintf("Invalid 'offset' parameter: must be an integer of 0 or more (at most %d)", math.MaxInt32)
		}
		page.offset = int(n)
	}
	return page, ""
}

// baselinesCountLine describes which of the total matching rows a page
// shows, naming the offset of the next page when more rows exist.
func baselinesCountLine(total, shown int, page baselinesPage) string {
	switch {
	case page.offset == 0 && shown == total:
		return fmt.Sprintf("Showing all %d baselines", total)
	case page.offset+shown < total:
		return fmt.Sprintf("Showing rows %d-%d of %d baselines (more available - use offset=%d for next page)",
			page.offset+1, page.offset+shown, total, page.offset+shown)
	default:
		return fmt.Sprintf("Showing rows %d-%d of %d baselines (last page)",
			page.offset+1, page.offset+shown, total)
	}
}

// baselinesPastEnd reports whether rows match the filter in fromWhere
// even though the page at offset came back empty, and if so returns the
// message telling the caller which offsets hold rows.
func baselinesPastEnd(ctx context.Context, pool *pgxpool.Pool, fromWhere string, args []any, offset int) (string, error) {
	if offset == 0 {
		return "", nil
	}
	var total int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) "+fromWhere, args...).Scan(&total); err != nil {
		return "", err
	}
	if total == 0 {
		return "", nil
	}
	return fmt.Sprintf("No rows at offset %d: %d baselines match, so use an offset below %d.",
		offset, total, total), nil
}

// baselineMetricPattern returns the bound parameter for the metric_name
// ILIKE filter: nil when no metric_name was supplied, in which case the
// filter is a no-op, and otherwise the user's text escaped and wrapped in
// '%' ... '%' so it matches the fully-qualified stored name as a
// case-insensitive substring (e.g. "cache_hit_ratio" matches
// "pg_stat_database.cache_hit_ratio").
func baselineMetricPattern(metricName *string) *string {
	if metricName == nil {
		return nil
	}
	p := "%" + escapeLikePattern(*metricName) + "%"
	return &p
}

// baselinesSingleConnection queries one page of baselines for a single connection
func baselinesSingleConnection(
	ctx context.Context, pool *pgxpool.Pool,
	connectionID int, connName string, metricName *string, page baselinesPage,
) (mcp.ToolResponse, error) {
	fromWhere := `
        FROM metric_baselines
        WHERE connection_id = $1
          AND ($2::text IS NULL OR metric_name ILIKE $2 ESCAPE '\')`
	filterArgs := []any{connectionID, baselineMetricPattern(metricName)}

	// id breaks ties between rows that differ only in database_name, so
	// consecutive pages neither repeat nor skip a row.
	query := `
        SELECT metric_name, period_type, day_of_week, hour_of_day,
               mean, stddev, min, max, sample_count, last_calculated,
               COUNT(*) OVER () AS total` + fromWhere + `
        ORDER BY metric_name, period_type, day_of_week, hour_of_day, id
        LIMIT $3 OFFSET $4
    `

	queryArgs := append(append([]any{}, filterArgs...), page.limit, page.offset)

	rows, err := pool.Query(ctx, query, queryArgs...)
	if err != nil {
		return mcp.NewToolError(fmt.Sprintf("Failed to query metric baselines: %v", err))
	}
	defer rows.Close()

	// Data rows
	var data strings.Builder
	rowCount := 0
	total := 0
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
			&mean, &stddev, &minVal, &maxVal, &sampleCount, &lastCalculated, &total); err != nil {
			return mcp.NewToolError(fmt.Sprintf("Failed to scan row: %v", err))
		}

		// Format row
		fmt.Fprintf(&data, "%s\t%s\t%s\t%s\t%.4f\t%.4f\t%.4f\t%.4f\t%d\n",
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
		msg, err := baselinesPastEnd(ctx, pool, fromWhere, filterArgs, page.offset)
		if err != nil {
			return mcp.NewToolError(fmt.Sprintf("Failed to count metric baselines: %v", err))
		}
		if msg != "" {
			return mcp.NewToolSuccess(msg)
		}
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

	// Build TSV output
	var sb strings.Builder
	metricInfo := "all metrics"
	if metricName != nil {
		metricInfo = fmt.Sprintf("metric: %s", *metricName)
	}
	fmt.Fprintf(&sb, "Metric Baselines | Connection: %d | %s\n%s\n\n",
		connectionID, metricInfo, baselinesCountLine(total, rowCount, page))

	// Header
	sb.WriteString("metric_name\tperiod_type\tday_of_week\thour_of_day\tmean\tstddev\tmin\tmax\tsample_count\n")
	sb.WriteString(data.String())

	return mcp.NewToolSuccess(sb.String())
}

// baselinesAllConnections queries one page of baselines across all accessible connections
func baselinesAllConnections(
	ctx context.Context, pool *pgxpool.Pool,
	allConnections bool, accessibleIDs []int, metricName *string, page baselinesPage,
) (mcp.ToolResponse, error) {
	connFilter, connArgs := buildConnectionFilter("mb.connection_id", allConnections, accessibleIDs)

	paramIdx := len(connArgs) + 1
	fromWhere := fmt.Sprintf(`
        FROM metric_baselines mb
        JOIN connections c ON c.id = mb.connection_id AND c.deleted_at IS NULL
        WHERE %s
          AND ($%d::text IS NULL OR mb.metric_name ILIKE $%d ESCAPE '\')`,
		connFilter, paramIdx, paramIdx)

	filterArgs := make([]any, 0, len(connArgs)+1)
	filterArgs = append(filterArgs, connArgs...)
	filterArgs = append(filterArgs, baselineMetricPattern(metricName))

	// connection_id and id break ties between connections sharing a name
	// and between rows that differ only in database_name, so consecutive
	// pages neither repeat nor skip a row.
	query := fmt.Sprintf(`
        SELECT mb.connection_id, c.name AS connection_name,
               mb.metric_name, mb.period_type, mb.day_of_week, mb.hour_of_day,
               mb.mean, mb.stddev, mb.min, mb.max, mb.sample_count, mb.last_calculated,
               COUNT(*) OVER () AS total%s
        ORDER BY c.name, mb.connection_id, mb.metric_name, mb.period_type,
                 mb.day_of_week, mb.hour_of_day, mb.id
        LIMIT $%d OFFSET $%d
    `, fromWhere, paramIdx+1, paramIdx+2)

	queryArgs := append(append([]any{}, filterArgs...), page.limit, page.offset)

	rows, err := pool.Query(ctx, query, queryArgs...)
	if err != nil {
		return mcp.NewToolError(fmt.Sprintf("Failed to query metric baselines: %v", err))
	}
	defer rows.Close()

	// Data rows
	var data strings.Builder
	rowCount := 0
	total := 0
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
			&mean, &stddev, &minVal, &maxVal, &sampleCount, &lastCalculated, &total); err != nil {
			return mcp.NewToolError(fmt.Sprintf("Failed to scan row: %v", err))
		}

		fmt.Fprintf(&data, "%d\t%s\t%s\t%s\t%s\t%s\t%.4f\t%.4f\t%.4f\t%.4f\t%d\n",
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
		msg, err := baselinesPastEnd(ctx, pool, fromWhere, filterArgs, page.offset)
		if err != nil {
			return mcp.NewToolError(fmt.Sprintf("Failed to count metric baselines: %v", err))
		}
		if msg != "" {
			return mcp.NewToolSuccess(msg)
		}
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

	// Build TSV output
	var sb strings.Builder
	metricInfo := "all metrics"
	if metricName != nil {
		metricInfo = fmt.Sprintf("metric: %s", *metricName)
	}
	fmt.Fprintf(&sb, "Metric Baselines | All accessible connections | %s\n%s\n\n",
		metricInfo, baselinesCountLine(total, rowCount, page))

	// Header - includes connection columns
	sb.WriteString("connection_id\tconnection_name\tmetric_name\tperiod_type\tday_of_week\thour_of_day\tmean\tstddev\tmin\tmax\tsample_count\n")
	sb.WriteString(data.String())

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
