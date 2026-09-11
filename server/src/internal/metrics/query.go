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
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DerivedMetricKind enumerates the computed metric types the generic
// time-series query path supports in addition to raw columns.
type DerivedMetricKind int

const (
	// DerivedPerSec is a per-second rate computed from the delta of a
	// cumulative counter column between consecutive samples.
	DerivedPerSec DerivedMetricKind = iota
	// DerivedDeadTupleRatio is the dead-tuple percentage computed from the
	// n_live_tup and n_dead_tup columns.
	DerivedDeadTupleRatio
)

// DerivedMetric describes a single computed metric to include in a query.
type DerivedMetric struct {
	// OutputName is the metric name returned to the client, e.g.
	// "seq_scan_per_sec" or "dead_tuple_ratio".
	OutputName string
	// BaseColumn is the source counter column for a per-second rate. It is
	// empty for DerivedDeadTupleRatio.
	BaseColumn string
	// Kind selects how the metric is computed.
	Kind DerivedMetricKind
}

// MetricFilters holds optional dimension filters for metric queries.
type MetricFilters struct {
	DatabaseName   string
	DatabaseColumn string // Resolved column name: "datname", "database_name", or ""
	SchemaName     string
	TableName      string
	IndexName      string
}

// maxLatestRowLimit bounds the number of rows a latest-row query may
// return, mirroring the bound-checking style applied to bucket counts.
const maxLatestRowLimit = 100

