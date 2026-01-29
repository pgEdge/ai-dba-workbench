/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Portions copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package database

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

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

	_, err := conn.Exec(ctx, `
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
    `, connectionID, dbName, probeName, extensionName,
		isAvailable, now, lastCollected, unavailableReason)

	return err
}
