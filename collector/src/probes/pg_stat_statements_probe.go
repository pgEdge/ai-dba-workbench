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
	"github.com/pgedge/ai-workbench/pkg/logger"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/collector/src/utils"
)

// PgStatStatementsProbe collects metrics from pg_stat_statements extension
type PgStatStatementsProbe struct {
	BaseMetricsProbe
}

// NewPgStatStatementsProbe creates a new pg_stat_statements probe
func NewPgStatStatementsProbe(config *ProbeConfig) *PgStatStatementsProbe {
	return &PgStatStatementsProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config, databaseScoped: true},
	}
}

// GetExtensionName returns the required extension name
func (p *PgStatStatementsProbe) GetExtensionName() string {
	return "pg_stat_statements"
}

// GetQuery returns the SQL query to execute (for PG <17)
// Version-specific queries are handled in Execute()
func (p *PgStatStatementsProbe) GetQuery() string {
	return fmt.Sprintf(`
        SELECT
            userid,
            dbid,
            queryid,
            query,
            calls,
            total_exec_time,
            mean_exec_time,
            min_exec_time,
            max_exec_time,
            stddev_exec_time,
            rows,
            shared_blks_hit,
            shared_blks_read,
            shared_blks_dirtied,
            shared_blks_written,
            local_blks_hit,
            local_blks_read,
            local_blks_dirtied,
            local_blks_written,
            temp_blks_read,
            temp_blks_written,
            blk_read_time,
            blk_write_time
        FROM pg_stat_statements
        ORDER BY total_exec_time DESC
        LIMIT %d
    `, PgStatStatementsQueryLimit)
}

// checkHasSharedBlkTime checks if pg_stat_statements has shared_blk_read_time column (PG 17+)
func (p *PgStatStatementsProbe) checkHasSharedBlkTime(ctx context.Context, conn *pgxpool.Conn) (bool, error) {
	var hasColumn bool
	err := conn.QueryRow(ctx, `
        SELECT EXISTS (
            SELECT 1
            FROM information_schema.columns
            WHERE table_schema = 'pg_catalog'
              AND table_name = 'pg_stat_statements'
              AND column_name = 'shared_blk_read_time'
        )
    `).Scan(&hasColumn)

	if err != nil {
		return false, fmt.Errorf("failed to check for shared_blk_read_time column: %w", err)
	}

	return hasColumn, nil
}

// checkHasBlkReadTime checks if pg_stat_statements has blk_read_time column (PG 13-16)
func (p *PgStatStatementsProbe) checkHasBlkReadTime(ctx context.Context, conn *pgxpool.Conn) (bool, error) {
	var hasColumn bool
	err := conn.QueryRow(ctx, `
        SELECT EXISTS (
            SELECT 1
            FROM information_schema.columns
            WHERE table_schema = 'pg_catalog'
              AND table_name = 'pg_stat_statements'
              AND column_name = 'blk_read_time'
        )
    `).Scan(&hasColumn)

	if err != nil {
		return false, fmt.Errorf("failed to check for blk_read_time column: %w", err)
	}

	return hasColumn, nil
}

// checkExtensionAvailable checks if pg_stat_statements extension is installed
func (p *PgStatStatementsProbe) checkExtensionAvailable(ctx context.Context, conn *pgxpool.Conn) (bool, error) {
	var exists bool
	err := conn.QueryRow(ctx, `
        SELECT EXISTS(
            SELECT 1
            FROM pg_extension
            WHERE extname = 'pg_stat_statements'
        )
    `).Scan(&exists)

	if err != nil {
		return false, fmt.Errorf("failed to check for pg_stat_statements extension: %w", err)
	}

	return exists, nil
}