// latestRowInternalColumns lists bookkeeping columns excluded from
// latest-row results because callers only need the collected metrics
// and their dimension keys, not internal storage fields.
var latestRowInternalColumns = map[string]bool{
	"connection_id": true,
	"collected_at":  true,
	"inserted_at":   true,
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
		"database_name":    true,
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

// IsEntityKeyColumn reports whether a probe output column is an
// entity-key (identity/dimension) column that identifies a distinct
// monitored entity, as opposed to a metric value or an internal
// bookkeeping column. Entity keys are the text/name-typed dimension
// columns such as schemaname, relname, indexrelname, database_name,
// and datname.
//
// Text type alone is not sufficient: some value columns are text yet
// vary over time for a fixed entity. This codebase renders a raw
// numeric value column as a human-readable string in a companion
// column whose name ends in "_pretty" (e.g. table_size_pretty for
// table_size, index_size_pretty for index_size, via pg_size_pretty).
// Such columns are values, not identity, so treating them as entity
// keys would fragment one real entity into several by its changing
// rendered size and defeat any DISTINCT ON reduction. They are
// therefore excluded here even though they are text. No genuine
// identity column in the schema ends in "_pretty", so the exclusion
// is a pure bug fix that changes nothing for existing probes.
func IsEntityKeyColumn(name, dataType string) bool {
	if latestRowInternalColumns[name] {
		return false
	}
	if strings.HasSuffix(name, "_pretty") {
		return false
	}
	switch dataType {
	case "text", "character varying", "name":
		return true
	default:
		return false
	}
}

// EntityKeyColumns returns the entity-key (identity/dimension) columns
// among cols, preserving their input order. It is the single source of
// truth shared by every latest-row path so that the DISTINCT ON entity
// grouping never drifts between call sites.
func EntityKeyColumns(cols []string, colTypes map[string]string) []string {
	var keys []string
	for _, col := range cols {
		if IsEntityKeyColumn(col, colTypes[col]) {
			keys = append(keys, col)
		}
	}
	return keys
}

// IsValidIdentifier checks whether a string is a valid SQL identifier.
func IsValidIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if i == 0 {
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && c != '_' {
				return false
			}
		} else {
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') &&
				(c < '0' || c > '9') && c != '_' {
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

// ResolveDatabaseColumn discovers which database-name column exists in a
// probe table. It prefers "database_name" and falls back to "datname".
// If neither column exists, it returns an empty string.
func ResolveDatabaseColumn(ctx context.Context, pool *pgxpool.Pool, probeName string) (string, error) {
	query := `
        SELECT column_name
        FROM information_schema.columns
        WHERE table_schema = 'metrics'
            AND table_name = $1
            AND column_name IN ('datname', 'database_name')
        ORDER BY CASE WHEN column_name = 'database_name' THEN 0 ELSE 1 END
        LIMIT 1
    `

	var col string
	err := pool.QueryRow(ctx, query, probeName).Scan(&col)
	if err != nil {
		// pgx returns ErrNoRows when no column matches; treat as
		// "table has no database column".
		if err.Error() == "no rows in result set" {
			return "", nil
		}
		return "", err
	}

	return col, nil
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

	whereSQL, queryArgs := metricQueryBase(
		connectionID, timeStart, timeEnd, bucketWidth, filters)

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
            SELECT bucket_time
            FROM generate_series($3::timestamptz, $4::timestamptz, $1::interval) AS g(bucket_time)
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
		whereSQL,
		strings.Join(GetQualifiedSelectCols(metricCols, "data_buckets"), ", "),
	)

	return query, queryArgs, nil
}

// metricQueryBase builds the shared WHERE clause and the leading query
// arguments used by both the raw-column and derived-metric query builders.
// The returned args are, in order: the bucket interval string, the
// connection ID, the start time, the end time, and then any filter values.
// The WHERE clause references $2, $3, $4 and any filter placeholders from $5.
func metricQueryBase(
	connectionID int,
	timeStart, timeEnd time.Time,
	bucketWidth time.Duration,
	filters MetricFilters,
) (string, []any) {
	queryArgs := []any{
		fmt.Sprintf("%d seconds", int(bucketWidth.Seconds())),
		connectionID,
		timeStart,
		timeEnd,
	}
	argNum := 5

	whereClauses := []string{
		"connection_id = $2",
		"collected_at >= $3",
		"collected_at <= $4",
	}

	if filters.DatabaseName != "" && filters.DatabaseColumn != "" {
		whereClauses = append(whereClauses,
			fmt.Sprintf("%s = $%d", QuoteIdentifier(filters.DatabaseColumn), argNum))
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
		argNum++
	}

	// IndexName filters on indexrelname, mirroring the SchemaName/TableName
	// filters above. Like those, it applies no probe-column-existence check:
	// if the probe table has no indexrelname column the query simply fails at
	// execution time, exactly as an unsupported schemaname/relname filter
	// would. This keeps the validation semantics identical across dimensions.
	if filters.IndexName != "" {
		whereClauses = append(whereClauses,
			fmt.Sprintf("indexrelname = $%d", argNum))
		queryArgs = append(queryArgs, filters.IndexName)
		// No argNum++ here: IndexName is the last filter. A new filter
		// added below must add argNum++ above first.
	}

	return strings.Join(whereClauses, " AND "), queryArgs
}

// rateAggExpr builds the bucket-level aggregation expression for one
// per-second rate column. The inner per-sample rate is exposed as rate_<idx>
// in the rate_samples CTE. NULL per-sample rates (counter resets or invalid
// elapsed times) are ignored by the standard aggregates; for "last" they are
// filtered out explicitly so a reset never becomes the reported value.
func rateAggExpr(aggregation string, idx int, outputName string) string {
	alias := QuoteIdentifier(outputName)
	if aggregation == "last" {
		return fmt.Sprintf(
			"(array_agg(rate_%d ORDER BY collected_at DESC) "+
				"FILTER (WHERE rate_%d IS NOT NULL))[1] AS %s",
			idx, idx, alias)
	}
	return fmt.Sprintf("%s(rate_%d) AS %s", aggregation, idx, alias)
}

// ratioTupleExpr builds the bucket-level aggregation expression for one
// tuple-count column feeding the dead-tuple ratio. Only "last" needs
// distinct handling, mirroring rateAggExpr: it picks the latest sample's
// value in the bucket so the ratio reflects the most recent live/dead
// counts. Every other aggregation collapses the bucket with SUM, matching
// the prior behavior.
func ratioTupleExpr(aggregation, column string) string {
	q := QuoteIdentifier(column)
	if aggregation == "last" {
		return fmt.Sprintf("(array_agg(%s ORDER BY collected_at DESC))[1]", q)
	}
	return fmt.Sprintf("SUM(%s)", q)
}

// BuildDerivedMetricsQuery constructs a time-bucketed SQL query for the
// derived metrics (per-second rates and the dead-tuple ratio) of a probe.
// It shares the same bucketing, gap-filling (generate_series), filtering,
// and argument layout as BuildMetricsQuery, so the caller can scan and apply
// LOCF identically. Output columns follow the order of the derived slice.
func BuildDerivedMetricsQuery(
	probeName string,
	derived []DerivedMetric,
	connectionID int,
	timeStart, timeEnd time.Time,
	buckets int,
	aggregation string,
	filters MetricFilters,
) (string, []any, error) {
	if len(derived) == 0 {
		return "", nil, fmt.Errorf("no derived metrics requested")
	}

	duration := timeEnd.Sub(timeStart)
	bucketWidth := duration / time.Duration(buckets)
	if bucketWidth < time.Second {
		bucketWidth = time.Second
	}

	whereSQL, queryArgs := metricQueryBase(
		connectionID, timeStart, timeEnd, bucketWidth, filters)

	var perSec []DerivedMetric
	hasRatio := false
	for _, d := range derived {
		switch d.Kind {
		case DerivedPerSec:
			perSec = append(perSec, d)
		case DerivedDeadTupleRatio:
			hasRatio = true
		default:
			return "", nil, fmt.Errorf(
				"unknown derived metric kind for %q", d.OutputName)
		}
	}

	var ctes []string
	var joins []string

	if len(perSec) > 0 {
		var innerCols []string
		var sampleCols []string
		var bucketCols []string
		for i, d := range perSec {
			qb := QuoteIdentifier(d.BaseColumn)
			innerCols = append(innerCols,
				fmt.Sprintf("SUM(%s) AS total_%d", qb, i),
				fmt.Sprintf(
					"LAG(SUM(%s)) OVER (ORDER BY collected_at) AS prev_%d",
					qb, i))
			// Discard negative deltas (a counter reset from pg_stat_reset()
			// or a server restart) and non-positive elapsed times (duplicate
			// or out-of-order samples) so neither yields a bogus rate; such
			// rows become NULL and are dropped by the bucket aggregate.
			sampleCols = append(sampleCols, fmt.Sprintf(
				"CASE WHEN (total_%d - prev_%d) >= 0 AND elapsed_sec > 0 "+
					"THEN (total_%d - prev_%d)::float / elapsed_sec "+
					"END AS rate_%d", i, i, i, i, i))
			bucketCols = append(bucketCols,
				rateAggExpr(aggregation, i, d.OutputName))
		}

		ctes = append(ctes, fmt.Sprintf(`
        rate_samples AS (
            SELECT
                collected_at,
                %s
            FROM (
                SELECT
                    collected_at,
                    %s,
                    EXTRACT(EPOCH FROM collected_at
                        - LAG(collected_at) OVER (ORDER BY collected_at)
                    ) AS elapsed_sec
                FROM metrics.%s
                WHERE %s
                GROUP BY collected_at
            ) samples
        )`,
			strings.Join(sampleCols, ",\n                "),
			strings.Join(innerCols, ",\n                    "),
			QuoteIdentifier(probeName),
			whereSQL,
		))

		ctes = append(ctes, fmt.Sprintf(`
        rate_buckets AS (
            SELECT
                date_bin($1::interval, collected_at, $3) AS bucket_time,
                %s
            FROM rate_samples
            GROUP BY date_bin($1::interval, collected_at, $3)
        )`,
			strings.Join(bucketCols, ",\n                "),
		))

		joins = append(joins,
			"LEFT JOIN rate_buckets ON all_buckets.bucket_time = rate_buckets.bucket_time")
	}

	if hasRatio {
		// The ratio is expressed on a 0-100 percentage scale (not a 0-1
		// fraction) to match what the client dashboards render.
		liveExpr := ratioTupleExpr(aggregation, "n_live_tup")
		deadExpr := ratioTupleExpr(aggregation, "n_dead_tup")
		ctes = append(ctes, fmt.Sprintf(`
        ratio_buckets AS (
            SELECT
                date_bin($1::interval, collected_at, $3) AS bucket_time,
                CASE WHEN %[1]s + %[2]s = 0 THEN 0
                     ELSE %[2]s::float
                          / (%[1]s + %[2]s)::float * 100.0
                END AS dead_tuple_ratio
            FROM metrics.%[3]s
            WHERE %[4]s
            GROUP BY date_bin($1::interval, collected_at, $3)
        )`,
			liveExpr,
			deadExpr,
			QuoteIdentifier(probeName),
			whereSQL,
		))

		joins = append(joins,
			"LEFT JOIN ratio_buckets ON all_buckets.bucket_time = ratio_buckets.bucket_time")
	}

	var selectCols []string
	for _, d := range derived {
		switch d.Kind {
		case DerivedPerSec:
			selectCols = append(selectCols,
				"rate_buckets."+QuoteIdentifier(d.OutputName))
		case DerivedDeadTupleRatio:
			selectCols = append(selectCols, "ratio_buckets.dead_tuple_ratio")
		}
	}

	ctes = append(ctes, `
        all_buckets AS (
            SELECT bucket_time
            FROM generate_series($3::timestamptz, $4::timestamptz, $1::interval) AS g(bucket_time)
        )`)

	query := fmt.Sprintf(`
        WITH %s
        SELECT
            all_buckets.bucket_time,
            %s
        FROM all_buckets
        %s
        ORDER BY all_buckets.bucket_time
    `,
		strings.Join(ctes, ","),
		strings.Join(selectCols, ",\n            "),
		strings.Join(joins, "\n        "),
	)

	return query, queryArgs, nil
}

// classifyMetrics splits the requested metric names into raw columns and
// derived metrics while preserving request order. When no metrics are
// requested, all discovered numeric columns are treated as raw metrics.
//
// A name that matches a real numeric column is a raw metric (a real column
// always wins, even if it happens to end in "_per_sec"). A name ending in
// "_per_sec" whose prefix is a real numeric column becomes a per-second
// rate. The literal name "dead_tuple_ratio" is accepted only when the probe
// exposes both n_live_tup and n_dead_tup. Anything else is a client error.
func classifyMetrics(
	requestedMetrics []string,
	metricCols []string,
	probeName string,
) ([]string, []DerivedMetric, []string, error) {
	available := make(map[string]bool, len(metricCols))
	for _, c := range metricCols {
		available[c] = true
	}

	if len(requestedMetrics) == 0 {
		order := append([]string(nil), metricCols...)
		return metricCols, nil, order, nil
	}

	var rawCols []string
	var derived []DerivedMetric
	var outputOrder []string

	// A repeated metric name would otherwise be scanned twice and emit
	// duplicate data points under the same series key; silently drop the
	// repeat rather than erroring, since the client's intent is unambiguous.
	seen := make(map[string]struct{}, len(requestedMetrics))

	for _, m := range requestedMetrics {
		m = strings.TrimSpace(m)
		if !IsValidIdentifier(m) {
			return nil, nil, nil, fmt.Errorf("invalid metric name %q", m)
		}
		if _, exists := seen[m]; exists {
			continue
		}
		seen[m] = struct{}{}

		switch {
		case available[m]:
			rawCols = append(rawCols, m)
			outputOrder = append(outputOrder, m)
		case strings.HasSuffix(m, "_per_sec"):
			base := strings.TrimSuffix(m, "_per_sec")
			if !available[base] {
				return nil, nil, nil, fmt.Errorf(
					"metric %q not found in probe %q: no numeric column %q "+
						"to compute a per-second rate", m, probeName, base)
			}
			derived = append(derived, DerivedMetric{
				OutputName: m,
				BaseColumn: base,
				Kind:       DerivedPerSec,
			})
			outputOrder = append(outputOrder, m)
		case m == "dead_tuple_ratio":
			if !available["n_live_tup"] || !available["n_dead_tup"] {
				return nil, nil, nil, fmt.Errorf(
					"metric %q not supported for probe %q: requires "+
						"n_live_tup and n_dead_tup columns", m, probeName)
			}
			derived = append(derived, DerivedMetric{
				OutputName: m,
				Kind:       DerivedDeadTupleRatio,
			})
			outputOrder = append(outputOrder, m)
		default:
			return nil, nil, nil, fmt.Errorf(
				"metric %q not found in probe %q", m, probeName)
		}
	}

	return rawCols, derived, outputOrder, nil
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

	// Split requested metrics into raw columns and derived metrics.
	rawCols, derived, outputOrder, err := classifyMetrics(
		requestedMetrics, metricCols, probeName)
	if err != nil {
		return nil, err
	}

	if len(outputOrder) == 0 {
		return nil, fmt.Errorf("no numeric metrics found in probe %q", probeName)
	}

	// Resolve the database column name for this probe table
	if filters.DatabaseName != "" && filters.DatabaseColumn == "" {
		dbCol, err := ResolveDatabaseColumn(ctx, pool, probeName)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve database column: %w", err)
		}
		filters.DatabaseColumn = dbCol
	}

	// Collect data across all connections
	dataMap := make(map[seriesKey][]MetricDataPoint)

	// Track last known value per metric column for LOCF
	lastKnown := make(map[string]float64)

	for _, connID := range connectionIDs {
		if len(rawCols) > 0 {
			query, queryArgs, err := BuildMetricsQuery(
				probeName, rawCols, colTypes, connID, timeStart, timeEnd,
				buckets, aggregation, filters)
			if err != nil {
				return nil, fmt.Errorf("failed to build query: %w", err)
			}
			if err := scanSeriesRows(ctx, pool, query, queryArgs, rawCols,
				connID, dataMap, lastKnown); err != nil {
				return nil, err
			}
		}

		if len(derived) > 0 {
			query, queryArgs, err := BuildDerivedMetricsQuery(
				probeName, derived, connID, timeStart, timeEnd,
				buckets, aggregation, filters)
			if err != nil {
				return nil, fmt.Errorf("failed to build derived query: %w", err)
			}
			names := make([]string, len(derived))
			for i, d := range derived {
				names[i] = d.OutputName
			}
			if err := scanSeriesRows(ctx, pool, query, queryArgs, names,
				connID, dataMap, lastKnown); err != nil {
				return nil, err
			}
		}
	}

	// Build result series in the requested metric order.
	var result []MetricSeries
	for _, metric := range outputOrder {
		for _, connID := range connectionIDs {
			key := seriesKey{metric: metric, connectionID: connID}
			data := dataMap[key]
			if data == nil {
				data = []MetricDataPoint{}
			}

			name := metric
			if len(connectionIDs) > 1 {
				name = fmt.Sprintf("%s (conn %d)", metric, connID)
			}

			result = append(result, MetricSeries{
				Name:   name,
				Metric: metric,
				Data:   data,
				Unit:   "",
			})
		}
	}

	return result, nil
}

// seriesKey identifies a metric series by name and connection.
type seriesKey struct {
	metric       string
	connectionID int
}

// scanSeriesRows executes a bucketed metrics query whose first selected
// column is the bucket time followed by one column per name in names, then
// accumulates the values into dataMap. NULL buckets (LEFT JOIN gaps or
// discarded derived samples) are filled with Last Observation Carried
// Forward using lastKnown, keyed per connection and metric name.
func scanSeriesRows(
	ctx context.Context,
	pool *pgxpool.Pool,
	query string,
	queryArgs []any,
	names []string,
	connID int,
	dataMap map[seriesKey][]MetricDataPoint,
	lastKnown map[string]float64,
) error {
	rows, err := pool.Query(ctx, query, queryArgs...)
	if err != nil {
		return fmt.Errorf("failed to query metrics for connection %d: %w", connID, err)
	}
	defer rows.Close()

	for rows.Next() {
		values := make([]any, len(names)+1)
		valuePtrs := make([]any, len(names)+1)
		var bucketTime time.Time
		valuePtrs[0] = &bucketTime
		for i := range names {
			var v any
			values[i+1] = &v
			valuePtrs[i+1] = &values[i+1]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}

		for i, name := range names {
			lkKey := fmt.Sprintf("%d:%s", connID, name)
			val, ok := toFloat64(values[i+1])
			if !ok {
				if prev, exists := lastKnown[lkKey]; exists {
					val = prev
				} else {
					continue
				}
			} else {
				lastKnown[lkKey] = val
			}
			key := seriesKey{metric: name, connectionID: connID}
			dataMap[key] = append(dataMap[key], MetricDataPoint{
				Time:  bucketTime,
				Value: val,
			})
		}
	}

	return rows.Err()
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

// ValidateOrder normalizes and validates a sort direction. It accepts
// "asc" or "desc" case-insensitively and defaults to "desc" when empty.
func ValidateOrder(order string) (string, error) {
	order = strings.ToLower(strings.TrimSpace(order))
	if order == "" {
		return "desc", nil
	}
	if order != "asc" && order != "desc" {
		return "", fmt.Errorf("invalid order %q: must be asc or desc", order)
	}
	return order, nil
}

// ResolveOrderByColumn validates an order_by request against the
// discovered metric columns of a probe table and returns the column to
// sort by. An empty request defaults to collected_at.
//
// order_by is validated against the discovered columns here, before the
// caller quotes it and interpolates it into an ORDER BY clause, because
// PostgreSQL parameter placeholders cannot bind identifiers; only values
// drawn from the trusted, discovered column set may reach the SQL text.
func ResolveOrderByColumn(orderBy string, metricCols []string) (string, error) {
	orderBy = strings.TrimSpace(orderBy)
	if orderBy == "" || orderBy == "collected_at" {
		return "collected_at", nil
	}
	for _, col := range metricCols {
		if col == orderBy {
			return orderBy, nil
		}
	}
	return "", fmt.Errorf(
		"invalid order_by %q: must be a metric column of the probe or collected_at",
		orderBy)
}

// GetProbeAllColumns discovers every column of a probe table in the
// metrics schema, returning the column names in ordinal order and a map
// from column name to its PostgreSQL data type.
func GetProbeAllColumns(ctx context.Context, pool *pgxpool.Pool, probeName string) ([]string, map[string]string, error) {
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

	var allCols []string
	colTypes := make(map[string]string)
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			return nil, nil, err
		}
		allCols = append(allCols, name)
		colTypes[name] = dataType
	}

	return allCols, colTypes, rows.Err()
}

