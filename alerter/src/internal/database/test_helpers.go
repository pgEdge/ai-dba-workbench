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
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewTestDatastore creates a Datastore wrapping a caller-owned pool.
// This is intended for use in tests where the caller manages the pool
// lifecycle directly. The caller must close the pool when done.
//
// This function exists because the Datastore fields are unexported,
// so tests in other packages cannot construct one directly.
func NewTestDatastore(pool *pgxpool.Pool) *Datastore {
	return &Datastore{pool: pool}
}
