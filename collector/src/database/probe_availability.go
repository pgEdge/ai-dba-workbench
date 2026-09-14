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
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/pkg/sqlmarker"
)

// probeAvailabilityUpsert records probe availability for a monitored
// connection. It runs once per probe per collection cycle against the
// Workbench's own datastore, so it is tagged as Workbench-internal to
// keep it out of the server's Top Queries panel when monitoring queries
// are hidden; see sqlmarker.Tag for why the marker sits immediately
// after the leading keyword.
var probeAvailabilityUpsert = sqlmarker.Tag(`
        INSERT INTO probe_availability
            (connection_id, database_name, probe_name, extension_name,
             is_available, last_checked, last_collected, unavailable_reason)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
        ON CONFLICT (connection_id, database_name, probe_name)
        DO UPDATE SET
            extension_name = EXCLUDED.extension_name,
            is_available = EXCLUDED.is_available,
            last_checked = EXCLUDED.last_checked,
            last_collected = COALESCE(EXCLUDED.last_collected, probe_availability.last_collected),
            unavailable_reason = EXCLUDED.unavailable_reason
    `)

// UpsertProbeAvailability records the availability status of a probe for
// a monitored connection. It inserts a new row or updates an existing one
// based on the unique constraint (connection_id, database_name, probe_name).
//
// Because the schema uses a plain UNIQUE constraint and NULL != NULL in
// SQL, this function coalesces a nil databaseName to an empty string so
// that ON CONFLICT matching works correctly for server-level probes.
func UpsertProbeAvailability(ctx context.Context, conn *pgxpool.Conn, connectionID int, databaseName *string, probeName string, extensionName *string, isAvailable bool, unavailableReason *string) error {
	now := time.Now().UTC()
	var lastCollected *time.Time
	if isAvailable {
		lastCollected = &now
	}

	// Convert nil database name to empty string to ensure the UNIQUE
	// constraint can detect conflicts for server-level probes.
	dbName := ""
	if databaseName != nil {
		dbName = *databaseName
	}

	_, err := conn.Exec(ctx, probeAvailabilityUpsert,
		connectionID, dbName, probeName, extensionName,
		isAvailable, now, lastCollected, unavailableReason)

	return err
}