// buildLatestRowsQuery constructs a SQL statement that returns the most
// recent row of a probe table per monitored entity for the given
// connections and filters.
//
// "Most recent" always means the greatest collected_at, never the historical
// maximum of orderCol. An inner DISTINCT ON reduces the matching rows to each
// entity's newest sample (ordered by collected_at DESC) before the outer
// query ranks those per-entity latest rows by orderCol and applies the limit.
// A filter that pins a single entity therefore yields exactly that entity's
// newest row regardless of orderCol, and a broad multi-entity request ranks
// each entity by its own latest sample rather than by any stale historical
// peak.
//
// The entity key always includes connection_id, plus any text/name dimension
// columns (schemaname, relname, indexrelname, ...) the probe exposes. Keying
// on connection_id is essential: without it, two different monitored servers
// that happen to have a table with the same schema+table name would collapse
// into a single DISTINCT ON group, silently dropping one connection's latest
// sample. connection_id is present on every probe table, so the entity key is
// never empty and one query shape serves every probe, whether or not it has
// text/name dimension columns.
//
// Precondition: outputCols must already have the internal bookkeeping columns
// (connection_id, collected_at, inserted_at) stripped by
// selectLatestOutputColumns. The inner subquery appends connection_id and
// collected_at itself, so if outputCols still contained them the subquery
// would emit duplicate column labels and the outer references would be
// ambiguous, breaking the query.
func buildLatestRowsQuery(
	probeName string,
	outputCols []string,
	colTypes map[string]string,
	connectionIDs []int,
	filters MetricFilters,
	orderCol string,
	order string,
	limit int,
) (string, []any) {
	var selectParts []string
	for _, col := range outputCols {
		selectParts = append(selectParts, QuoteIdentifier(col))
	}
	selectClause := strings.Join(selectParts, ", ")

	var whereClauses []string
	var args []any
	argNum := 1

	var connPlaceholders []string
	for _, id := range connectionIDs {
		connPlaceholders = append(connPlaceholders, fmt.Sprintf("$%d", argNum))
		args = append(args, id)
		argNum++
	}
	whereClauses = append(whereClauses,
		fmt.Sprintf("connection_id IN (%s)", strings.Join(connPlaceholders, ", ")))

	if filters.DatabaseName != "" && filters.DatabaseColumn != "" {
		whereClauses = append(whereClauses,
			fmt.Sprintf("%s = $%d", QuoteIdentifier(filters.DatabaseColumn), argNum))
		args = append(args, filters.DatabaseName)
		argNum++
	}

	if filters.SchemaName != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("schemaname = $%d", argNum))
		args = append(args, filters.SchemaName)
		argNum++
	}

	if filters.TableName != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("relname = $%d", argNum))
		args = append(args, filters.TableName)
		argNum++
	}

	if filters.IndexName != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("indexrelname = $%d", argNum))
		args = append(args, filters.IndexName)
		argNum++
	}

	whereClause := strings.Join(whereClauses, " AND ")

	// connection_id is always part of the entity key so that two monitored
	// servers sharing a schema+table name never collapse into one DISTINCT ON
	// group. Any text/name dimension columns the probe exposes extend the key.
	distinctParts := []string{"connection_id"}
	for _, col := range EntityKeyColumns(outputCols, colTypes) {
		distinctParts = append(distinctParts, QuoteIdentifier(col))
	}
	distinctClause := strings.Join(distinctParts, ", ")

	// Reduce to each entity's newest sample first (DISTINCT ON ordered by
	// collected_at DESC), then rank those per-entity latest rows by orderCol.
	// connection_id and collected_at are carried through the inner select so
	// the DISTINCT ON, its collected_at tiebreak, and the default order_by
	// (collected_at) all resolve, while the outer select returns only the
	// caller's columns.
	query := fmt.Sprintf(`
        SELECT %s
        FROM (
            SELECT DISTINCT ON (%s) %s, connection_id, collected_at
            FROM metrics.%s
            WHERE %s
            ORDER BY %s, collected_at DESC
        ) latest
        ORDER BY %s %s, collected_at DESC
        LIMIT $%d
    `,
		selectClause,
		distinctClause,
		selectClause,
		QuoteIdentifier(probeName),
		whereClause,
		distinctClause,
		QuoteIdentifier(orderCol),
		order,
		argNum,
	)
	args = append(args, limit)

	return query, args
}

