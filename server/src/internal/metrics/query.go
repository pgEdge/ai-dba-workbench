/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package metrics

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MetricFilters holds optional dimension filters for metric queries.
type MetricFilters struct {
	DatabaseName string
	SchemaName   string
	TableName    string
}

// MetricDataPoint represents a single time-value pair in a metric series.
type MetricDataPoint struct {
	Time  time.Time `json:"time"`
	Value float64   `json:"value"`
}

// MetricSeries represents a named series of metric data points.
type MetricSeries struct {
	Name   string            `json:"name"`
	Metric string            `json:"metric"`
	Data   []MetricDataPoint `json:"data"`
	Unit   string            `json:"unit"`
}

// MetricBaseline holds aggregated baseline statistics for a metric.
type MetricBaseline struct {
	Metric string  `json:"metric"`
	Mean   float64 `json:"mean"`
	Stddev float64 `json:"stddev"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	P50    float64 `json:"p50"`
	P95    float64 `json:"p95"`
	P99    float64 `json:"p99"`
}

// ValidTimeRanges maps dashboard time range strings to their duration.
var ValidTimeRanges = map[string]time.Duration{
	"1h":  1 * time.Hour,
	"6h":  6 * time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
}

// ParseTimeRange converts a time range string like "1h", "6h", "24h",
// "7d", or "30d" into start and end times. The end time is always now.
func ParseTimeRange(timeRange string) (time.Time, time.Time, error) {
	duration, ok := ValidTimeRanges[timeRange]
	if !ok {
		return time.Time{}, time.Time{},
			fmt.Errorf("invalid time range %q: must be one of 1h, 6h, 24h, 7d, 30d", timeRange)
	}

	now := time.Now().UTC()
	return now.Add(-duration), now, nil
}

// IsMetricColumn determines whether a column represents a numeric metric
// as opposed to a dimension identifier.
func IsMetricColumn(name, dataType string) bool {
	dimensionColumns := map[string]bool{
		"connection_id":    true,
		"collected_at":     true,
		"inserted_at":      true,
		"datid":            true,
		"datname":          true,
		"pid":              true,
		"usesysid":         true,
		"usename":          true,
		"application_name": true,
		"client_addr":      true,
		"client_hostname":  true,
		"client_port":      true,
		"backend_start":    true,
		"xact_start":       true,
		"query_start":      true,
		"state_change":     true,
		"wait_event_type":  true,
		"wait_event":       true,
		"state":            true,
		"backend_xid":      true,
		"backend_xmin":     true,
		"query":            true,
		"backend_type":     true,
		"relid":            true,
		"relname":          true,
		"schemaname":       true,
		"indexrelid":       true,
		"indexrelname":     true,
		"funcid":           true,
		"funcname":         true,
		"queryid":          true,
		"slot_name":        true,
		"plugin":           true,
		"slot_type":        true,
		"sender_host":      true,
		"sender_port":      true,
		"conninfo":         true,
		"status":           true,
		"name":             true,
		"setting":          true,
		"unit":             true,
		"category":         true,
		"short_desc":       true,
		"extra_desc":       true,
		"context":          true,
		"vartype":          true,
		"source":           true,
		"boot_val":         true,
		"reset_val":        true,
		"sourcefile":       true,
		"sourceline":       true,
	}

	if dimensionColumns[name] {
		return false
	}

	// Timestamp types are dimensions
	if strings.Contains(dataType, "timestamp") || dataType == "date" || dataType == "time" {
		return false
	}

	// Text types are typically dimensions
	if dataType == "text" || dataType == "character varying" ||
		dataType == "name" || dataType == "inet" || dataType == "oid" {
		return false
	}

	// Numeric types are metrics
	if dataType == "bigint" || dataType == "integer" || dataType == "smallint" ||
		dataType == "numeric" || dataType == "double precision" || dataType == "real" ||
		dataType == "interval" {
		return true
	}

	return false
}

// IsValidIdentifier checks whether a string is a valid SQL identifier.
func IsValidIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if i == 0 {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_') {
				return false
			}
		} else {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
				(c >= '0' && c <= '9') || c == '_') {
				return false
			}
		}
	}
	return true
}

// QuoteIdentifier quotes a SQL identifier to prevent injection.
func QuoteIdentifier(name string) string {
	escaped := strings.ReplaceAll(name, `"`, `""`)
	return `"` + escaped + `"`
}

