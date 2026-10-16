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
	"log"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pgedge/ai-workbench/pkg/rollback"
)

// Deleting a connection happens in two steps (issue #669).
// DeleteConnection marks the row (connections.deleted_at), switches off
// monitoring and alerting, and deletes the small dependent rows in one
// transaction, so the caller sees the connection gone at once. The
// ConnectionPurger below then removes the connection's metrics history
// in bounded batches, one autocommitted DELETE at a time, and finally
// deletes the row itself, which cascades whatever the collector wrote
// in the meantime. All the state the purge needs is in deleted_at, so
// a server restart part way through resumes where the last one left
// off.

const (
	// DefaultConnectionPurgeBatchSize is the number of metrics rows the
	// purge deletes per statement. Each batch is located through the
	// primary key, which leads with connection_id on every metrics
	// table, so a batch of this size finishes in well under a second
	// and never holds locks or a snapshot for long.
	DefaultConnectionPurgeBatchSize = 5000

	// DefaultConnectionPurgeInterval is how often the purge looks for
	// marked connections when nothing nudges it. The nudge from the
	// delete handler starts most purges; the ticker picks up the rest,
	// such as one interrupted by a failed batch.
	DefaultConnectionPurgeInterval = 5 * time.Minute

	// connectionPurgeBatchTimeout bounds one batch DELETE. The pool's
	// statement_timeout applies too; this also covers acquiring a
	// connection from a busy pool.
	connectionPurgeBatchTimeout = time.Minute
)

// listDeletedConnectionsQuery lists the connections marked for purge,
// oldest first.
const listDeletedConnectionsQuery = `
    SELECT id FROM connections
    WHERE deleted_at IS NOT NULL
    ORDER BY deleted_at, id
`

// connectionMarkedQuery reports whether a connection is still marked
// for purge. PurgeConnection checks it before deleting anything, so a
// live connection's metrics can never be purged by mistake.
const connectionMarkedQuery = `
    SELECT EXISTS (
        SELECT 1 FROM connections WHERE id = $1 AND deleted_at IS NOT NULL
    )
`

// metricsLeafTablesQuery lists the tables that hold rows in the metrics
// schema and carry a connection_id column: the partitions of every
// partitioned metrics table, and any unpartitioned one. Partitioned
// parents (relkind 'p') hold no rows and are skipped. Discovering the
// tables, rather than listing the foreign keys, also reaches the
// metrics tables that have no foreign key to connections at all.
const metricsLeafTablesQuery = `
    SELECT c.relname
    FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    JOIN pg_attribute a ON a.attrelid = c.oid
    WHERE n.nspname = 'metrics'
      AND c.relkind = 'r'
      AND a.attname = 'connection_id'
      AND NOT a.attisdropped
    ORDER BY c.relname
`

// connectionDependentsQuery lists the single-column foreign keys
// outside the metrics schema that cascade from a connections row. The
// mark step deletes those rows itself, rather than waiting for the
// final DELETE, because they are what makes a connection visible
// (alerts, blackouts, relationships, thresholds) and are small.
// Partition-inherited constraints (conparentid <> 0) are skipped, as
// their parent's constraint covers them.
const connectionDependentsQuery = `
    SELECT n.nspname, cl.relname, a.attname
    FROM pg_constraint con
    JOIN pg_class cl ON cl.oid = con.conrelid
    JOIN pg_namespace n ON n.oid = cl.relnamespace
    JOIN pg_attribute a ON a.attrelid = con.conrelid
                       AND a.attnum = con.conkey[1]
    WHERE con.contype = 'f'
      AND con.confrelid = 'connections'::regclass
      AND con.confdeltype = 'c'
      AND con.conparentid = 0
      AND array_length(con.conkey, 1) = 1
      AND n.nspname <> 'metrics'
    ORDER BY n.nspname, cl.relname, a.attname
`

// deleteConnectionDependents deletes, inside the mark transaction, the
// rows of every non-metrics table that cascades from connection id.
// Each table and column name comes from the catalog and is quoted as
// an identifier.
func deleteConnectionDependents(ctx context.Context, tx pgx.Tx, id int) error {
	rows, err := tx.Query(ctx, connectionDependentsQuery)
	if err != nil {
		return fmt.Errorf("failed to list connection dependents: %w", err)
	}
	type dependent struct{ schema, table, column string }
	deps, err := scanAll(rows, func(r pgx.Rows, d *dependent) error {
		return r.Scan(&d.schema, &d.table, &d.column)
	})
	if err != nil {
		return fmt.Errorf("failed to read connection dependents: %w", err)
	}

	for _, d := range deps {
		stmt := fmt.Sprintf("DELETE FROM %s WHERE %s = $1",
			pgx.Identifier{d.schema, d.table}.Sanitize(),
			pgx.Identifier{d.column}.Sanitize())
		if _, err := tx.Exec(ctx, stmt, id); err != nil {
			return fmt.Errorf("failed to delete %s.%s rows of the connection: %w",
				d.schema, d.table, err)
		}
	}
	return nil
}

