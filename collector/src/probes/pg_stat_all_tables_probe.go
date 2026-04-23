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

// PgStatAllTablesProbe collects metrics from pg_stat_all_tables view
// and joins with pg_statio_all_tables for I/O statistics
type PgStatAllTablesProbe struct {
	BaseMetricsProbe
}

// NewPgStatAllTablesProbe creates a new pg_stat_all_tables probe
func NewPgStatAllTablesProbe(config *ProbeConfig) *PgStatAllTablesProbe {
	return &PgStatAllTablesProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config, databaseScoped: true},
	}
}

// GetQuery returns the SQL query to execute
func (p *PgStatAllTablesProbe) GetQuery() string {
	return `
        SELECT
            s.schemaname,
            s.relname,
            s.seq_scan,
            s.seq_tup_read,
            s.idx_scan,
            s.idx_tup_fetch,
            s.n_tup_ins,
            s.n_tup_upd,
            s.n_tup_del,
            s.n_tup_hot_upd,
            s.n_live_tup,
            s.n_dead_tup,
            s.n_mod_since_analyze,
            s.last_vacuum,
            s.last_autovacuum,
            s.last_analyze,
            s.last_autoanalyze,
            s.vacuum_count,
            s.autovacuum_count,
            s.analyze_count,
            s.autoanalyze_count,
            io.heap_blks_read,
            io.heap_blks_hit,
            io.idx_blks_read,
            io.idx_blks_hit,
            io.toast_blks_read,
            io.toast_blks_hit,
            io.tidx_blks_read,
            io.tidx_blks_hit
        FROM pg_stat_all_tables s
        LEFT JOIN pg_statio_all_tables io ON s.relid = io.relid
        ORDER BY s.schemaname, s.relname
    `
}

// Execute runs the probe against a monitored connection
func (p *PgStatAllTablesProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]any, error) {
	query := WrapQuery(ProbeNamePgStatAllTables, p.GetQuery())
	rows, err := monitoredConn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatAllTablesProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]any) error {
	if len(metrics) == 0 {
		return nil // Nothing to store
	}

	// Ensure partition exists for this timestamp
	if err := p.EnsurePartition(ctx, datastoreConn, timestamp); err != nil {
		return fmt.Errorf("failed to ensure partition: %w", err)
	}

	// Define columns in order
	columns := []string{
		"connection_id", "collected_at", "database_name",
		"schemaname", "relname",
		"seq_scan", "seq_tup_read", "idx_scan", "idx_tup_fetch",
		"n_tup_ins", "n_tup_upd", "n_tup_del", "n_tup_hot_upd",
		"n_live_tup", "n_dead_tup", "n_mod_since_analyze",
		"last_vacuum", "last_autovacuum", "last_analyze", "last_autoanalyze",
		"vacuum_count", "autovacuum_count", "analyze_count", "autoanalyze_count",
		"heap_blks_read", "heap_blks_hit",
		"idx_blks_read", "idx_blks_hit",
		"toast_blks_read", "toast_blks_hit",
		"tidx_blks_read", "tidx_blks_hit",
	}

	// Build values array
	var values [][]any
	for _, metric := range metrics {
		// Extract database_name from the metric (set by scheduler)
		databaseName, ok := metric["_database_name"]
		if !ok {
			return fmt.Errorf("database_name not found in metrics")
		}

		row := []any{
			connectionID,
			timestamp,
			databaseName,
			metric["schemaname"],
			metric["relname"],
			metric["seq_scan"],
			metric["seq_tup_read"],
			metric["idx_scan"],
			metric["idx_tup_fetch"],
			metric["n_tup_ins"],
			metric["n_tup_upd"],
			metric["n_tup_del"],
			metric["n_tup_hot_upd"],
			metric["n_live_tup"],
			metric["n_dead_tup"],
			metric["n_mod_since_analyze"],
			metric["last_vacuum"],
			metric["last_autovacuum"],
			metric["last_analyze"],
			metric["last_autoanalyze"],
			metric["vacuum_count"],
			metric["autovacuum_count"],
			metric["analyze_count"],
			metric["autoanalyze_count"],
			metric["heap_blks_read"],
			metric["heap_blks_hit"],
			metric["idx_blks_read"],
			metric["idx_blks_hit"],
			metric["toast_blks_read"],
			metric["toast_blks_hit"],
			metric["tidx_blks_read"],
			metric["tidx_blks_hit"],
		}
		values = append(values, row)
	}

	// Store metrics
	if err := StoreMetrics(ctx, datastoreConn, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store metrics: %w", err)
	}

	return nil
}
