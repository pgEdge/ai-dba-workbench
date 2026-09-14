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
            blk_write_time,
            NULL::timestamptz AS stats_reset
        FROM pg_stat_statements
        ORDER BY total_exec_time DESC
        LIMIT %d
    `, PgStatStatementsQueryLimit)
}

// checkHasColumn reports whether the pg_stat_statements view that the
// probe's own queries will reach through the search path exposes the
// named column. The extension is installed into the current schema at
// CREATE EXTENSION time, normally public, and is often relocated on
// managed services, so the view is resolved with pg_table_is_visible
// rather than by assuming a schema: filtering information_schema on
// table_schema = 'pg_catalog' matched nothing on an ordinary install and
// silently dropped every probe onto the PostgreSQL 12 query shape (#439).
func (p *PgStatStatementsProbe) checkHasColumn(ctx context.Context, conn *pgxpool.Conn, column string) (bool, error) {
	var hasColumn bool
	err := conn.QueryRow(ctx, `
        SELECT EXISTS (
            SELECT 1
            FROM pg_catalog.pg_attribute a
            JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
            WHERE c.relname = 'pg_stat_statements'
              AND c.relkind = 'v'
              AND pg_catalog.pg_table_is_visible(c.oid)
              AND a.attname = $1
              AND a.attnum > 0
              AND NOT a.attisdropped
        )
    `, column).Scan(&hasColumn)

	if err != nil {
		return false, fmt.Errorf("failed to check for %s column: %w", column, err)
	}

	return hasColumn, nil
}

// checkHasSharedBlkTime checks if pg_stat_statements has the
// shared_blk_read_time column (PostgreSQL 17 and later).
func (p *PgStatStatementsProbe) checkHasSharedBlkTime(ctx context.Context, conn *pgxpool.Conn) (bool, error) {
	return p.checkHasColumn(ctx, conn, "shared_blk_read_time")
}

// checkHasBlkReadTime checks if pg_stat_statements has the blk_read_time
// column (PostgreSQL 13 to 16).
func (p *PgStatStatementsProbe) checkHasBlkReadTime(ctx context.Context, conn *pgxpool.Conn) (bool, error) {
	return p.checkHasColumn(ctx, conn, "blk_read_time")
}

// checkHasStatsInfoView reports whether the pg_stat_statements_info view
// is visible on the monitored connection's search path. The view arrived
// with pg_stat_statements 1.9 (PostgreSQL 14), but the extension version
// is what matters, not the server version, so the catalog is consulted
// directly. Visibility rather than a fixed schema is checked because the
// extension may be installed in any schema and the probe query names the
// view unqualified.
func (p *PgStatStatementsProbe) checkHasStatsInfoView(ctx context.Context, conn *pgxpool.Conn) (bool, error) {
	var hasView bool
	err := conn.QueryRow(ctx, `
        SELECT EXISTS (
            SELECT 1
            FROM pg_catalog.pg_class c
            WHERE c.relname = 'pg_stat_statements_info'
              AND c.relkind = 'v'
              AND pg_catalog.pg_table_is_visible(c.oid)
        )
    `).Scan(&hasView)

	if err != nil {
		return false, fmt.Errorf("failed to check for pg_stat_statements_info view: %w", err)
	}

	return hasView, nil
}

// statsResetSelect returns the SELECT-list expression that yields the
// stats_reset column: the live value from pg_stat_statements_info where
// the view exists, otherwise a typed NULL so every query variant has the
// same shape.
func statsResetSelect(hasStatsInfo bool) string {
	if hasStatsInfo {
		return "(SELECT stats_reset FROM pg_stat_statements_info) AS stats_reset"
	}
	return "NULL::timestamptz AS stats_reset"
}

// Execute runs the probe against a monitored connection
func (p *PgStatStatementsProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]any, error) {
	// Check if extension is available (cached)
	available, err := cachedCheck(connectionName, "pg_stat_statements_ext", func() (bool, error) {
		return CheckExtensionExists(ctx, connectionName, monitoredConn, "pg_stat_statements")
	})
	if err != nil {
		return nil, err
	}

	if !available {
		// Extension not available, return empty metrics (not an error)
		return []map[string]any{}, nil
	}

	// Check if we have the new shared_blk_read_time column (PG 17+) (cached)
	hasSharedBlkTime, err := cachedCheck(connectionName, "pg_stat_statements_shared_blk_time", func() (bool, error) {
		return p.checkHasSharedBlkTime(ctx, monitoredConn)
	})
	if err != nil {
		return nil, err
	}

	// Check if we have the blk_read_time column (PG 13-16) (cached)
	hasBlkReadTime := false
	if !hasSharedBlkTime {
		hasBlkReadTime, err = cachedCheck(connectionName, "pg_stat_statements_blk_read_time", func() (bool, error) {
			return p.checkHasBlkReadTime(ctx, monitoredConn)
		})
		if err != nil {
			return nil, err
		}
	}

	// Check if the pg_stat_statements_info view exists (extension 1.9+,
	// PostgreSQL 14+) so stats_reset can be captured (cached)
	hasStatsInfo, err := cachedCheck(connectionName, "pg_stat_statements_info_view", func() (bool, error) {
		return p.checkHasStatsInfoView(ctx, monitoredConn)
	})
	if err != nil {
		return nil, err
	}
	statsResetExpr := statsResetSelect(hasStatsInfo)

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
                local_blk_write_time,
                %s
            FROM pg_stat_statements
            ORDER BY total_exec_time DESC
            LIMIT %d
        `, statsResetExpr, PgStatStatementsQueryLimit)
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
                NULL::double precision AS local_blk_write_time,
                %s
            FROM pg_stat_statements
            ORDER BY total_exec_time DESC
            LIMIT %d
        `, statsResetExpr, PgStatStatementsQueryLimit)
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
                NULL::double precision AS local_blk_write_time,
                %s
            FROM pg_stat_statements
            ORDER BY total_exec_time DESC
            LIMIT %d
        `, statsResetExpr, PgStatStatementsQueryLimit)
	}

	wrappedQuery := WrapQuery(ProbeNamePgStatStatements, query)
	rows, err := monitoredConn.Query(ctx, wrappedQuery)
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
		"stats_reset",
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
			metric["stats_reset"],
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

	// Store metrics
	if err := StoreMetrics(ctx, datastoreConn, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store metrics: %w", err)
	}

	return nil
}
