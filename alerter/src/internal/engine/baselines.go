/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package engine

import (
	"context"
	"math"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// calculateBaselines recalculates metric baselines for anomaly detection.
// It generates three types of baselines:
//   - 'all': Global aggregate baseline across all historical data
//   - 'hourly': Baselines per hour of day (0-23) for time-aware anomaly detection
//   - 'daily': Baselines per day of week (0=Sunday to 6=Saturday)
func (e *Engine) calculateBaselines(ctx context.Context) {
	e.debugLog("Calculating baselines...")

	// Get all active connections
	connections, err := e.datastore.GetActiveConnections(ctx)
	if err != nil {
		e.log("ERROR: Failed to get active connections: %v", err)
		return
	}

	// Get all enabled alert rules to determine which metrics need baselines
	rules, err := e.datastore.GetEnabledAlertRules(ctx)
	if err != nil {
		e.log("ERROR: Failed to get alert rules: %v", err)
		return
	}

	// Get lookback days from config (default to 7 if not set)
	cfg := e.getConfig()
	lookbackDays := cfg.Baselines.LookbackDays
	if lookbackDays <= 0 {
		lookbackDays = 7
	}

	// Minimum samples required to create a time-period baseline
	const minSamplesForTimePeriod = 3

	e.log("Calculating baselines for %d connections, %d rules (lookback: %d days)",
		len(connections), len(rules), lookbackDays)

	// Rows written for metrics that no longer support baselines (the
	// fallback path removed in #408 wrote one per connection every
	// cycle) would otherwise sit in metric_baselines forever, cold and
	// visible to the server's get_metric_baselines tool.
	deleted, err := e.datastore.DeleteBaselinesForUnsupportedMetrics(ctx)
	if err != nil {
		e.log("ERROR: Failed to delete baselines for unsupported metrics: %v", err)
	} else if deleted > 0 {
		e.log("Deleted %d baseline rows for metrics without historical data", deleted)
	}

	// For each metric, fetch historical data and calculate baselines
	for _, rule := range rules {
		if ctx.Err() != nil {
			return
		}

		// A metric without a historical query cannot be baselined;
		// building one from the latest sample instead would produce a
		// row the warmup gate can never admit.
		if !database.SupportsBaselines(rule.MetricName) {
			e.debugLog("Skipping baselines for metric %s: no historical data query", rule.MetricName)
			continue
		}

		// Get historical metric values for all connections
		histValues, err := e.datastore.GetHistoricalMetricValues(ctx, rule.MetricName, lookbackDays)
		if err != nil {
			e.log("ERROR: Failed to get historical data for metric %s: %v", rule.MetricName, err)
			continue
		}

		if len(histValues) == 0 {
			continue
		}

		e.debugLog("Processing %d historical values for metric %s", len(histValues), rule.MetricName)

		// Group values by connection ID and optionally database name
		type groupKey struct {
			connectionID int
			databaseName string
		}
		groupedValues := make(map[groupKey][]database.HistoricalMetricValue)

		for _, hv := range histValues {
			dbName := ""
			if hv.DatabaseName != nil {
				dbName = *hv.DatabaseName
			}
			key := groupKey{connectionID: hv.ConnectionID, databaseName: dbName}
			groupedValues[key] = append(groupedValues[key], hv)
		}

		// Process each connection/database group
		for key, values := range groupedValues {
			if ctx.Err() != nil {
				return
			}

			var dbNamePtr *string
			if key.databaseName != "" {
				dbNamePtr = &key.databaseName
			}

			// Capture the earliest sample timestamp once for this
			// (connection, metric) pair so the same value is shared
			// across the 'all', 'hourly', and 'daily' period_type
			// rows written below. Anomaly warmup logic in Task 6/7
			// reads this column to gate detection until the baseline
			// has matured.
			earliest := earliestTimestamp(values)

			// Calculate 'all' baseline (global aggregate)
			e.calculateAllBaseline(ctx, key.connectionID, dbNamePtr, rule.MetricName, values, earliest)

			// Calculate hourly baselines (by hour of day)
			e.calculateHourlyBaselines(ctx, key.connectionID, dbNamePtr, rule.MetricName, values, minSamplesForTimePeriod, earliest)

			// Calculate daily baselines (by day of week)
			e.calculateDailyBaselines(ctx, key.connectionID, dbNamePtr, rule.MetricName, values, minSamplesForTimePeriod, earliest)
		}
	}

	e.log("Baseline calculation complete")
}

// calculateAllBaseline calculates the global 'all' baseline for a metric.
// The earliest parameter is the minimum collected_at timestamp across the
// raw samples backing this (connection, metric) group; it is persisted on
// the baseline row so anomaly detection can gate on baseline maturity.
func (e *Engine) calculateAllBaseline(ctx context.Context, connID int, dbName *string, metricName string, values []database.HistoricalMetricValue, earliest time.Time) {
	if len(values) == 0 {
		return
	}

	// Extract float values
	floatValues := make([]float64, len(values))
	for i, v := range values {
		floatValues[i] = v.Value
	}

	mean, stddev := calculateStats(floatValues)

	baseline := &database.MetricBaseline{
		ConnectionID:     connID,
		DatabaseName:     dbName,
		MetricName:       metricName,
		PeriodType:       "all",
		Mean:             mean,
		StdDev:           stddev,
		Min:              minValue(floatValues),
		Max:              maxValue(floatValues),
		SampleCount:      totalSamples(values),
		LastCalculated:   time.Now(),
		EarliestSampleAt: earliest,
	}

	if err := e.datastore.UpsertMetricBaseline(ctx, baseline); err != nil {
		e.log("ERROR: Failed to upsert 'all' baseline for %s on connection %d: %v",
			metricName, connID, err)
	}
}

// calculateHourlyBaselines calculates baselines for each hour of the day
// (0-23). The earliest parameter is the minimum collected_at timestamp
// across the raw samples backing this (connection, metric) group; it is
// shared across every hourly row so all period_type baselines for the
// same input data agree on baseline age.
func (e *Engine) calculateHourlyBaselines(ctx context.Context, connID int, dbName *string, metricName string, values []database.HistoricalMetricValue, minSamples int, earliest time.Time) {
	// Group values by hour of day
	hourlyValues := make(map[int][]database.HistoricalMetricValue)
	for _, v := range values {
		hour, _ := baselinePeriodKeys(v.CollectedAt)
		hourlyValues[hour] = append(hourlyValues[hour], v)
	}

	// Calculate baseline for each hour that has enough data points
	for hour, hourValues := range hourlyValues {
		if len(hourValues) < minSamples {
			continue
		}

		vals := metricValues(hourValues)
		mean, stddev := calculateStats(vals)
		hourVal := hour

		baseline := &database.MetricBaseline{
			ConnectionID:     connID,
			DatabaseName:     dbName,
			MetricName:       metricName,
			PeriodType:       "hourly",
			HourOfDay:        &hourVal,
			Mean:             mean,
			StdDev:           stddev,
			Min:              minValue(vals),
			Max:              maxValue(vals),
			SampleCount:      totalSamples(hourValues),
			LastCalculated:   time.Now(),
			EarliestSampleAt: earliest,
		}

		if err := e.datastore.UpsertMetricBaseline(ctx, baseline); err != nil {
			e.log("ERROR: Failed to upsert hourly baseline for %s hour %d on connection %d: %v",
				metricName, hour, connID, err)
		}
	}
}

// calculateDailyBaselines calculates baselines for each day of the week
// (0=Sunday to 6=Saturday). The earliest parameter is the minimum
// collected_at timestamp across the raw samples backing this (connection,
// metric) group; it is shared across every daily row so all period_type
// baselines for the same input data agree on baseline age.
func (e *Engine) calculateDailyBaselines(ctx context.Context, connID int, dbName *string, metricName string, values []database.HistoricalMetricValue, minSamples int, earliest time.Time) {
	// Group values by day of week (0=Sunday, 1=Monday, ..., 6=Saturday)
	dailyValues := make(map[int][]database.HistoricalMetricValue)
	for _, v := range values {
		_, dayOfWeek := baselinePeriodKeys(v.CollectedAt)
		dailyValues[dayOfWeek] = append(dailyValues[dayOfWeek], v)
	}

	// Calculate baseline for each day that has enough data points
	for day, dayValues := range dailyValues {
		if len(dayValues) < minSamples {
			continue
		}

		vals := metricValues(dayValues)
		mean, stddev := calculateStats(vals)
		dayVal := day

		baseline := &database.MetricBaseline{
			ConnectionID:     connID,
			DatabaseName:     dbName,
			MetricName:       metricName,
			PeriodType:       "daily",
			DayOfWeek:        &dayVal,
			Mean:             mean,
			StdDev:           stddev,
			Min:              minValue(vals),
			Max:              maxValue(vals),
			SampleCount:      totalSamples(dayValues),
			LastCalculated:   time.Now(),
			EarliestSampleAt: earliest,
		}

		if err := e.datastore.UpsertMetricBaseline(ctx, baseline); err != nil {
			e.log("ERROR: Failed to upsert daily baseline for %s day %d on connection %d: %v",
				metricName, day, connID, err)
		}
	}
}

// baselinePeriodKeys returns the hour_of_day (0-23) and day_of_week
// (0=Sunday to 6=Saturday) bucket keys for a timestamp. It is the single
// definition used both when hourly and daily baselines are written and
// when selectBaseline looks one up, so the two sides cannot drift.
//
// The timestamp is normalised to UTC first. pgx scans timestamptz
// columns into the process's local zone unless a ScanLocation is
// configured, and time.Now() is local too, so without this a collector
// and an alerter in different zones, or one alerter restarted under a
// different TZ, would bucket the same instant under different keys. The
// keys are therefore UTC hours and UTC weekdays; a metric's diurnal
// cycle is still captured, just keyed in UTC.
func baselinePeriodKeys(t time.Time) (hourOfDay, dayOfWeek int) {
	u := t.UTC()
	return u.Hour(), int(u.Weekday())
}

// metricValues extracts the plain float values from historical samples,
// in order, for the statistics helpers.
func metricValues(samples []database.HistoricalMetricValue) []float64 {
	vals := make([]float64, len(samples))
	for i, s := range samples {
		vals[i] = s.Value
	}
	return vals
}

// totalSamples returns the number of raw collector samples behind the
// given historical rows, which is not the same as the number of rows:
// the hourly bucketed queries return one row per hour and report how many
// samples that hour aggregated.
//
// Baseline warmup (anomaly.tier1.warmup.*.min_samples) is configured in
// samples, so counting rows would make the bucketed metrics look far
// colder than they are; at the default seven day lookback a bucketed
// metric yields at most 168 rows against a min_samples of 100, and at a
// lookback of four days or fewer it could never warm at all. A row with
// an unset (zero or negative) count is read as a single sample, which is
// what every non-bucketed query and every hand-built value is. See
// GitHub issue #409.
func totalSamples(samples []database.HistoricalMetricValue) int64 {
	var total int64
	for _, s := range samples {
		if s.SampleCount > 0 {
			total += s.SampleCount
			continue
		}
		total++
	}
	return total
}

// earliestTimestamp returns the smallest CollectedAt across the given
// samples, or the Go zero time if samples is empty. The historical SQL
// queries that source these samples generally order by collected_at, but
// this scan is defensive: callers persist the return value verbatim as
// the baseline's earliest_sample_at, which feeds anomaly-detection
// warmup gating, so correctness matters more than the small extra pass.
func earliestTimestamp(samples []database.HistoricalMetricValue) time.Time {
	if len(samples) == 0 {
		return time.Time{}
	}
	earliest := samples[0].CollectedAt
	for _, s := range samples[1:] {
		if s.CollectedAt.Before(earliest) {
			earliest = s.CollectedAt
		}
	}
	return earliest
}

// calculateStats calculates mean and standard deviation for a slice of values
func calculateStats(values []float64) (mean, stddev float64) {
	if len(values) == 0 {
		return 0, 0
	}

	// Calculate mean
	var sum float64
	for _, v := range values {
		sum += v
	}
	mean = sum / float64(len(values))

	// Calculate standard deviation
	var variance float64
	for _, v := range values {
		diff := v - mean
		variance += diff * diff
	}
	variance /= float64(len(values))
	stddev = math.Sqrt(variance)

	return mean, stddev
}

// minValue returns the minimum value in a slice
func minValue(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	min := values[0]
	for _, v := range values[1:] {
		if v < min {
			min = v
		}
	}
	return min
}

// maxValue returns the maximum value in a slice
func maxValue(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	max := values[0]
	for _, v := range values[1:] {
		if v > max {
			max = v
		}
	}
	return max
}
