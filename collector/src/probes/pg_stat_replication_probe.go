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

// PgStatReplicationProbe collects metrics from pg_stat_replication view
// On standbys, it also collects from pg_stat_wal_receiver to provide a unified view
type PgStatReplicationProbe struct {
	BaseMetricsProbe
}

// NewPgStatReplicationProbe creates a new pg_stat_replication probe
func NewPgStatReplicationProbe(config *ProbeConfig) *PgStatReplicationProbe {
	return &PgStatReplicationProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgStatReplicationProbe) GetName() string {
	return ProbeNamePgStatReplication
}

// GetTableName returns the metrics table name
func (p *PgStatReplicationProbe) GetTableName() string {
	return ProbeNamePgStatReplication
}

// IsDatabaseScoped returns false as pg_stat_replication is server-scoped
func (p *PgStatReplicationProbe) IsDatabaseScoped() bool {
	return false
}

// GetQuery returns the SQL query to execute
func (p *PgStatReplicationProbe) GetQuery() string {
	return ""
}

// checkIsInRecovery checks if the server is in recovery mode (standby)
func (p *PgStatReplicationProbe) checkIsInRecovery(ctx context.Context, conn *pgxpool.Conn) (bool, error) {
	var inRecovery bool
	err := conn.QueryRow(ctx, `SELECT pg_is_in_recovery()`).Scan(&inRecovery)
	if err != nil {
		return false, fmt.Errorf("failed to check recovery status: %w", err)
	}
	return inRecovery, nil
}

// Execute runs the probe against a monitored connection
func (p *PgStatReplicationProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
	// Check if we're on a standby
	inRecovery, err := p.checkIsInRecovery(ctx, monitoredConn)
	if err != nil {
		return nil, err
	}

	var metrics []map[string]interface{}

	if !inRecovery {
		// Primary: collect from pg_stat_replication
		query := `
            SELECT
                'primary' AS role,
                pid,
                usesysid,
                usename,
                application_name,
                client_addr,
                client_hostname,
                client_port,
                backend_start,
                backend_xmin::text,
                state,
                sent_lsn::text,
                write_lsn::text,
                flush_lsn::text,
                replay_lsn::text,
                write_lag,
                flush_lag,
                replay_lag,
                sync_priority,
                sync_state,
                reply_time,
                -- Receiver columns are NULL on primary
                NULL::integer AS receiver_pid,
                NULL::text AS receiver_status,
                NULL::text AS receive_start_lsn,
                NULL::integer AS receive_start_tli,
                NULL::text AS written_lsn,
                NULL::text AS receiver_flushed_lsn,
                NULL::integer AS received_tli,
                NULL::timestamptz AS last_msg_send_time,
                NULL::timestamptz AS last_msg_receipt_time,
                NULL::text AS latest_end_lsn,
                NULL::timestamptz AS latest_end_time,
                NULL::text AS receiver_slot_name,
                NULL::text AS sender_host,
                NULL::integer AS sender_port,
                NULL::text AS conninfo
            FROM pg_stat_replication
            ORDER BY pid
        `

		rows, err := monitoredConn.Query(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("failed to execute query: %w", err)
		}
		defer rows.Close()

		metrics, err = utils.ScanRowsToMaps(rows)
		if err != nil {
			return nil, err
		}
	}

	// Also check if there's a wal receiver (for standbys and cascade replicas)
	receiverQuery := `
        SELECT
            'standby' AS role,
            pid,
            NULL::oid AS usesysid,
            NULL::text AS usename,
            NULL::text AS application_name,
            NULL::inet AS client_addr,
            NULL::text AS client_hostname,
            NULL::integer AS client_port,
            NULL::timestamptz AS backend_start,
            NULL::text AS backend_xmin,
            NULL::text AS state,
            NULL::text AS sent_lsn,
            NULL::text AS write_lsn,
            NULL::text AS flush_lsn,
            NULL::text AS replay_lsn,
            NULL::interval AS write_lag,
            NULL::interval AS flush_lag,
            NULL::interval AS replay_lag,
            NULL::integer AS sync_priority,
            NULL::text AS sync_state,
            NULL::timestamptz AS reply_time,
            -- Receiver columns
            pid AS receiver_pid,
            status AS receiver_status,
            receive_start_lsn::text,
            receive_start_tli,
            written_lsn::text,
            flushed_lsn::text AS receiver_flushed_lsn,
            received_tli,
            last_msg_send_time,
            last_msg_receipt_time,
            latest_end_lsn::text,
            latest_end_time,
            slot_name AS receiver_slot_name,
            sender_host,
            sender_port,
            conninfo
        FROM pg_stat_wal_receiver
    `

	receiverRows, err := monitoredConn.Query(ctx, receiverQuery)
	if err != nil {
		// If query fails (e.g., view doesn't exist), just continue with what we have
		return metrics, nil
	}
	defer receiverRows.Close()

	receiverMetrics, err := utils.ScanRowsToMaps(receiverRows)
	if err != nil {
		return metrics, nil
	}

	// Append receiver metrics to primary metrics
	metrics = append(metrics, receiverMetrics...)

	return metrics, nil
}

// Store stores the collected metrics in the datastore
func (p *PgStatReplicationProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
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
		"role",
		"pid", "usesysid", "usename", "application_name",
		"client_addr", "client_hostname", "client_port",
		"backend_start", "backend_xmin", "state",
		"sent_lsn", "write_lsn", "flush_lsn", "replay_lsn",
		"write_lag", "flush_lag", "replay_lag",
		"sync_priority", "sync_state", "reply_time",
		"receiver_pid", "receiver_status", "receive_start_lsn", "receive_start_tli",
		"written_lsn", "receiver_flushed_lsn", "received_tli",
		"last_msg_send_time", "last_msg_receipt_time",
		"latest_end_lsn", "latest_end_time",
		"receiver_slot_name", "sender_host", "sender_port", "conninfo",
	}

	// Build values array
	var values [][]interface{}
	for _, metric := range metrics {
		row := []interface{}{
			connectionID,
			timestamp,
			metric["role"],
			metric["pid"],
			metric["usesysid"],
			metric["usename"],
			metric["application_name"],
			metric["client_addr"],
			metric["client_hostname"],
			metric["client_port"],
			metric["backend_start"],
			metric["backend_xmin"],
			metric["state"],
			metric["sent_lsn"],
			metric["write_lsn"],
			metric["flush_lsn"],
			metric["replay_lsn"],
			metric["write_lag"],
			metric["flush_lag"],
			metric["replay_lag"],
			metric["sync_priority"],
			metric["sync_state"],
			metric["reply_time"],
			metric["receiver_pid"],
			metric["receiver_status"],
			metric["receive_start_lsn"],
			metric["receive_start_tli"],
			metric["written_lsn"],
			metric["receiver_flushed_lsn"],
			metric["received_tli"],
			metric["last_msg_send_time"],
			metric["last_msg_receipt_time"],
			metric["latest_end_lsn"],
			metric["latest_end_time"],
			metric["receiver_slot_name"],
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
func (p *PgStatReplicationProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