// GetProbeMetricColumns discovers numeric metric columns for a probe table
// in the metrics schema. It returns the column names and a map from column
// name to its PostgreSQL data type.
func GetProbeMetricColumns(ctx context.Context, pool *pgxpool.Pool, probeName string) ([]string, map[string]string, error) {
	query := `
        SELECT column_name, data_type
        FROM information_schema.columns
        WHERE table_schema = 'metrics'
            AND table_name = $1
        ORDER BY ordinal_position
    `

	rows, err := pool.Query(ctx, query, probeName)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var metricCols []string
	colTypes := make(map[string]string)
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			return nil, nil, err
		}
		if IsMetricColumn(name, dataType) {
			metricCols = append(metricCols, name)
			colTypes[name] = dataType
		}
	}

	return metricCols, colTypes, rows.Err()
}

// GetAggSelectCols returns aggregated SELECT expressions with quoted
// identifiers to prevent SQL injection.
func GetAggSelectCols(metricCols []string, aggregation string) []string {
	var cols []string
	for _, col := range metricCols {
		quotedCol := QuoteIdentifier(col)
		if aggregation == "last" {
			cols = append(cols,
				fmt.Sprintf("(array_agg(%s ORDER BY collected_at DESC))[1] AS %s",
					quotedCol, quotedCol))
		} else {
			cols = append(cols,
				fmt.Sprintf("%s(%s) AS %s", aggregation, quotedCol, quotedCol))
		}
	}
	return cols
}

// GetQuotedSelectCols returns column names with quoted identifiers for
// use in SELECT clauses.
func GetQuotedSelectCols(metricCols []string) []string {
	var cols []string
	for _, col := range metricCols {
		cols = append(cols, QuoteIdentifier(col))
	}
	return cols
}

// GetCoalescedSelectCols returns column expressions qualified with a
// table alias, wrapped in COALESCE to replace NULLs with a zero value.
// For interval columns the default is '0 seconds'::interval; for all
// other numeric types the default is 0.  This ensures LEFT JOIN gaps
// produce zero values instead of NULLs.
func GetCoalescedSelectCols(metricCols []string, tableAlias string, colTypes map[string]string) []string {
	var cols []string
	for _, col := range metricCols {
		qualified := tableAlias + "." + QuoteIdentifier(col)
		defaultVal := "0"
		if colTypes[col] == "interval" {
			defaultVal = "'0 seconds'::interval"
		}
		cols = append(cols, "COALESCE("+qualified+", "+defaultVal+") AS "+QuoteIdentifier(col))
	}
	return cols
}

// GetQualifiedSelectCols returns column expressions qualified with a
// table alias. Unlike GetCoalescedSelectCols, NULL values from LEFT JOIN
// gaps pass through so the caller can apply LOCF (Last Observation
// Carried Forward).
func GetQualifiedSelectCols(metricCols []string, tableAlias string) []string {
	var cols []string
	for _, col := range metricCols {
		cols = append(cols, tableAlias+"."+QuoteIdentifier(col))
	}
	return cols
}

