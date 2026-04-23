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

// PgDatabaseProbe collects metrics from pg_database catalog
type PgDatabaseProbe struct {
	BaseMetricsProbe
}

// NewPgDatabaseProbe creates a new pg_database probe
func NewPgDatabaseProbe(config *ProbeConfig) *PgDatabaseProbe {
	return &PgDatabaseProbe{
		BaseMetricsProbe: BaseMetricsProbe{config: config},
	}
}

// GetQuery returns the SQL query to execute (default for PG16+)
func (p *PgDatabaseProbe) GetQuery() string {
	return p.GetQueryForVersion(16)
}

// GetQueryForVersion returns the appropriate SQL query for the given PostgreSQL version
func (p *PgDatabaseProbe) GetQueryForVersion(pgVersion int) string {
	datlocprovider := "datlocprovider"
	if pgVersion < 16 {
		// datlocprovider was introduced in PostgreSQL 16
		datlocprovider = "NULL AS datlocprovider"
	}

	return fmt.Sprintf(`
        SELECT
            datname,
            datdba,
            encoding,
            %s,
            datistemplate,
            datallowconn,
            datconnlimit,
            datfrozenxid,
            datminmxid,
            dattablespace,
            age(datfrozenxid) AS age_datfrozenxid,
            mxid_age(datminmxid) AS age_datminmxid,
            pg_database_size(datname) AS database_size_bytes
        FROM pg_database
    `, datlocprovider)
}

// Execute runs the probe against a monitored connection
func (p *PgDatabaseProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]any, error) {
	query := WrapQuery(ProbeNamePgDatabase, p.GetQueryForVersion(pgVersion))
	rows, err := monitoredConn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store stores the collected metrics in the datastore
func (p *PgDatabaseProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]any) error {
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
		"datname", "datdba", "encoding", "datlocprovider",
		"datistemplate", "datallowconn", "datconnlimit",
		"datfrozenxid", "datminmxid", "dattablespace",
		"age_datfrozenxid", "age_datminmxid", "database_size_bytes",
	}

	// Build values array
	var values [][]any
	for _, metric := range metrics {
		row := []any{
			connectionID,
			timestamp,
			metric["datname"],
			metric["datdba"],
			metric["encoding"],
			metric["datlocprovider"],
			metric["datistemplate"],
			metric["datallowconn"],
			metric["datconnlimit"],
			metric["datfrozenxid"],
			metric["datminmxid"],
			metric["dattablespace"],
			metric["age_datfrozenxid"],
			metric["age_datminmxid"],
			metric["database_size_bytes"],
		}
		values = append(values, row)
	}

	// Use COPY protocol to store metrics
	if err := StoreMetricsWithCopy(ctx, datastoreConn, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store metrics: %w", err)
	}

	return nil
}
