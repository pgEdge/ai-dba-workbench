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

import "github.com/jackc/pgx/v5/pgxpool"

// NewTestClient creates a database client for testing with mock data
// This allows tests in other packages to create clients with predetermined metadata
func NewTestClient(connStr string, metadata map[string]TableInfo) *Client {
	client := NewClient(nil)

	// Add mock connection info
	client.connections[connStr] = &ConnectionInfo{
		ConnString:     connStr,
		Pool:           nil, // No actual connection pool needed for tests
		Metadata:       metadata,
		MetadataLoaded: true,
	}

	// Set as default connection
	client.defaultConnStr = connStr

	return client
}

// NewTestDatastore wraps an existing pgxpool.Pool in a *Datastore for use
// by integration tests in other packages. The caller owns the pool and is
// responsible for closing it; the returned Datastore's Close method is a
// no-op on the pool it was given (i.e. callers must Close the pool
// themselves when they are done).
//
// This helper exists only to keep Datastore's fields unexported while
// allowing handler-level integration tests to exercise the full datastore
// code path without duplicating NewDatastore's config/pool-building
// logic.
func NewTestDatastore(pool *pgxpool.Pool) *Datastore {
	return &Datastore{
		pool:         pool,
		serverSecret: "",
	}
}
