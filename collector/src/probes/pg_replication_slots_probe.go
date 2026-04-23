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
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/collector/src/utils"
)

// PgReplicationSlotsProbe collects metrics from pg_replication_slots view
// and joins with pg_stat_replication_slots (PG14+) for statistics
type PgReplicationSlotsProbe struct {
	BaseMetricsProbe
}

// NewPgReplicationSlotsProbe creates a new pg_replication_slots probe
func NewPgReplicationSlotsProbe(config *ProbeConfig) *PgReplicationSlotsProbe {
	return &PgReplicationSlotsProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetQuery returns the SQL query to execute
func (p *PgReplicationSlotsProbe) GetQuery() string {
	return ""
}

// checkStatReplicationSlotsAvailable checks if pg_stat_replication_slots view exists (PG14+)
func (p *PgReplicationSlotsProbe) checkStatReplicationSlotsAvailable(ctx context.Context, conn *pgxpool.Conn) (bool, error) {
	var exists bool
	err := conn.QueryRow(ctx, `
        SELECT EXISTS(
            SELECT 1
            FROM pg_views
            WHERE schemaname = 'pg_catalog'
            AND viewname = 'pg_stat_replication_slots'
        )
    `).Scan(&exists)

	if err != nil {
		return false, fmt.Errorf("failed to check for pg_stat_replication_slots view: %w", err)
	}

	return exists, nil
}

// checkHasTotalCount checks if pg_stat_replication_slots has total_count column (PG15+)
func (p *PgReplicationSlotsProbe) checkHasTotalCount(ctx context.Context, conn *pgxpool.Conn) (bool, error) {
	var exists bool
	err := conn.QueryRow(ctx, `
        SELECT EXISTS(
            SELECT 1
            FROM information_schema.columns
            WHERE table_schema = 'pg_catalog'
            AND table_name = 'pg_stat_replication_slots'
            AND column_name = 'total_count'
        )
    `).Scan(&exists)

	if err != nil {
		return false, fmt.Errorf("failed to check for total_count column: %w", err)
	}

	return exists, nil
}

// Execute runs the probe against a monitored connection
func (p *PgReplicationSlotsProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]any, error) {
	// Check if pg_stat_replication_slots is available (PG14+)
	statsAvailable, err := cachedCheck(connectionName, "stat_replication_slots_available", func() (bool, error) {
		return p.checkStatReplicationSlotsAvailable(ctx, monitoredConn)
	})
	if err != nil {
		return nil, err
	}

	var walStatusExpr, safeWalSizeExpr string
	if pgVersion >= 13 {
		walStatusExpr = "rs.wal_status"
		safeWalSizeExpr = "rs.safe_wal_size"
	} else {
		walStatusExpr = "NULL::text AS wal_status"
		safeWalSizeExpr = "NULL::bigint AS safe_wal_size"
	}

	var query string
	if statsAvailable {
		// Check for total_count column (PG15+)
		hasTotalCount, err := cachedCheck(connectionName, "replication_slots_total_count", func() (bool, error) {
			return p.checkHasTotalCount(ctx, monitoredConn)
		})
		if err != nil {
			return nil, err
		}

		var totalCountExpr string
		if hasTotalCount {
			totalCountExpr = "srs.total_count"
		} else {
			totalCountExpr = "NULL::bigint AS total_count"
		}

		// PG14+ with pg_stat_replication_slots join
		query = fmt.Sprintf(`
            SELECT
                rs.slot_name,
                rs.slot_type,
                rs.active,
                %s,
                %s,
                pg_wal_lsn_diff(pg_current_wal_lsn(), rs.restart_lsn) AS retained_bytes,
                srs.spill_txns,
                srs.spill_count,
                srs.spill_bytes,
                srs.stream_txns,
                srs.stream_count,
                srs.stream_bytes,
                srs.total_txns,
                %s,
                srs.total_bytes,
                srs.stats_reset
            FROM pg_replication_slots rs
            LEFT JOIN pg_stat_replication_slots srs ON rs.slot_name = srs.slot_name
            WHERE rs.restart_lsn IS NOT NULL
            ORDER BY rs.slot_name
        `, walStatusExpr, safeWalSizeExpr, totalCountExpr)
	} else {
		// PG13 and earlier without pg_stat_replication_slots
		query = fmt.Sprintf(`
            SELECT
                slot_name,
                slot_type,
                active,
                %s,
                %s,
                pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn) AS retained_bytes,
                NULL::bigint AS spill_txns,
                NULL::bigint AS spill_count,
                NULL::bigint AS spill_bytes,
                NULL::bigint AS stream_txns,
                NULL::bigint AS stream_count,
                NULL::bigint AS stream_bytes,
                NULL::bigint AS total_txns,
                NULL::bigint AS total_count,
                NULL::bigint AS total_bytes,
                NULL::timestamptz AS stats_reset
            FROM pg_replication_slots
            WHERE restart_lsn IS NOT NULL
            ORDER BY slot_name
        `, walStatusExpr, safeWalSizeExpr)
	}

	wrappedQuery := WrapQuery(ProbeNamePgReplicationSlots, query)
	rows, err := monitoredConn.Query(ctx, wrappedQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgReplicationSlotsProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]any) error {
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
		"spill_txns", "spill_count", "spill_bytes",
		"stream_txns", "stream_count", "stream_bytes",
		"total_txns", "total_count", "total_bytes",
		"stats_reset",
	}

	// Build values array
	var values [][]any
	for _, metric := range metrics {
		row := []any{
			connectionID,
			timestamp,
			metric["slot_name"],
			metric["slot_type"],
			metric["active"],
			metric["wal_status"],
			metric["safe_wal_size"],
			metric["retained_bytes"],
			metric["spill_txns"],
			metric["spill_count"],
			metric["spill_bytes"],
			metric["stream_txns"],
			metric["stream_count"],
			metric["stream_bytes"],
			metric["total_txns"],
			metric["total_count"],
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
