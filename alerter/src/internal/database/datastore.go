/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
// Package database provides datastore access for the alerter.
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/pkg/connstring"
	"github.com/pgedge/ai-workbench/pkg/hostvalidation"
)

// Datastore provides access to the PostgreSQL datastore
type Datastore struct {
	pool   *pgxpool.Pool
	config *config.Config
}

// coldFrontDatastoreQuery reports whether the datastore stores metrics in
// pgEdge ColdFront: the coldfront extension is installed, or a relation
// in the metrics schema is a view, as every one is there (issue #755).
// It reads the catalog only. The extension is checked as well as the
// views because it is installed before the collector's migrations
// create them, so an alerter started first still detects ColdFront.
// The server's datastore pool runs the same check.
const coldFrontDatastoreQuery = `
    SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'coldfront')
        OR EXISTS (SELECT 1 FROM pg_class c
                   JOIN pg_namespace n ON n.oid = c.relnamespace
                   WHERE n.nspname = 'metrics' AND c.relkind = 'v')
`

// datastoreExecMode returns the query execution mode for the datastore
// pool. On plain PostgreSQL it is pgx's default, which caches a named
// prepared statement per query on each connection.
//
// On a pgEdge ColdFront datastore that cache is unsafe. ColdFront
// attaches its Iceberg catalog ("ice") inside the transaction that first
// plans a statement over a metrics view; when that transaction rolls
// back, as a statement timeout, cancellation or rollback.Tx does, the
// attach is undone, and executing the cached statement again on that
// pooled connection fails with 'Binder Error: Catalog "ice" does not
// exist!'. QueryExecModeDescribeExec never creates a named prepared
// statement: each execution parses the statement afresh on the unnamed
// statement, while parameter and result types still come from the
// server's description, as in the default mode. It costs one extra
// round trip per query.
func datastoreExecMode(coldFront bool) pgx.QueryExecMode {
	if coldFront {
		return pgx.QueryExecModeDescribeExec
	}
	return pgx.QueryExecModeCacheStatement
}

// detectColdFrontDatastore opens one connection with connConfig and
// reports whether the datastore is a pgEdge ColdFront one
// (coldFrontDatastoreQuery). The connection also proves the datastore
// reachable before the pool is built.
func detectColdFrontDatastore(ctx context.Context, connConfig *pgx.ConnConfig) (bool, error) {
	conn, err := pgx.ConnectConfig(ctx, connConfig.Copy())
	if err != nil {
		return false, fmt.Errorf("failed to connect to datastore: %w", err)
	}
	defer conn.Close(context.WithoutCancel(ctx)) //nolint:errcheck // best-effort close of a probe connection
	var coldFront bool
	if err := conn.QueryRow(ctx, coldFrontDatastoreQuery).Scan(&coldFront); err != nil {
		return false, fmt.Errorf("failed to detect the datastore kind: %w", err)
	}
	return coldFront, nil
}

// NewDatastore creates a new datastore connection
func NewDatastore(cfg *config.Config) (*Datastore, error) {
	connString := connstring.BuildFromConfig(cfg.Datastore, "pgEdge AI DBA Workbench - Alerter")

	poolConfig, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}

	// pgx would send hostaddr to the server as a startup parameter, which
	// PostgreSQL refuses; dial it in place of resolving the host instead,
	// keeping the host name for TLS verification.
	if lookup, ok, err := hostvalidation.TakeHostAddr(poolConfig.ConnConfig.RuntimeParams); ok {
		if err != nil {
			return nil, fmt.Errorf("invalid datastore hostaddr: %w", err)
		}
		poolConfig.ConnConfig.LookupFunc = lookup
	}

	// Configure pool settings
	maxConns := cfg.Pool.MaxConnections
	if maxConns > 0 && maxConns <= 32767 {
		poolConfig.MaxConns = int32(maxConns)
	}
	poolConfig.MaxConnIdleTime = time.Duration(cfg.Pool.MaxIdleSeconds) * time.Second

	// Create connection pool
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	coldFront, err := detectColdFrontDatastore(ctx, poolConfig.ConnConfig)
	if err != nil {
		return nil, err
	}
	poolConfig.ConnConfig.DefaultQueryExecMode = datastoreExecMode(coldFront)

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	return &Datastore{
		pool:   pool,
		config: cfg,
	}, nil
}

// Close closes the datastore connection pool
func (d *Datastore) Close() {
	if d.pool != nil {
		d.pool.Close()
	}
}
