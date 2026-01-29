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
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/collector/src/utils"
)

// PgReplicationSlotsProbe collects metrics from pg_replication_slots view
type PgReplicationSlotsProbe struct {
	BaseMetricsProbe
}

// NewPgReplicationSlotsProbe creates a new pg_replication_slots probe
func NewPgReplicationSlotsProbe(config *ProbeConfig) *PgReplicationSlotsProbe {
	return &PgReplicationSlotsProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgReplicationSlotsProbe) GetName() string {
	return ProbeNamePgReplicationSlots
}

// GetTableName returns the metrics table name
func (p *PgReplicationSlotsProbe) GetTableName() string {
	return ProbeNamePgReplicationSlots
}

// IsDatabaseScoped returns false as pg_replication_slots is server-scoped
func (p *PgReplicationSlotsProbe) IsDatabaseScoped() bool {
	return false
}

// GetQuery returns the SQL query to execute
func (p *PgReplicationSlotsProbe) GetQuery() string {
	return ""
}

// Execute runs the probe against a monitored connection
func (p *PgReplicationSlotsProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
	var walStatusExpr, safeWalSizeExpr string
	if pgVersion >= 130000 {
		walStatusExpr = "wal_status"
		safeWalSizeExpr = "safe_wal_size"
	} else {
		walStatusExpr = "NULL::text AS wal_status"
		safeWalSizeExpr = "NULL::bigint AS safe_wal_size"
	}

	query := fmt.Sprintf(`
        SELECT
            slot_name,
            slot_type,
            active,
            %s,
            %s,
            pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn) AS retained_bytes
        FROM pg_replication_slots
        WHERE restart_lsn IS NOT NULL
        ORDER BY slot_name
    `, walStatusExpr, safeWalSizeExpr)

	rows, err := monitoredConn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgReplicationSlotsProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
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
		"slot_name", "slot_type", "active",
		"wal_status", "safe_wal_size", "retained_bytes",
	}

	// Build values array
	var values [][]interface{}
	for _, metric := range metrics {
		row := []interface{}{
			connectionID,
			timestamp,
			metric["slot_name"],
			metric["slot_type"],
			metric["active"],
			metric["wal_status"],
			metric["safe_wal_size"],
			metric["retained_bytes"],
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
func (p *PgReplicationSlotsProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