// BuildMetricsQuery constructs a time-bucketed aggregation SQL query for
// the given probe, columns, connection, time range, and filters. The
// colTypes map provides each column's PostgreSQL data type so that the
// aggregation default can match (e.g. interval vs numeric). NULL values
// from LEFT JOIN gaps are preserved so the caller can apply LOCF.
func BuildMetricsQuery(
	probeName string,
	metricCols []string,
	colTypes map[string]string,
	connectionID int,
	timeStart, timeEnd time.Time,
	buckets int,
	aggregation string,
	filters MetricFilters,
) (string, []any, error) {
	// Calculate bucket width
	duration := timeEnd.Sub(timeStart)
	bucketWidth := duration / time.Duration(buckets)
	if bucketWidth < time.Second {
		bucketWidth = time.Second
	}

	// Build WHERE clause
	var whereClauses []string
	queryArgs := []any{
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
	if filters.DatabaseName != "" {
		whereClauses = append(whereClauses,
			fmt.Sprintf("datname = $%d", argNum))
		queryArgs = append(queryArgs, filters.DatabaseName)
		argNum++
	}

	if filters.SchemaName != "" {
		whereClauses = append(whereClauses,
			fmt.Sprintf("schemaname = $%d", argNum))
		queryArgs = append(queryArgs, filters.SchemaName)
		argNum++
	}

	if filters.TableName != "" {
		whereClauses = append(whereClauses,
			fmt.Sprintf("relname = $%d", argNum))
		queryArgs = append(queryArgs, filters.TableName)
	}

	query := fmt.Sprintf(`
        WITH data_buckets AS (
            SELECT
                date_bin($1::interval, collected_at, $3) AS bucket_time,
                %s
            FROM metrics.%s
            WHERE %s
            GROUP BY date_bin($1::interval, collected_at, $3)
        ),
        all_buckets AS (
            SELECT generate_series($3::timestamptz, $4::timestamptz, $1::interval) AS bucket_time
        )
        SELECT
            all_buckets.bucket_time,
            %s
        FROM all_buckets
        LEFT JOIN data_buckets ON all_buckets.bucket_time = data_buckets.bucket_time
        ORDER BY all_buckets.bucket_time
    `,
		strings.Join(GetAggSelectCols(metricCols, aggregation), ", "),
		QuoteIdentifier(probeName),
		strings.Join(whereClauses, " AND "),
		strings.Join(GetQualifiedSelectCols(metricCols, "data_buckets"), ", "),
	)

	return query, queryArgs, nil
}

// QueryTimeSeries executes a metrics query and returns the results as
// MetricSeries slices. Each numeric column becomes its own series. When
// multiple connection IDs are provided, results are combined.
func QueryTimeSeries(
	ctx context.Context,
	pool *pgxpool.Pool,
	probeName string,
	connectionIDs []int,
	timeRange string,
	filters MetricFilters,
	buckets int,
	aggregation string,
	requestedMetrics []string,
) ([]MetricSeries, error) {
	if !IsValidIdentifier(probeName) {
		return nil, fmt.Errorf("invalid probe name %q", probeName)
	}

	timeStart, timeEnd, err := ParseTimeRange(timeRange)
	if err != nil {
		return nil, err
	}

	// Verify probe exists
	var count int
	existsQuery := `
        SELECT COUNT(*) FROM information_schema.tables
        WHERE table_schema = 'metrics'
            AND table_name = $1
            AND table_type = 'BASE TABLE'
    `
	if err := pool.QueryRow(ctx, existsQuery, probeName).Scan(&count); err != nil {
		return nil, fmt.Errorf("failed to verify probe: %w", err)
	}
	if count == 0 {
		return nil, fmt.Errorf("probe %q not found", probeName)
	}

	// Discover metric columns
	metricCols, colTypes, err := GetProbeMetricColumns(ctx, pool, probeName)
	if err != nil {
		return nil, fmt.Errorf("failed to get probe columns: %w", err)
	}

	// Filter to requested metrics if specified
	if len(requestedMetrics) > 0 {
		available := make(map[string]bool, len(metricCols))
		for _, col := range metricCols {
			available[col] = true
		}

		var filtered []string
		for _, m := range requestedMetrics {
			m = strings.TrimSpace(m)
			if !IsValidIdentifier(m) {
				return nil, fmt.Errorf("invalid metric name %q", m)
			}
			if !available[m] {
				return nil, fmt.Errorf("metric %q not found in probe %q", m, probeName)
			}
			filtered = append(filtered, m)
		}
		metricCols = filtered
	}

	if len(metricCols) == 0 {
		return nil, fmt.Errorf("no numeric metrics found in probe %q", probeName)
	}

	// Collect data across all connections
	type seriesKey struct {
		metric       string
		connectionID int
	}
	dataMap := make(map[seriesKey][]MetricDataPoint)

	// Track last known value per metric column for LOCF
	lastKnown := make(map[string]float64)

	for _, connID := range connectionIDs {
		query, queryArgs, err := BuildMetricsQuery(
			probeName, metricCols, colTypes, connID, timeStart, timeEnd,
			buckets, aggregation, filters)
		if err != nil {
			return nil, fmt.Errorf("failed to build query: %w", err)
		}

		rows, err := pool.Query(ctx, query, queryArgs...)
		if err != nil {
			return nil, fmt.Errorf("failed to query metrics for connection %d: %w", connID, err)
		}

		for rows.Next() {
			values := make([]any, len(metricCols)+1)
			valuePtrs := make([]any, len(metricCols)+1)
			var bucketTime time.Time
			valuePtrs[0] = &bucketTime
			for i := range metricCols {
				var v any
				values[i+1] = &v
				valuePtrs[i+1] = &values[i+1]
			}

			if err := rows.Scan(valuePtrs...); err != nil {
				rows.Close()
				return nil, fmt.Errorf("failed to scan row: %w", err)
			}

			for i, col := range metricCols {
				val, ok := toFloat64(values[i+1])
				if !ok {
					// Empty bucket from LEFT JOIN gap - apply LOCF
					lkKey := fmt.Sprintf("%d:%s", connID, col)
					if prev, exists := lastKnown[lkKey]; exists {
						val = prev
					} else {
						continue
					}
				} else {
					lkKey := fmt.Sprintf("%d:%s", connID, col)
					lastKnown[lkKey] = val
				}
				key := seriesKey{metric: col, connectionID: connID}
				dataMap[key] = append(dataMap[key], MetricDataPoint{
					Time:  bucketTime,
					Value: val,
				})
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("error iterating results: %w", err)
		}
	}

	// Build result series
	var result []MetricSeries
	for _, col := range metricCols {
		for _, connID := range connectionIDs {
			key := seriesKey{metric: col, connectionID: connID}
			data := dataMap[key]
			if data == nil {
				data = []MetricDataPoint{}
			}

			name := col
			if len(connectionIDs) > 1 {
				name = fmt.Sprintf("%s (conn %d)", col, connID)
			}

			result = append(result, MetricSeries{
				Name:   name,
				Metric: col,
				Data:   data,
				Unit:   "",
			})
		}
	}

	return result, nil
}

// QueryBaselines retrieves aggregated baseline statistics for the given
// connection and probe metrics. It uses the 'overall' period type when
// available and approximates percentiles from mean and stddev.
func QueryBaselines(
	ctx context.Context,
	pool *pgxpool.Pool,
	connectionID int,
	probeName string,
	requestedMetrics []string,
) ([]MetricBaseline, error) {
	// Build the metric filter
	var metricFilter string
	var queryArgs []any
	queryArgs = append(queryArgs, connectionID)

	if probeName != "" {
		// Filter to metrics that start with the probe name prefix
		metricFilter = " AND metric_name LIKE $2 || '.%'"
		queryArgs = append(queryArgs, probeName)
	}

	// Query baselines, preferring 'overall' period type
	query := fmt.Sprintf(`
        SELECT metric_name, mean, stddev, min, max
        FROM metric_baselines
        WHERE connection_id = $1
            AND period_type = 'overall'
            %s
        ORDER BY metric_name
    `, metricFilter)

	rows, err := pool.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to query baselines: %w", err)
	}
	defer rows.Close()

	baselineMap := make(map[string]*MetricBaseline)
	for rows.Next() {
		var metricName string
		var mean, stddev, minVal, maxVal float64
		if err := rows.Scan(&metricName, &mean, &stddev, &minVal, &maxVal); err != nil {
			return nil, fmt.Errorf("failed to scan baseline row: %w", err)
		}

		baselineMap[metricName] = &MetricBaseline{
			Metric: metricName,
			Mean:   mean,
			Stddev: stddev,
			Min:    minVal,
			Max:    maxVal,
			P50:    mean,
			P95:    mean + 2*stddev,
			P99:    mean + 3*stddev,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating baselines: %w", err)
	}

	// If no 'overall' baselines found, aggregate across all periods
	if len(baselineMap) == 0 {
		aggQuery := fmt.Sprintf(`
            SELECT metric_name,
                   AVG(mean) AS mean,
                   AVG(stddev) AS stddev,
                   MIN(min) AS min,
                   MAX(max) AS max
            FROM metric_baselines
            WHERE connection_id = $1
                %s
            GROUP BY metric_name
            ORDER BY metric_name
        `, metricFilter)

		rows2, err := pool.Query(ctx, aggQuery, queryArgs...)
		if err != nil {
			return nil, fmt.Errorf("failed to query aggregated baselines: %w", err)
		}
		defer rows2.Close()

		for rows2.Next() {
			var metricName string
			var mean, stddev, minVal, maxVal float64
			if err := rows2.Scan(&metricName, &mean, &stddev, &minVal, &maxVal); err != nil {
				return nil, fmt.Errorf("failed to scan aggregated baseline: %w", err)
			}

			baselineMap[metricName] = &MetricBaseline{
				Metric: metricName,
				Mean:   mean,
				Stddev: stddev,
				Min:    minVal,
				Max:    maxVal,
				P50:    mean,
				P95:    mean + 2*stddev,
				P99:    mean + 3*stddev,
			}
		}
		if err := rows2.Err(); err != nil {
			return nil, fmt.Errorf("error iterating aggregated baselines: %w", err)
		}
	}

	// Filter to requested metrics if specified
	var result []MetricBaseline
	if len(requestedMetrics) > 0 {
		for _, m := range requestedMetrics {
			if bl, ok := baselineMap[m]; ok {
				result = append(result, *bl)
			}
		}
	} else {
		for _, bl := range baselineMap {
			result = append(result, *bl)
		}
	}

	if result == nil {
		result = []MetricBaseline{}
	}

	return result, nil
}

// toFloat64 converts a scanned database value to float64. It returns
// false when the value cannot be converted.
func toFloat64(v any) (float64, bool) {
	if v == nil {
		return 0, false
	}

	// Dereference pointer
	if ptr, ok := v.(*any); ok {
		if ptr == nil || *ptr == nil {
			return 0, false
		}
		return toFloat64(*ptr)
	}

	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int64:
		return float64(val), true
	case int32:
		return float64(val), true
	case int:
		return float64(val), true
	case int16:
		return float64(val), true
	case int8:
		return float64(val), true
	case uint64:
		return float64(val), true
	case uint32:
		return float64(val), true
	case uint16:
		return float64(val), true
	case uint8:
		return float64(val), true
	case pgtype.Numeric:
		f, err := val.Float64Value()
		if err != nil {
			return 0, false
		}
		if !f.Valid {
			return 0, false
		}
		return f.Float64, true
	case *pgtype.Numeric:
		if val == nil {
			return 0, false
		}
		f, err := val.Float64Value()
		if err != nil {
			return 0, false
		}
		if !f.Valid {
			return 0, false
		}
		return f.Float64, true
	case pgtype.Interval:
		if !val.Valid {
			// NULL interval means no lag reported; treat as zero
			return 0, true
		}
		// Convert interval to total seconds
		return float64(val.Microseconds) / 1_000_000.0, true
	case *pgtype.Interval:
		if val == nil {
			return 0, true
		}
		if !val.Valid {
			// NULL interval means no lag reported; treat as zero
			return 0, true
		}
		return float64(val.Microseconds) / 1_000_000.0, true
	default:
		return 0, false
	}
}
