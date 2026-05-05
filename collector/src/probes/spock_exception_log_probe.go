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

// SpockExceptionLogProbe captures rows added to spock.exception_log in a
// rolling 15-minute source-side window. The probe is database-scoped so it
// runs once per monitored database connection; pgEdge clusters typically
// register Spock in a single application database, but this scoping lets us
// pick up exception rows from any database that has the extension loaded
// without depending on cluster-level configuration.
//
// The probe short-circuits cleanly on databases where Spock is not
// installed: Execute returns (nil, nil) so the scheduler treats the
// collection as a successful no-op rather than retrying on every cycle.
type SpockExceptionLogProbe struct {
	BaseMetricsProbe
}

// NewSpockExceptionLogProbe constructs a SpockExceptionLogProbe with the
// supplied configuration. databaseScoped is set to true so the scheduler
// iterates over each user database when invoking the probe.
func NewSpockExceptionLogProbe(config *ProbeConfig) *SpockExceptionLogProbe {
	return &SpockExceptionLogProbe{
		BaseMetricsProbe: BaseMetricsProbe{
			config:         config,
			databaseScoped: true,
		},
	}
}

// GetExtensionName advertises the Spock dependency to the scheduler so the
// "extension missing" reason can be surfaced in operator-facing tooling.
// This satisfies the ExtensionProbe interface defined in base.go.
func (p *SpockExceptionLogProbe) GetExtensionName() string {
	return "spock"
}

// GetQuery returns the SQL executed against the monitored database. The
// rolling 15-minute window is anchored on retry_errored_at because that is
// the column Spock advances when an apply attempt fails; collected_at on
// the destination side is recorded separately by Store.
//
// The column list mirrors metrics.spock_exception_log so Store can pass the
// row maps straight through without reshaping. local_tup, remote_old_tup,
// and remote_new_tup are JSONB on both sides, so pgx scans them into Go
// values that the COPY/INSERT path serializes back to JSONB transparently.
func (p *SpockExceptionLogProbe) GetQuery() string {
	return `
		SELECT remote_origin,
		       remote_commit_ts,
		       command_counter,
		       retry_errored_at,
		       remote_xid,
		       local_origin,
		       local_commit_ts,
		       table_schema,
		       table_name,
		       operation,
		       local_tup,
		       remote_old_tup,
		       remote_new_tup,
		       ddl_statement,
		       ddl_user,
		       error_message
		  FROM spock.exception_log
		 WHERE retry_errored_at > now() - interval '15 minutes'
	`
}

// Execute runs the rolling-window query against the monitored database
// after first verifying that the Spock extension is installed. On
// databases without Spock the call returns (nil, nil) without touching
// the catalog — the CheckExtensionExists helper caches the negative
// result so the existence query only hits the catalog once per
// connection lifetime.
func (p *SpockExceptionLogProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]any, error) {
	exists, err := CheckExtensionExists(ctx, connectionName, monitoredConn, "spock")
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	query := WrapQuery(ProbeNameSpockExceptionLog, p.GetQuery())
	rows, err := monitoredConn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute spock_exception_log query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store writes the captured rows to metrics.spock_exception_log. Empty
// metric slices short-circuit before any datastore work so the scheduler
// can call Store unconditionally after Execute.
//
// The column order matches the v3 migration (collector/src/database/
// schema.go) and the surface contract documented on GetQuery; do not
// reorder or rename columns without updating both call sites.
func (p *SpockExceptionLogProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]any) error {
	if len(metrics) == 0 {
		return nil
	}

	if err := p.EnsurePartition(ctx, datastoreConn, timestamp); err != nil {
		return fmt.Errorf("failed to ensure partition: %w", err)
	}

	columns := []string{
		"connection_id", "collected_at",
		"remote_origin", "remote_commit_ts", "command_counter",
		"retry_errored_at", "remote_xid", "local_origin",
		"local_commit_ts", "table_schema", "table_name", "operation",
		"local_tup", "remote_old_tup", "remote_new_tup",
		"ddl_statement", "ddl_user", "error_message",
	}

	values := make([][]any, 0, len(metrics))
	for _, m := range metrics {
		values = append(values, []any{
			connectionID,
			timestamp,
			m["remote_origin"],
			m["remote_commit_ts"],
			m["command_counter"],
			m["retry_errored_at"],
			m["remote_xid"],
			m["local_origin"],
			m["local_commit_ts"],
			m["table_schema"],
			m["table_name"],
			m["operation"],
			m["local_tup"],
			m["remote_old_tup"],
			m["remote_new_tup"],
			m["ddl_statement"],
			m["ddl_user"],
			m["error_message"],
		})
	}

	if err := StoreMetrics(ctx, datastoreConn, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store spock_exception_log metrics: %w", err)
	}
	return nil
}
