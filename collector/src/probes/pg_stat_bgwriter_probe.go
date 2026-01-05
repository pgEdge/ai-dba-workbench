/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
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

// PgStatBgwriterProbe collects metrics from pg_stat_bgwriter view
// Note: This view is deprecated in PostgreSQL 17+ (replaced by pg_stat_checkpointer)
type PgStatBgwriterProbe struct {
	BaseMetricsProbe
}

// NewPgStatBgwriterProbe creates a new pg_stat_bgwriter probe
func NewPgStatBgwriterProbe(config *ProbeConfig) *PgStatBgwriterProbe {
	return &PgStatBgwriterProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgStatBgwriterProbe) GetName() string {
	return ProbeNamePgStatBgwriter
}

// GetTableName returns the metrics table name
func (p *PgStatBgwriterProbe) GetTableName() string {
	return ProbeNamePgStatBgwriter
}

// IsDatabaseScoped returns false as pg_stat_bgwriter is server-scoped
func (p *PgStatBgwriterProbe) IsDatabaseScoped() bool {
	return false
}

// GetQuery returns the SQL query to execute
func (p *PgStatBgwriterProbe) GetQuery() string {
	return `
        SELECT
            buffers_clean,
            maxwritten_clean,
            buffers_alloc,
            stats_reset
        FROM pg_stat_bgwriter
    `
}

// Execute runs the probe against a monitored connection
func (p *PgStatBgwriterProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
	rows, err := monitoredConn.Query(ctx, p.GetQuery())
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatBgwriterProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
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
		"buffers_clean", "maxwritten_clean", "buffers_alloc",
		"stats_reset",
	}

	// Build values array
	var values [][]interface{}
	for _, metric := range metrics {
		row := []interface{}{
			connectionID,
			timestamp,
			metric["buffers_clean"],
			metric["maxwritten_clean"],
			metric["buffers_alloc"],
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
func (p *PgStatBgwriterProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
