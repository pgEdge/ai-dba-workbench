/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
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

// PgStatReplicationSlotsProbe collects metrics from pg_stat_replication_slots view
type PgStatReplicationSlotsProbe struct {
	BaseMetricsProbe
}

// NewPgStatReplicationSlotsProbe creates a new pg_stat_replication_slots probe
func NewPgStatReplicationSlotsProbe(config *ProbeConfig) *PgStatReplicationSlotsProbe {
	return &PgStatReplicationSlotsProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgStatReplicationSlotsProbe) GetName() string {
	return ProbeNamePgStatReplicationSlots
}

// GetTableName returns the metrics table name
func (p *PgStatReplicationSlotsProbe) GetTableName() string {
	return ProbeNamePgStatReplicationSlots
}

// IsDatabaseScoped returns false as pg_stat_replication_slots is server-scoped
func (p *PgStatReplicationSlotsProbe) IsDatabaseScoped() bool {
	return false
}

// GetQuery returns the SQL query to execute
func (p *PgStatReplicationSlotsProbe) GetQuery() string {
	return `
        SELECT
            slot_name,
            spill_txns,
            spill_count,
            spill_bytes,
            stream_txns,
            stream_count,
            stream_bytes,
            total_txns,
            total_bytes,
            stats_reset
        FROM pg_stat_replication_slots
        ORDER BY slot_name
    `
}

// Execute runs the probe against a monitored connection
func (p *PgStatReplicationSlotsProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
	rows, err := monitoredConn.Query(ctx, p.GetQuery())
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatReplicationSlotsProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
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
		"slot_name", "spill_txns", "spill_count", "spill_bytes",
		"stream_txns", "stream_count", "stream_bytes",
		"total_txns", "total_bytes", "stats_reset",
	}

	// Build values array
	var values [][]interface{}
	for _, metric := range metrics {
		row := []interface{}{
			connectionID,
			timestamp,
			metric["slot_name"],
			metric["spill_txns"],
			metric["spill_count"],
			metric["spill_bytes"],
			metric["stream_txns"],
			metric["stream_count"],
			metric["stream_bytes"],
			metric["total_txns"],
			metric["total_bytes"],
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
func (p *PgStatReplicationSlotsProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