// validateLatestRowParams validates and normalizes the caller-supplied
// parameters for a latest-rows query. It returns the normalized sort
// direction and the row limit clamped to [1, maxLatestRowLimit].
func validateLatestRowParams(
	probeName string,
	connectionIDs []int,
	order string,
	limit int,
) (string, int, error) {
	if !IsValidIdentifier(probeName) {
		return "", 0, fmt.Errorf("invalid probe name %q", probeName)
	}

	normalizedOrder, err := ValidateOrder(order)
	if err != nil {
		return "", 0, err
	}

	if len(connectionIDs) == 0 {
		return "", 0, fmt.Errorf("at least one connection ID is required")
	}

	if limit < 1 {
		limit = 1
	}
	if limit > maxLatestRowLimit {
		limit = maxLatestRowLimit
	}

	return normalizedOrder, limit, nil
}

// selectLatestOutputColumns filters bookkeeping columns out of a probe's
// full column set, leaving only the columns returned to callers.
func selectLatestOutputColumns(allCols []string) []string {
	var outputCols []string
	for _, col := range allCols {
		if latestRowInternalColumns[col] {
			continue
		}
		outputCols = append(outputCols, col)
	}
	return outputCols
}

// discoverLatestRowColumns verifies the probe table exists, discovers its
// output columns, resolves the order_by column against them, and resolves
// the database filter column. It mutates filters.DatabaseColumn in place
// when a database filter is requested but the column is not yet known.
func discoverLatestRowColumns(
	ctx context.Context,
	pool *pgxpool.Pool,
	probeName string,
	orderBy string,
	filters *MetricFilters,
) ([]string, map[string]string, string, error) {
	var count int
	existsQuery := `
        SELECT COUNT(*) FROM information_schema.tables
        WHERE table_schema = 'metrics'
            AND table_name = $1
            AND table_type = 'BASE TABLE'
    `
	if err := pool.QueryRow(ctx, existsQuery, probeName).Scan(&count); err != nil {
		return nil, nil, "", fmt.Errorf("failed to verify probe: %w", err)
	}
	if count == 0 {
		return nil, nil, "", fmt.Errorf("probe %q not found", probeName)
	}

	allCols, colTypes, err := GetProbeAllColumns(ctx, pool, probeName)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to get probe columns: %w", err)
	}

	outputCols := selectLatestOutputColumns(allCols)
	if len(outputCols) == 0 {
		return nil, nil, "", fmt.Errorf("no columns found in probe %q", probeName)
	}

	// order_by is validated against the full set of returned columns, not
	// just the numeric metrics, so callers can sort by dimension and
	// timestamp columns (e.g. last_vacuum) that appear in the response.
	orderCol, err := ResolveOrderByColumn(orderBy, outputCols)
	if err != nil {
		return nil, nil, "", err
	}

	if filters.DatabaseName != "" && filters.DatabaseColumn == "" {
		dbCol, err := ResolveDatabaseColumn(ctx, pool, probeName)
		if err != nil {
			return nil, nil, "", fmt.Errorf("failed to resolve database column: %w", err)
		}
		filters.DatabaseColumn = dbCol
	}

	return outputCols, colTypes, orderCol, nil
}

