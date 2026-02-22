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

	// For each metric, fetch historical data and calculate baselines
	for _, rule := range rules {
		if ctx.Err() != nil {
			return
		}

		// Get historical metric values for all connections
		histValues, err := e.datastore.GetHistoricalMetricValues(ctx, rule.MetricName, lookbackDays)
		if err != nil {
			e.debugLog("No historical data for metric %s: %v", rule.MetricName, err)
			// Fall back to current values for 'all' baseline only
			e.calculateGlobalBaselinesFallback(ctx, connections, rule.MetricName)
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

			// Calculate 'all' baseline (global aggregate)
			e.calculateAllBaseline(ctx, key.connectionID, dbNamePtr, rule.MetricName, values)

			// Calculate hourly baselines (by hour of day)
			e.calculateHourlyBaselines(ctx, key.connectionID, dbNamePtr, rule.MetricName, values, minSamplesForTimePeriod)

			// Calculate daily baselines (by day of week)
			e.calculateDailyBaselines(ctx, key.connectionID, dbNamePtr, rule.MetricName, values, minSamplesForTimePeriod)
		}
	}

	e.log("Baseline calculation complete")
}

// calculateAllBaseline calculates the global 'all' baseline for a metric
func (e *Engine) calculateAllBaseline(ctx context.Context, connID int, dbName *string, metricName string, values []database.HistoricalMetricValue) {
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
		ConnectionID:   connID,
		DatabaseName:   dbName,
		MetricName:     metricName,
		PeriodType:     "all",
		Mean:           mean,
		StdDev:         stddev,
		Min:            minValue(floatValues),
		Max:            maxValue(floatValues),
		SampleCount:    int64(len(floatValues)),
		LastCalculated: time.Now(),
	}

	if err := e.datastore.UpsertMetricBaseline(ctx, baseline); err != nil {
		e.log("ERROR: Failed to upsert 'all' baseline for %s on connection %d: %v",
			metricName, connID, err)
	}
}

// calculateHourlyBaselines calculates baselines for each hour of the day (0-23)
func (e *Engine) calculateHourlyBaselines(ctx context.Context, connID int, dbName *string, metricName string, values []database.HistoricalMetricValue, minSamples int) {
	// Group values by hour of day
	hourlyValues := make(map[int][]float64)
	for _, v := range values {
		hour := v.CollectedAt.Hour()
		hourlyValues[hour] = append(hourlyValues[hour], v.Value)
	}

	// Calculate baseline for each hour that has enough samples
	for hour, vals := range hourlyValues {
		if len(vals) < minSamples {
			continue
		}

		mean, stddev := calculateStats(vals)
		hourVal := hour

		baseline := &database.MetricBaseline{
			ConnectionID:   connID,
			DatabaseName:   dbName,
			MetricName:     metricName,
			PeriodType:     "hourly",
			HourOfDay:      &hourVal,
			Mean:           mean,
			StdDev:         stddev,
			Min:            minValue(vals),
			Max:            maxValue(vals),
			SampleCount:    int64(len(vals)),
			LastCalculated: time.Now(),
		}

		if err := e.datastore.UpsertMetricBaseline(ctx, baseline); err != nil {
			e.log("ERROR: Failed to upsert hourly baseline for %s hour %d on connection %d: %v",
				metricName, hour, connID, err)
		}
	}
}

// calculateDailyBaselines calculates baselines for each day of the week (0=Sunday to 6=Saturday)
func (e *Engine) calculateDailyBaselines(ctx context.Context, connID int, dbName *string, metricName string, values []database.HistoricalMetricValue, minSamples int) {
	// Group values by day of week (0=Sunday, 1=Monday, ..., 6=Saturday)
	dailyValues := make(map[int][]float64)
	for _, v := range values {
		// Go's time.Weekday() returns 0=Sunday, 1=Monday, etc.
		dayOfWeek := int(v.CollectedAt.Weekday())
		dailyValues[dayOfWeek] = append(dailyValues[dayOfWeek], v.Value)
	}

	// Calculate baseline for each day that has enough samples
	for day, vals := range dailyValues {
		if len(vals) < minSamples {
			continue
		}

		mean, stddev := calculateStats(vals)
		dayVal := day

		baseline := &database.MetricBaseline{
			ConnectionID:   connID,
			DatabaseName:   dbName,
			MetricName:     metricName,
			PeriodType:     "daily",
			DayOfWeek:      &dayVal,
			Mean:           mean,
			StdDev:         stddev,
			Min:            minValue(vals),
			Max:            maxValue(vals),
			SampleCount:    int64(len(vals)),
			LastCalculated: time.Now(),
		}

		if err := e.datastore.UpsertMetricBaseline(ctx, baseline); err != nil {
			e.log("ERROR: Failed to upsert daily baseline for %s day %d on connection %d: %v",
				metricName, day, connID, err)
		}
	}
}

// calculateGlobalBaselinesFallback calculates only 'all' baselines when historical data
// is not available. This uses the current metric values as a fallback.
func (e *Engine) calculateGlobalBaselinesFallback(ctx context.Context, connections []int, metricName string) {
	// Get current metric values
	values, err := e.datastore.GetLatestMetricValues(ctx, metricName)
	if err != nil {
		return
	}

	// Group by connection
	for _, connID := range connections {
		var connValues []float64
		for _, v := range values {
			if v.ConnectionID == connID {
				connValues = append(connValues, v.Value)
			}
		}

		if len(connValues) == 0 {
			continue
		}

		mean, stddev := calculateStats(connValues)

		baseline := &database.MetricBaseline{
			ConnectionID:   connID,
			MetricName:     metricName,
			PeriodType:     "all",
			Mean:           mean,
			StdDev:         stddev,
			Min:            minValue(connValues),
			Max:            maxValue(connValues),
			SampleCount:    int64(len(connValues)),
			LastCalculated: time.Now(),
		}

		if err := e.datastore.UpsertMetricBaseline(ctx, baseline); err != nil {
			e.log("ERROR: Failed to upsert fallback baseline for %s on connection %d: %v",
				metricName, connID, err)
		}
	}
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
