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
	"errors"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Sentinel errors for datastore operations
var (
	// ErrConnectionNotFound is returned when a connection is not found
	ErrConnectionNotFound = errors.New("connection not found")

	// ErrUnsafeConnectionTarget is returned by BuildConnectionString when
	// a stored connection's host or hostaddr could reshape the
	// connection string (a list separator, stray brackets or a
	// non-IP hostaddr), so the row is refused rather than connected to.
	ErrUnsafeConnectionTarget = errors.New("stored connection host or hostaddr is not valid")
)

// Datastore manages the connection to the collector's datastore database
type Datastore struct {
	pool         *pgxpool.Pool
	serverSecret string
	mu           sync.RWMutex

	// isProbeView decides which metrics views the connection purge
	// deletes through (purgeableViews). Nil selects
	// metrics.IsKnownProbe; tests set it to admit their fixture views.
	isProbeView func(string) bool

	// sslCertDir is connection_security.ssl_cert_dir: the directory
	// stored SSL file paths must lie in. Empty refuses every such path.
	// It is guarded by mu; see SetSSLCertDir.
	sslCertDir string
}
