/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ConnectionPurgeBatchSize is the number of metric rows
// PurgeConnectionMetrics deletes per statement. Each batch commits on
// its own, so it must stay well inside DefaultDatastoreStatementTimeout
// on a slow datastore; ten thousand rows take a fraction of a second.
const ConnectionPurgeBatchSize = 10000

// metricsPurgeTargetsQuery lists every leaf table under the metrics
// schema that holds a single-column foreign key to connections, with
// the name of that column. Partitioned tables carry the constraint on
// the parent (conparentid = 0) and on clones in each partition, so the
// parent's constraint is expanded to its leaves with pg_partition_tree.
// pg_partition_tree returns nothing for a table outside any partition
// tree, so an ordinary table (relkind 'r') is added as its own leaf.
// connections is resolved through search_path, as DeleteConnection's
// unqualified DELETE resolves it. The table and column
// names are read from the catalog and quoted with pgx.Identifier before
// use, never taken from a caller.
const metricsPurgeTargetsQuery = `
    SELECT ln.nspname, lc.relname, a.attname
    FROM pg_constraint con
    JOIN pg_class pc ON pc.oid = con.conrelid
    JOIN pg_namespace pn ON pn.oid = pc.relnamespace
    JOIN pg_attribute a ON a.attrelid = con.conrelid
                       AND a.attnum = con.conkey[1]
    CROSS JOIN LATERAL (
        SELECT relid FROM pg_partition_tree(con.conrelid) WHERE isleaf
        UNION
        SELECT con.conrelid WHERE pc.relkind = 'r'
    ) pt
    JOIN pg_class lc ON lc.oid = pt.relid
    JOIN pg_namespace ln ON ln.oid = lc.relnamespace
    WHERE con.contype = 'f'
      AND con.conparentid = 0
      AND con.confrelid = 'connections'::regclass
      AND cardinality(con.conkey) = 1
      AND pn.nspname = 'metrics'
    ORDER BY ln.nspname, lc.relname`

// metricsPurgeTarget is one leaf table PurgeConnectionMetrics clears.
type metricsPurgeTarget struct {
	schema string
	table  string
	column string
}

// batchDeleteSQL returns the statement that deletes up to $2 rows of
// the target belonging to connection $1. Matching on ctid lets the
// LIMIT bound the delete; ONLY is safe because the target is a leaf.
func (t metricsPurgeTarget) batchDeleteSQL() string {
	table := pgx.Identifier{t.schema, t.table}.Sanitize()
	column := pgx.Identifier{t.column}.Sanitize()
	return fmt.Sprintf(`DELETE FROM ONLY %s WHERE ctid = ANY(ARRAY(
        SELECT ctid FROM ONLY %s WHERE %s = $1 LIMIT $2))`,
		table, table, column)
}

// DisableConnectionMonitoring sets is_monitored to false on a
// connection, so that the collector stops writing new metrics for it
// whilst PurgeConnectionMetrics clears the old ones. It returns
// ErrConnectionNotFound when no such connection exists.
func (d *Datastore) DisableConnectionMonitoring(ctx context.Context, id int) error {
	tag, err := d.pool.Exec(ctx, `
        UPDATE connections
        SET is_monitored = FALSE,
            updated_at = CURRENT_TIMESTAMP
        WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to disable monitoring for connection: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConnectionNotFound
	}
	return nil
}

// PurgeConnectionMetrics deletes a connection's rows from every
// metrics table, ConnectionPurgeBatchSize rows at a time, each batch in
// its own transaction (issue #669). DeleteConnection would remove the
// same rows through ON DELETE CASCADE, but in one transaction whose
// length grows with the connection's retention history and eventually
// outlasts any deadline; clearing them first in short batches leaves
// that cascade almost nothing to do, and because every batch commits,
// an interrupted purge keeps its progress and a retry resumes it.
//
// Like DeleteConnection it does not take d.mu, for the same reason: it
// guards no Go state here, and holding it would stall other calls.
func (d *Datastore) PurgeConnectionMetrics(ctx context.Context, id int) error {
	targets, err := d.metricsPurgeTargets(ctx)
	if err != nil {
		return err
	}
	for _, target := range targets {
		if err := d.purgeMetricsTarget(ctx, target, id); err != nil {
			return err
		}
	}
	return nil
}

// metricsPurgeTargets reads the leaf tables PurgeConnectionMetrics
// clears from the catalog.
func (d *Datastore) metricsPurgeTargets(ctx context.Context) ([]metricsPurgeTarget, error) {
	rows, err := d.pool.Query(ctx, metricsPurgeTargetsQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to list metrics tables to purge: %w", err)
	}
	targets, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (metricsPurgeTarget, error) {
		var t metricsPurgeTarget
		err := row.Scan(&t.schema, &t.table, &t.column)
		return t, err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to read metrics tables to purge: %w", err)
	}
	return targets, nil
}

// purgeMetricsTarget deletes connection id's rows from one leaf table
// in batches until a batch finds none. It stops on a batch that deletes
// nothing rather than on a short batch, so rows the collector inserts
// whilst it runs are still caught.
func (d *Datastore) purgeMetricsTarget(ctx context.Context, target metricsPurgeTarget, id int) error {
	query := target.batchDeleteSQL()
	for {
		tag, err := d.pool.Exec(ctx, query, id, ConnectionPurgeBatchSize)
		if err != nil {
			return fmt.Errorf("failed to purge %s.%s for connection: %w",
				target.schema, target.table, err)
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
	}
}
