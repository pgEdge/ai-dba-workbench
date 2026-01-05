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

// PgStatWALReceiverProbe collects metrics from pg_stat_wal_receiver view
type PgStatWALReceiverProbe struct {
	BaseMetricsProbe
}

// NewPgStatWALReceiverProbe creates a new pg_stat_wal_receiver probe
func NewPgStatWALReceiverProbe(config *ProbeConfig) *PgStatWALReceiverProbe {
	return &PgStatWALReceiverProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgStatWALReceiverProbe) GetName() string {
	return ProbeNamePgStatWALReceiver
}

// GetTableName returns the metrics table name
func (p *PgStatWALReceiverProbe) GetTableName() string {
	return ProbeNamePgStatWALReceiver
}

// IsDatabaseScoped returns false as pg_stat_wal_receiver is server-scoped
func (p *PgStatWALReceiverProbe) IsDatabaseScoped() bool {
	return false
}

// GetQuery returns the SQL query to execute
func (p *PgStatWALReceiverProbe) GetQuery() string {
	return `
        SELECT
            pid,
            status,
            receive_start_lsn::text,
            receive_start_tli,
            written_lsn::text,
            flushed_lsn::text,
            received_tli,
            last_msg_send_time,
            last_msg_receipt_time,
            latest_end_lsn::text,
            latest_end_time,
            slot_name,
            sender_host,
            sender_port,
            conninfo
        FROM pg_stat_wal_receiver
    `
}

// Execute runs the probe against a monitored connection
func (p *PgStatWALReceiverProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
	rows, err := monitoredConn.Query(ctx, p.GetQuery())
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatWALReceiverProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
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
		"pid", "status", "receive_start_lsn", "receive_start_tli",
		"written_lsn", "flushed_lsn", "received_tli",
		"last_msg_send_time", "last_msg_receipt_time",
		"latest_end_lsn", "latest_end_time",
		"slot_name", "sender_host", "sender_port", "conninfo",
	}

	// Build values array
	var values [][]interface{}
	for _, metric := range metrics {
		row := []interface{}{
			connectionID,
			timestamp,
			metric["pid"],
			metric["status"],
			metric["receive_start_lsn"],
			metric["receive_start_tli"],
			metric["written_lsn"],
			metric["flushed_lsn"],
			metric["received_tli"],
			metric["last_msg_send_time"],
			metric["last_msg_receipt_time"],
			metric["latest_end_lsn"],
			metric["latest_end_time"],
			metric["slot_name"],
			metric["sender_host"],
			metric["sender_port"],
			metric["conninfo"],
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
func (p *PgStatWALReceiverProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
