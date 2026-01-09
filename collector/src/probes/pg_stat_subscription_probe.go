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

// PgStatSubscriptionProbe collects metrics from pg_stat_subscription view
type PgStatSubscriptionProbe struct {
	BaseMetricsProbe
}

// NewPgStatSubscriptionProbe creates a new pg_stat_subscription probe
func NewPgStatSubscriptionProbe(config *ProbeConfig) *PgStatSubscriptionProbe {
	return &PgStatSubscriptionProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetName returns the probe name
func (p *PgStatSubscriptionProbe) GetName() string {
	return ProbeNamePgStatSubscription
}

// GetTableName returns the metrics table name
func (p *PgStatSubscriptionProbe) GetTableName() string {
	return ProbeNamePgStatSubscription
}

// IsDatabaseScoped returns false as pg_stat_subscription is server-scoped
func (p *PgStatSubscriptionProbe) IsDatabaseScoped() bool {
	return false
}

// GetQuery returns the SQL query to execute (for PG 16+)
// Version-specific queries are handled in Execute()
func (p *PgStatSubscriptionProbe) GetQuery() string {
	return `
        SELECT
            subid,
            subname,
            worker_type,
            pid,
            leader_pid,
            relid,
            received_lsn::text,
            last_msg_send_time,
            last_msg_receipt_time,
            latest_end_lsn::text,
            latest_end_time
        FROM pg_stat_subscription
    `
}

// checkHasWorkerType checks if the pg_stat_subscription view has worker_type column (PG 16+)
func (p *PgStatSubscriptionProbe) checkHasWorkerType(ctx context.Context, conn *pgxpool.Conn) (bool, error) {
	var hasColumn bool
	err := conn.QueryRow(ctx, `
        SELECT EXISTS (
            SELECT 1
            FROM information_schema.columns
            WHERE table_schema = 'pg_catalog'
              AND table_name = 'pg_stat_subscription'
              AND column_name = 'worker_type'
        )
    `).Scan(&hasColumn)

	if err != nil {
		return false, fmt.Errorf("failed to check for worker_type column: %w", err)
	}

	return hasColumn, nil
}

// Execute runs the probe against a monitored connection
func (p *PgStatSubscriptionProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]interface{}, error) {
	// Check if we have worker_type column (PG 16+)
	hasWorkerType, err := p.checkHasWorkerType(ctx, monitoredConn)
	if err != nil {
		return nil, err
	}

	var query string
	if hasWorkerType {
		// PostgreSQL 16+ with worker_type and leader_pid columns
		query = `
            SELECT
                subid,
                subname,
                worker_type,
                pid,
                leader_pid,
                relid,
                received_lsn::text,
                last_msg_send_time,
                last_msg_receipt_time,
                latest_end_lsn::text,
                latest_end_time
            FROM pg_stat_subscription
        `
	} else {
		// PostgreSQL < 16 without worker_type and leader_pid columns
		query = `
            SELECT
                subid,
                subname,
                NULL::text AS worker_type,
                pid,
                NULL::integer AS leader_pid,
                relid,
                received_lsn::text,
                last_msg_send_time,
                last_msg_receipt_time,
                latest_end_lsn::text,
                latest_end_time
            FROM pg_stat_subscription
        `
	}

	rows, err := monitoredConn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatSubscriptionProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]interface{}) error {
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
		"subid", "subname", "worker_type", "pid", "leader_pid", "relid",
		"received_lsn", "last_msg_send_time", "last_msg_receipt_time",
		"latest_end_lsn", "latest_end_time",
	}

	// Build values array
	var values [][]interface{}
	for _, metric := range metrics {
		row := []interface{}{
			connectionID,
			timestamp,
			metric["subid"],
			metric["subname"],
			metric["worker_type"],
			metric["pid"],
			metric["leader_pid"],
			metric["relid"],
			metric["received_lsn"],
			metric["last_msg_send_time"],
			metric["last_msg_receipt_time"],
			metric["latest_end_lsn"],
			metric["latest_end_time"],
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
func (p *PgStatSubscriptionProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
