/*-------------------------------------------------------------------------
 *
 * pgEdge AI Workbench
 *
 * Copyright (c) 2025, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package probes

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/collector/src/utils"

	"fmt"
	"time"
)

// PgStatSubscriptionStatsProbe collects metrics from pg_stat_subscription_stats view
type PgStatSubscriptionStatsProbe struct {
	BaseMetricsProbe
}

// NewPgStatSubscriptionStatsProbe creates a new pg_stat_subscription_stats probe
func NewPgStatSubscriptionStatsProbe(config *ProbeConfig) *PgStatSubscriptionStatsProbe {
	return &PgStatSubscriptionStatsProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgStatSubscriptionStatsProbe) GetName() string {
	return ProbeNamePgStatSubscriptionStats
}

// GetTableName returns the metrics table name
func (p *PgStatSubscriptionStatsProbe) GetTableName() string {
	return ProbeNamePgStatSubscriptionStats
}

// IsDatabaseScoped returns false as pg_stat_subscription_stats is server-scoped
func (p *PgStatSubscriptionStatsProbe) IsDatabaseScoped() bool {
	return false
}

// GetQuery returns the SQL query to execute
func (p *PgStatSubscriptionStatsProbe) GetQuery() string {
	return `
        SELECT
            subid,
            subname,
            apply_error_count,
            sync_error_count,
            stats_reset
        FROM pg_stat_subscription_stats
    `
}

// checkViewAvailable checks if pg_stat_subscription_stats view exists (PG 15+)
func (p *PgStatSubscriptionStatsProbe) checkViewAvailable(ctx context.Context, conn *pgxpool.Conn) (bool, error) {
	var exists bool
	err := conn.QueryRow(ctx, `
        SELECT EXISTS(
            SELECT 1
            FROM pg_views
            WHERE schemaname = 'pg_catalog'
            AND viewname = 'pg_stat_subscription_stats'
        )
    `).Scan(&exists)

	if err != nil {
		return false, fmt.Errorf("failed to check for pg_stat_subscription_stats view: %w", err)
	}

	return exists, nil
}

// Execute runs the probe against a monitored connection
func (p *PgStatSubscriptionStatsProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
	// Check if view is available (PG 15+)
	available, err := p.checkViewAvailable(ctx, monitoredConn)
	if err != nil {
		return nil, err
	}

	if !available {
		// View not available, return empty metrics (not an error)
		return []map[string]interface{}{}, nil
	}

	rows, err := monitoredConn.Query(ctx, p.GetQuery())
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatSubscriptionStatsProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
	if len(metrics) == 0 {
		return nil // Nothing to store
	}

	// Ensure partition exists for this timestamp
	if err := p.EnsurePartition(ctx, datastoreConn, timestamp); err != nil {
		return fmt.Errorf("failed to ensure partition: %w", err)
	}

	// Define columns in order
	columns := []string{
		"connection_id", "collected_at",
		"subid", "subname", "apply_error_count", "sync_error_count", "stats_reset",
	}

	// Build values array
	var values [][]interface{}
	for _, metric := range metrics {
		row := []interface{}{
			connectionID,
			timestamp,
			metric["subid"],
			metric["subname"],
			metric["apply_error_count"],
			metric["sync_error_count"],
			metric["stats_reset"],
		}
		values = append(values, row)
	}

	// Use COPY protocol to store metrics
	if err := StoreMetricsWithCopy(ctx, datastoreConn, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store metrics: %w", err)
	}

	return nil
}

// EnsurePartition ensures a partition exists for the given timestamp
func (p *PgStatSubscriptionStatsProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