// ListDeletedConnectionIDs returns the ids of the connections marked
// deleted and still waiting for their purge, oldest first.
func (d *Datastore) ListDeletedConnectionIDs(ctx context.Context) ([]int, error) {
	rows, err := d.pool.Query(ctx, listDeletedConnectionsQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to list deleted connections: %w", err)
	}
	ids, err := scanAll(rows, func(r pgx.Rows, id *int) error {
		return r.Scan(id)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to read deleted connections: %w", err)
	}
	return ids, nil
}

// PurgeConnection removes a deleted connection's metrics, batchSize
// rows per statement, and then the connection row itself. It does
// nothing for a connection that is not marked deleted. Each batch is
// its own short transaction, so the purge never holds one long
// transaction, and an interrupted purge resumes from what is left.
func (d *Datastore) PurgeConnection(ctx context.Context, id int, batchSize int) error {
	if batchSize <= 0 {
		return fmt.Errorf("purge batch size must be positive, got %d", batchSize)
	}

	var marked bool
	if err := d.pool.QueryRow(ctx, connectionMarkedQuery, id).Scan(&marked); err != nil {
		return fmt.Errorf("failed to check connection %d is marked deleted: %w", id, err)
	}
	if !marked {
		return nil
	}

	rows, err := d.pool.Query(ctx, metricsLeafTablesQuery)
	if err != nil {
		return fmt.Errorf("failed to list metrics tables: %w", err)
	}
	tables, err := scanAll(rows, func(r pgx.Rows, name *string) error {
		return r.Scan(name)
	})
	if err != nil {
		return fmt.Errorf("failed to read metrics tables: %w", err)
	}

	for _, table := range tables {
		if err := d.purgeMetricsTable(ctx, table, id, batchSize); err != nil {
			return err
		}
	}

	return d.removeDeletedConnection(ctx, id)
}

// purgeMetricsTable deletes connection id's rows from one metrics
// table, batchSize rows at a time, until a batch comes up short. The
// inner SELECT walks the primary key index; ctid = ANY(...) is then a
// TID scan, so each batch touches only the rows it deletes.
func (d *Datastore) purgeMetricsTable(ctx context.Context, table string, id int, batchSize int) error {
	ident := pgx.Identifier{"metrics", table}.Sanitize()
	stmt := fmt.Sprintf(`DELETE FROM %s WHERE ctid = ANY(ARRAY(
        SELECT ctid FROM %s WHERE connection_id = $1 LIMIT $2))`, ident, ident)

	for {
		batchCtx, cancel := context.WithTimeout(ctx, connectionPurgeBatchTimeout)
		tag, err := d.pool.Exec(batchCtx, stmt, id, batchSize)
		cancel()
		if err != nil {
			return fmt.Errorf("failed to purge metrics.%s rows of connection %d: %w",
				table, id, err)
		}
		if tag.RowsAffected() < int64(batchSize) {
			return nil
		}
	}
}

// removeDeletedConnection deletes the marked connection row once its
// metrics are gone. The DELETE still cascades, catching any row the
// collector or alerter wrote after the mark, and sets
// notification_history.connection_id to NULL. It runs under the same
// local statement_timeout as the mark step, as a backstop in case the
// cascade has more to do than expected.
func (d *Datastore) removeDeletedConnection(ctx context.Context, id int) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin purge of connection %d: %w", id, err)
	}
	defer rollback.Tx(ctx, tx) //nolint:errcheck // no-op after commit

	if _, err := tx.Exec(ctx,
		`SELECT set_config('statement_timeout', $1, true)`,
		strconv.FormatInt(ConnectionDeleteTimeout.Milliseconds(), 10),
	); err != nil {
		return fmt.Errorf("failed to set purge statement timeout: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM connections WHERE id = $1 AND deleted_at IS NOT NULL`, id,
	); err != nil {
		return fmt.Errorf("failed to remove deleted connection %d: %w", id, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit purge of connection %d: %w", id, err)
	}
	return nil
}

// connectionPurgeStore is the part of the Datastore the purger uses,
// so the scheduling loop can be tested without a database.
type connectionPurgeStore interface {
	ListDeletedConnectionIDs(ctx context.Context) ([]int, error)
	PurgeConnection(ctx context.Context, id int, batchSize int) error
}

// ConnectionPurger runs the second step of a connection delete in the
// background: it purges every connection marked deleted, at startup,
// whenever Nudge is called, and every interval in between.
type ConnectionPurger struct {
	store     connectionPurgeStore
	interval  time.Duration
	batchSize int
	nudge     chan struct{}
	logf      func(format string, args ...any)
}

// NewConnectionPurger returns a purger for the datastore's deleted
// connections, using the default interval and batch size.
func NewConnectionPurger(ds *Datastore) *ConnectionPurger {
	return newConnectionPurger(ds, DefaultConnectionPurgeInterval,
		DefaultConnectionPurgeBatchSize, log.Printf)
}

func newConnectionPurger(store connectionPurgeStore, interval time.Duration,
	batchSize int, logf func(string, ...any)) *ConnectionPurger {
	return &ConnectionPurger{
		store:     store,
		interval:  interval,
		batchSize: batchSize,
		// One buffered slot coalesces the nudges that arrive whilst a
		// pass is running into a single follow-up pass.
		nudge: make(chan struct{}, 1),
		logf:  logf,
	}
}

// Nudge asks the purger to run a pass soon. It never blocks; a nudge
// that arrives whilst one is already pending is dropped, because the
// pending pass will see the new connection too.
func (p *ConnectionPurger) Nudge() {
	select {
	case p.nudge <- struct{}{}:
	default:
	}
}

// Run purges marked connections until ctx is canceled: once straight
// away, then on every nudge and every interval.
func (p *ConnectionPurger) Run(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		p.RunOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-p.nudge:
		}
	}
}

// RunOnce purges every connection currently marked deleted. A failure
// on one connection is logged and the pass moves on to the next; the
// next pass retries it.
func (p *ConnectionPurger) RunOnce(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	ids, err := p.store.ListDeletedConnectionIDs(ctx)
	if err != nil {
		p.logf("[ERROR] Connection purge: %v", err)
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		if err := p.store.PurgeConnection(ctx, id, p.batchSize); err != nil {
			p.logf("[ERROR] Connection purge: %v", err)
		}
	}
}
