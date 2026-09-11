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
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PartitionRetentionTask is the maintenance_runs key under which the
// partition dropper records its completions.
const PartitionRetentionTask = "partition_retention"

// GetMaintenanceRun returns when the named maintenance task last
// completed. The second return value is false when the task has never
// run, which is distinct from an error and is the expected state on a
// freshly migrated datastore.
func GetMaintenanceRun(ctx context.Context, conn *pgxpool.Conn, task string) (time.Time, bool, error) {
	var lastRun time.Time
	err := conn.QueryRow(ctx, `
        SELECT last_run_at FROM maintenance_runs WHERE task = $1
    `, task).Scan(&lastRun)

	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}

	return lastRun, true, nil
}

// RecordMaintenanceRun stamps the named maintenance task as having
// completed at the given time. Storing the completion in the datastore
// rather than in memory is what decouples the schedule from the
// collector's process uptime, so a restart resumes the existing cycle
// instead of starting a fresh one.
func RecordMaintenanceRun(ctx context.Context, conn *pgxpool.Conn, task string, at time.Time) error {
	_, err := conn.Exec(ctx, `
        INSERT INTO maintenance_runs (task, last_run_at)
        VALUES ($1, $2)
        ON CONFLICT (task) DO UPDATE SET last_run_at = EXCLUDED.last_run_at
    `, task, at.UTC())

	return err
}