// scanLatestRows reads every row from rows into flat maps keyed by the
// given output columns, normalizing each scanned value for JSON output.
// It always returns a non-nil slice so callers emit an empty JSON array
// rather than null when the query yields no rows.
func scanLatestRows(rows pgx.Rows, outputCols []string) ([]map[string]any, error) {
	result := []map[string]any{}
	for rows.Next() {
		values := make([]any, len(outputCols))
		valuePtrs := make([]any, len(outputCols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		row := make(map[string]any, len(outputCols))
		for i, col := range outputCols {
			row[col] = normalizeLatestValue(values[i])
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return result, nil
}

// QueryLatestRows returns the newest row per monitored entity of a probe
// table as flat maps keyed by column name. Unlike QueryTimeSeries it
// produces raw row objects rather than bucketed series, so dashboards can
// read individual column values (including dimension and timestamp columns)
// directly. "Newest" always means the greatest collected_at: a filter that
// pins a single entity yields exactly that entity's latest sample, and
// order_by only ranks the per-entity latest rows, never selecting a stale
// historical peak.
func QueryLatestRows(
	ctx context.Context,
	pool *pgxpool.Pool,
	probeName string,
	connectionIDs []int,
	filters MetricFilters,
	orderBy string,
	order string,
	limit int,
) ([]map[string]any, error) {
	order, limit, err := validateLatestRowParams(probeName, connectionIDs, order, limit)
	if err != nil {
		return nil, err
	}

	outputCols, colTypes, orderCol, err := discoverLatestRowColumns(
		ctx, pool, probeName, orderBy, &filters)
	if err != nil {
		return nil, err
	}

	query, args := buildLatestRowsQuery(
		probeName, outputCols, colTypes, connectionIDs, filters, orderCol, order, limit)

	// This is not a SQL injection risk despite passing a non-literal query
	// string: the only identifiers interpolated into the text are probeName,
	// orderCol, the output column names, and the entity-key (DISTINCT ON)
	// columns, each of which is validated against a live-discovered allow-list
	// (the information_schema.tables existence check, ResolveOrderByColumn, and
	// GetProbeAllColumns) and then QuoteIdentifier-wrapped before it reaches the
	// query. Every runtime value (connection IDs, filter strings, and the
	// limit) is bound through $N placeholders in args and is never concatenated
	// into the SQL text.
	// nosemgrep: go_sql_rule-concat-sqli
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query latest rows: %w", err)
	}
	defer rows.Close()

	return scanLatestRows(rows, outputCols)
}

// sanitizeFloat returns nil for non-finite floats. Go's encoding/json
// cannot marshal NaN or +/-Inf and errors out on the whole payload, so
// such values are dropped to null to keep the response well-formed.
func sanitizeFloat(f float64) any {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil
	}
	return f
}

// normalizeLatestValue converts a scanned database value into a
// JSON-friendly representation, applying non-finite float protection.
func normalizeLatestValue(v any) any {
	switch val := v.(type) {
	case nil:
		return nil
	case float64:
		return sanitizeFloat(val)
	case float32:
		return sanitizeFloat(float64(val))
	case time.Time:
		return val.Format(time.RFC3339)
	case []byte:
		return string(val)
	case pgtype.Numeric:
		f, ok := toFloat64(val)
		if !ok {
			return nil
		}
		return sanitizeFloat(f)
	case pgtype.Interval:
		f, ok := toFloat64(val)
		if !ok {
			return nil
		}
		return sanitizeFloat(f)
	default:
		return val
	}
}

// finiteFloat reports a float only when it is finite. NaN and +/-Inf are
// rejected because encoding/json cannot marshal them; a non-finite value
// would otherwise break the entire JSON response for every series in the
// request. A rejected value is treated as a gap and handled by LOCF.
func finiteFloat(f float64) (float64, bool) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// toFloat64 converts a scanned database value to float64. It returns
// false when the value cannot be converted or is not finite.
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
		return finiteFloat(val)
	case float32:
		return finiteFloat(float64(val))
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
		return finiteFloat(f.Float64)
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
		return finiteFloat(f.Float64)
	case pgtype.Interval:
		if !val.Valid {
			// NULL interval means no lag reported; treat as zero
			return 0, true
		}
		return intervalToSeconds(val), true
	case *pgtype.Interval:
		if val == nil {
			return 0, true
		}
		if !val.Valid {
			// NULL interval means no lag reported; treat as zero
			return 0, true
		}
		return intervalToSeconds(*val), true
	default:
		return 0, false
	}
}

// intervalToSeconds converts a pgtype.Interval to total seconds. It
// follows PostgreSQL's interval-to-scalar convention where one day is
// 86400 seconds and one month is 30 days; the Days and Months components
// are included so intervals larger than a day are not silently truncated.
func intervalToSeconds(iv pgtype.Interval) float64 {
	const (
		secondsPerDay   = 86_400.0
		secondsPerMonth = 30.0 * secondsPerDay
	)
	return float64(iv.Microseconds)/1_000_000.0 +
		float64(iv.Days)*secondsPerDay +
		float64(iv.Months)*secondsPerMonth
}
