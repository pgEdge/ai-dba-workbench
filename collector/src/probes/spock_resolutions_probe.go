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

// SpockResolutionsProbe captures rows added to spock.resolutions in a
// rolling 15-minute source-side window. The probe is database-scoped so
// it runs once per monitored database connection; pgEdge clusters
// typically register Spock in a single application database, but this
// scoping lets us pick up resolution rows from any database that has the
// extension loaded without depending on cluster-level configuration.
//
// The probe short-circuits cleanly on databases where Spock is not
// installed: Execute returns (nil, nil) so the scheduler treats the
// collection as a successful no-op rather than retrying on every cycle.
type SpockResolutionsProbe struct {
	BaseMetricsProbe
}

// NewSpockResolutionsProbe constructs a SpockResolutionsProbe with the
// supplied configuration. databaseScoped is set to true so the
// scheduler iterates over each user database when invoking the probe.
func NewSpockResolutionsProbe(config *ProbeConfig) *SpockResolutionsProbe {
	return &SpockResolutionsProbe{
		BaseMetricsProbe: BaseMetricsProbe{
			config:         config,
			databaseScoped: true,
		},
	}
}

// GetExtensionName advertises the Spock dependency to the scheduler so
// the "extension missing" reason can be surfaced in operator-facing
// tooling. This satisfies the ExtensionProbe interface defined in
// base.go.
func (p *SpockResolutionsProbe) GetExtensionName() string {
	return "spock"
}

// GetQuery returns the SQL executed against the monitored database.
// The rolling 15-minute window is anchored on log_time because that is
// the column Spock advances when a conflict is auto-resolved on the
// receiving node; collected_at on the destination side is recorded
// separately by Store.
//
// xid and pg_lsn values are projected through ::text casts so pgx scans
// them into Go strings rather than the dedicated PostgreSQL types,
// matching the TEXT columns used by metrics.spock_resolutions on the
// datastore side. The column list mirrors the v3 migration so Store
// can pass row maps straight through without reshaping.
func (p *SpockResolutionsProbe) GetQuery() string {
	return `
		SELECT id,
		       node_name,
		       log_time,
		       relname,
		       idxname,
		       conflict_type,
		       conflict_resolution,
		       local_origin,
		       local_tuple,
		       local_xid::text  AS local_xid,
		       local_timestamp,
		       remote_origin,
		       remote_tuple,
		       remote_xid::text AS remote_xid,
		       remote_timestamp,
		       remote_lsn::text AS remote_lsn
		  FROM spock.resolutions
		 WHERE log_time > now() - interval '15 minutes'
	`
}

// Execute runs the rolling-window query against the monitored database
// after first verifying that the Spock extension is installed. On
// databases without Spock the call returns (nil, nil) without touching
// the catalog further; the CheckExtensionExists helper caches the
// negative result so the existence query only hits the catalog once
// per connection lifetime.
func (p *SpockResolutionsProbe) Execute(ctx context.Context, connectionName string, monitoredConn *pgxpool.Conn, pgVersion int) ([]map[string]any, error) {
	exists, err := CheckExtensionExists(ctx, connectionName, monitoredConn, "spock")
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	query := WrapQuery(ProbeNameSpockResolutions, p.GetQuery())
	rows, err := monitoredConn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute spock_resolutions query: %w", err)
	}
	defer rows.Close()

	return utils.ScanRowsToMaps(rows)
}

// Store writes the captured rows to metrics.spock_resolutions. Empty
// metric slices short-circuit before any datastore work so the
// scheduler can call Store unconditionally after Execute.
//
// The column order matches the v3 migration (collector/src/database/
// schema.go) and the surface contract documented on GetQuery; do not
// reorder or rename columns without updating both call sites. xid and
// pg_lsn values arrive as Go strings (already cast to text on the
// source side) and land in TEXT columns on the destination, avoiding
// any pgx type negotiation in the COPY path.
func (p *SpockResolutionsProbe) Store(ctx context.Context, datastoreConn *pgxpool.Conn, connectionID int, timestamp time.Time, metrics []map[string]any) error {
	if len(metrics) == 0 {
		return nil
	}

	if err := p.EnsurePartition(ctx, datastoreConn, timestamp); err != nil {
		return fmt.Errorf("failed to ensure partition: %w", err)
	}

	columns := []string{
		"connection_id", "collected_at",
		"id", "node_name", "log_time", "relname", "idxname",
		"conflict_type", "conflict_resolution",
		"local_origin", "local_tuple", "local_xid", "local_timestamp",
		"remote_origin", "remote_tuple", "remote_xid", "remote_timestamp",
		"remote_lsn",
	}

	values := make([][]any, 0, len(metrics))
	for _, m := range metrics {
		values = append(values, []any{
			connectionID,
			timestamp,
			m["id"],
			m["node_name"],
			m["log_time"],
			m["relname"],
			m["idxname"],
			m["conflict_type"],
			m["conflict_resolution"],
			m["local_origin"],
			m["local_tuple"],
			m["local_xid"],
			m["local_timestamp"],
			m["remote_origin"],
			m["remote_tuple"],
			m["remote_xid"],
			m["remote_timestamp"],
			m["remote_lsn"],
		})
	}

	if err := StoreMetrics(ctx, datastoreConn, p.GetTableName(), columns, values); err != nil {
		return fmt.Errorf("failed to store spock_resolutions metrics: %w", err)
	}
	return nil
}
