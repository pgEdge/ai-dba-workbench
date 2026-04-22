/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"context"
	"fmt"
)

// queryMetricValues executes a SQL query that returns rows with three columns
// (connection_id, value, collected_at) and scans them into MetricValue structs.
func (d *Datastore) queryMetricValues(ctx context.Context, sql string) ([]MetricValue, error) {
	rows, err := d.pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []MetricValue
	for rows.Next() {
		var mv MetricValue
		if err := rows.Scan(&mv.ConnectionID, &mv.Value, &mv.CollectedAt); err != nil {
			return nil, err
		}
		results = append(results, mv)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// queryMetricValuesWithDB executes a SQL query that returns rows with four columns
// (connection_id, database_name, value, collected_at) and scans them into MetricValue structs.
func (d *Datastore) queryMetricValuesWithDB(ctx context.Context, sql string) ([]MetricValue, error) {
	rows, err := d.pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []MetricValue
	for rows.Next() {
		var mv MetricValue
		var dbName string
		if err := rows.Scan(&mv.ConnectionID, &dbName, &mv.Value, &mv.CollectedAt); err != nil {
			return nil, err
		}
		mv.DatabaseName = &dbName
		results = append(results, mv)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// queryMetricValuesWithDBAndObject executes a SQL query that returns rows with five columns
// (connection_id, database_name, object_name, value, collected_at) and scans them into MetricValue structs.
func (d *Datastore) queryMetricValuesWithDBAndObject(ctx context.Context, sql string) ([]MetricValue, error) {
	rows, err := d.pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []MetricValue
	for rows.Next() {
		var mv MetricValue
		var dbName string
		var objectName string
		if err := rows.Scan(&mv.ConnectionID, &dbName, &objectName, &mv.Value, &mv.CollectedAt); err != nil {
			return nil, err
		}
		mv.DatabaseName = &dbName
		mv.ObjectName = &objectName
		results = append(results, mv)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// GetLatestMetricValues retrieves the most recent values for a metric across all connections.
// This queries the collected data tables to find current metric values.
func (d *Datastore) GetLatestMetricValues(ctx context.Context, metricName string) ([]MetricValue, error) {
	cfg, ok := metricRegistry[metricName]
	if !ok {
		return nil, fmt.Errorf("metric %s not implemented", metricName)
	}

	var results []MetricValue
	var err error

	switch cfg.scan {
	case scanBasic:
		results, err = d.queryMetricValues(ctx, cfg.latestSQL)
	case scanWithDB:
		results, err = d.queryMetricValuesWithDB(ctx, cfg.latestSQL)
	case scanWithDBObject:
		results, err = d.queryMetricValuesWithDBAndObject(ctx, cfg.latestSQL)
	default:
		return nil, fmt.Errorf("unknown scan type for metric: %s", metricName)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get %s values: %w", metricName, err)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no data found for metric %s", metricName)
	}

	return results, nil
}

// GetLatestMetricValue retrieves the most recent value for a metric (single value).
// This is a convenience wrapper that returns the first value found.
func (d *Datastore) GetLatestMetricValue(ctx context.Context, metricName string) (value float64, connectionID int, dbName *string, err error) {
	values, err := d.GetLatestMetricValues(ctx, metricName)
	if err != nil {
		return 0, 0, nil, err
	}
	if len(values) == 0 {
		return 0, 0, nil, fmt.Errorf("no data found for metric %s", metricName)
	}
	return values[0].Value, values[0].ConnectionID, values[0].DatabaseName, nil
}

// queryHistoricalMetricValuesBasic executes a historical SQL query that returns rows with
// (connection_id, database_name, value, collected_at) where database_name is scanned as-is
// (typically NULL for basic metrics).
func (d *Datastore) queryHistoricalMetricValuesBasic(ctx context.Context, sql string, lookbackDays int) ([]HistoricalMetricValue, error) {
	rows, err := d.pool.Query(ctx, sql, lookbackDays)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []HistoricalMetricValue
	for rows.Next() {
		var hv HistoricalMetricValue
		if err := rows.Scan(&hv.ConnectionID, &hv.DatabaseName, &hv.Value, &hv.CollectedAt); err != nil {
			return nil, err
		}
		results = append(results, hv)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// queryHistoricalMetricValuesWithDB executes a historical SQL query that returns rows with
// (connection_id, database_name, value, collected_at) where database_name is a non-null string.
func (d *Datastore) queryHistoricalMetricValuesWithDB(ctx context.Context, sql string, lookbackDays int) ([]HistoricalMetricValue, error) {
	rows, err := d.pool.Query(ctx, sql, lookbackDays)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []HistoricalMetricValue
	for rows.Next() {
		var hv HistoricalMetricValue
		var dbName string
		if err := rows.Scan(&hv.ConnectionID, &dbName, &hv.Value, &hv.CollectedAt); err != nil {
			return nil, err
		}
		hv.DatabaseName = &dbName
		results = append(results, hv)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// GetHistoricalMetricValues retrieves historical metric values for baseline calculation.
// It returns values with timestamps from the specified lookback period to enable
// grouping by hour of day and day of week for time-aware baselines.
//
// Every query branch INNER JOINs against the connections table so rows for
// connections that have been deleted (but whose metric rows have not yet aged
// out of the metrics.* tables) are filtered at query time. Without that
// filter, downstream UpsertMetricBaseline calls would fail with foreign key
// violations on metric_baselines.connection_id. See GitHub issue #56.
func (d *Datastore) GetHistoricalMetricValues(ctx context.Context, metricName string, lookbackDays int) ([]HistoricalMetricValue, error) {
	cfg, ok := metricRegistry[metricName]
	if !ok {
		// For metrics not explicitly handled, return an error
		// This allows the caller to fall back to other baseline calculation methods
		return nil, fmt.Errorf("historical data not implemented for metric %s", metricName)
	}

	if cfg.historicalSQL == "" {
		// For metrics not explicitly handled, return an error
		// This allows the caller to fall back to other baseline calculation methods
		return nil, fmt.Errorf("historical data not implemented for metric %s", metricName)
	}

	var results []HistoricalMetricValue
	var err error

	switch cfg.historicalScan {
	case historicalScanBasic:
		results, err = d.queryHistoricalMetricValuesBasic(ctx, cfg.historicalSQL, lookbackDays)
	case historicalScanWithDB:
		results, err = d.queryHistoricalMetricValuesWithDB(ctx, cfg.historicalSQL, lookbackDays)
	default:
		return nil, fmt.Errorf("unknown historical scan type for metric: %s", metricName)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to query historical %s: %w", metricName, err)
	}

	return results, nil
}