// Execute runs the probe against a monitored connection
func (p *PgStatStatementsProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]any, error) {
	// Check if extension is available
	available, err := p.checkExtensionAvailable(ctx, monitoredConn)
	if err != nil {
		return nil, err
	}

	if !available {
		// Extension not available, return empty metrics (not an error)
		return []map[string]any{}, nil
	}

	// Check if we have the new shared_blk_read_time column (PG 17+)
	hasSharedBlkTime, err := p.checkHasSharedBlkTime(ctx, monitoredConn)
	if err != nil {
		return nil, err
	}

	// Check if we have the blk_read_time column (PG 13-16)
	hasBlkReadTime := false
	if !hasSharedBlkTime {
		hasBlkReadTime, err = p.checkHasBlkReadTime(ctx, monitoredConn)
		if err != nil {
			return nil, err
		}
	}

	var query string
	if hasSharedBlkTime {
		// PostgreSQL 17+ with new timing column names
		query = fmt.Sprintf(`
            SELECT
                userid,
                dbid,
                queryid,
                toplevel,
                query,
                calls,
                total_exec_time,
                mean_exec_time,
                min_exec_time,
                max_exec_time,
                stddev_exec_time,
                rows,
                shared_blks_hit,
                shared_blks_read,
                shared_blks_dirtied,
                shared_blks_written,
                local_blks_hit,
                local_blks_read,
                local_blks_dirtied,
                local_blks_written,
                temp_blks_read,
                temp_blks_written,
                shared_blk_read_time,
                shared_blk_write_time,
                local_blk_read_time,
                local_blk_write_time
            FROM pg_stat_statements
            ORDER BY total_exec_time DESC
            LIMIT %d
        `, PgStatStatementsQueryLimit)
	} else if hasBlkReadTime {
		// PostgreSQL 13-16 with old timing column names
		// Map old columns to new names for consistent storage
		query = fmt.Sprintf(`
            SELECT
                userid,
                dbid,
                queryid,
                toplevel,
                query,
                calls,
                total_exec_time,
                mean_exec_time,
                min_exec_time,
                max_exec_time,
                stddev_exec_time,
                rows,
                shared_blks_hit,
                shared_blks_read,
                shared_blks_dirtied,
                shared_blks_written,
                local_blks_hit,
                local_blks_read,
                local_blks_dirtied,
                local_blks_written,
                temp_blks_read,
                temp_blks_written,
                blk_read_time AS shared_blk_read_time,
                blk_write_time AS shared_blk_write_time,
                NULL::double precision AS local_blk_read_time,
                NULL::double precision AS local_blk_write_time
            FROM pg_stat_statements
            ORDER BY total_exec_time DESC
            LIMIT %d
        `, PgStatStatementsQueryLimit)
	} else {
		// PostgreSQL 12 and earlier without timing columns or toplevel
		// Use NULL for timing columns and TRUE for toplevel (not available in PG <13)
		query = fmt.Sprintf(`
            SELECT
                userid,
                dbid,
                queryid,
                TRUE AS toplevel,
                query,
                calls,
                total_exec_time,
                mean_exec_time,
                min_exec_time,
                max_exec_time,
                stddev_exec_time,
                rows,
                shared_blks_hit,
                shared_blks_read,
                shared_blks_dirtied,
                shared_blks_written,
                local_blks_hit,
                local_blks_read,
                local_blks_dirtied,
                local_blks_written,
                temp_blks_read,
                temp_blks_written,
                NULL::double precision AS shared_blk_read_time,
                NULL::double precision AS shared_blk_write_time,
                NULL::double precision AS local_blk_read_time,
                NULL::double precision AS local_blk_write_time
            FROM pg_stat_statements
            ORDER BY total_exec_time DESC
            LIMIT %d
        `, PgStatStatementsQueryLimit)
	}

	wrappedQuery := WrapQuery(ProbeNamePgStatStatements, query)
	rows, err := monitoredConn.Query(ctx, wrappedQuery) // nosemgrep: go-sql-concat-sqli
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgStatStatementsProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]any) error {
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
		"userid", "dbid", "queryid", "toplevel", "query",
		"calls", "total_exec_time", "mean_exec_time", "min_exec_time", "max_exec_time", "stddev_exec_time",
		"rows",
		"shared_blks_hit", "shared_blks_read", "shared_blks_dirtied", "shared_blks_written",
		"local_blks_hit", "local_blks_read", "local_blks_dirtied", "local_blks_written",
		"temp_blks_read", "temp_blks_written",
		"shared_blk_read_time", "shared_blk_write_time",
		"local_blk_read_time", "local_blk_write_time",
	}

	// Build values array, filtering out rows with NULL queryid and deduplicating
	// (queryid is NULL for utility statements like VACUUM, ANALYZE, etc.)
	var values [][]any
	var skippedCount int
	var duplicateCount int

	// Track seen keys to detect duplicates using the same uniqueness constraint as PostgreSQL:
	// (database_name, queryid, userid, dbid, toplevel)
	type uniqueKey struct {
		database string
		queryid  any
		userid   any
		dbid     any
		toplevel any
	}
	seenKeys := make(map[uniqueKey]bool)

	for _, metric := range metrics {
		// Skip rows with NULL queryid as they cannot be stored with our primary key
		if metric["queryid"] == nil {
			skippedCount++
			continue
		}

		// Extract database_name from the metric (set by scheduler)
		databaseName, ok := metric["_database_name"]
		if !ok {
			return fmt.Errorf("database_name not found in metrics")
		}

		// Check for duplicates using the full uniqueness constraint
		key := uniqueKey{
			database: fmt.Sprintf("%v", databaseName),
			queryid:  metric["queryid"],
			userid:   metric["userid"],
			dbid:     metric["dbid"],
			toplevel: metric["toplevel"],
		}
		if seenKeys[key] {
			// Duplicate found - skip it and log
			duplicateCount++
			logger.Infof("Skipping duplicate row: database=%s, queryid=%v, userid=%v, dbid=%v, toplevel=%v",
				key.database, key.queryid, key.userid, key.dbid, key.toplevel)
			continue
		}
		seenKeys[key] = true

		row := []any{
			connectionID,
			timestamp,
			databaseName,
			metric["userid"],
			metric["dbid"],
			metric["queryid"],
			metric["toplevel"],
			metric["query"],
			metric["calls"],
			metric["total_exec_time"],
			metric["mean_exec_time"],
			metric["min_exec_time"],
			metric["max_exec_time"],
			metric["stddev_exec_time"],
			metric["rows"],
			metric["shared_blks_hit"],
			metric["shared_blks_read"],
			metric["shared_blks_dirtied"],
			metric["shared_blks_written"],
			metric["local_blks_hit"],
			metric["local_blks_read"],
			metric["local_blks_dirtied"],
			metric["local_blks_written"],
			metric["temp_blks_read"],
			metric["temp_blks_written"],
			metric["shared_blk_read_time"],
			metric["shared_blk_write_time"],
			metric["local_blk_read_time"],
			metric["local_blk_write_time"],
		}
		values = append(values, row)
	}

	// Log if we skipped any rows
	if skippedCount > 0 {
		logger.Infof("Skipped %d pg_stat_statements row(s) with NULL queryid (utility statements)", skippedCount)
	}
	if duplicateCount > 0 {
		logger.Infof("Skipped %d duplicate pg_stat_statements row(s)", duplicateCount)
	}

	// If all rows were filtered out, nothing to store
	if len(values) == 0 {
		return nil
	}

	// Use standard COPY protocol to store metrics
	if err := StoreMetricsWithCopy(ctx, datastoreConn, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store metrics: %w", err)
	}

	return nil
}

// EnsurePartition ensures a partition exists for the given timestamp
func (p *PgStatStatementsProbe) EnsurePartition(ctx context.Context, datastoreConn *pgxpool.Conn, timestamp time.Time) error {
	return EnsurePartition(ctx, datastoreConn, p.GetTableName(), timestamp)
}
