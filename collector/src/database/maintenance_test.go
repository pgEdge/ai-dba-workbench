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
	"testing"
	"time"
)

// TestMaintenanceRunRoundTrip covers the persistence that decouples the
// partition dropper's schedule from process uptime (issue #437).
func TestMaintenanceRunRoundTrip(t *testing.T) {
	if err := setupTestDatabase(); err != nil {
		t.Skipf("test database not provisioned: %v", err)
	}

	pool, conn := getTestConnection(t)
	defer pool.Close()
	defer conn.Release()

	ctx := context.Background()

	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	const task = "test_partition_retention"
	if _, err := conn.Exec(ctx,
		"DELETE FROM maintenance_runs WHERE task = $1", task); err != nil {
		t.Fatalf("clearing prior state: %v", err)
	}

	// A task that has never run reports absence, not an error, because
	// that is the ordinary state of a freshly migrated datastore.
	if _, found, err := GetMaintenanceRun(ctx, conn, task); err != nil {
		t.Fatalf("GetMaintenanceRun on empty table: %v", err)
	} else if found {
		t.Fatal("GetMaintenanceRun reported a run that was never recorded")
	}

	first := time.Now().UTC().Add(-9 * 24 * time.Hour).Truncate(time.Second)
	if err := RecordMaintenanceRun(ctx, conn, task, first); err != nil {
		t.Fatalf("RecordMaintenanceRun: %v", err)
	}

	got, found, err := GetMaintenanceRun(ctx, conn, task)
	if err != nil || !found {
		t.Fatalf("GetMaintenanceRun after record: found=%v err=%v", found, err)
	}
	if !got.UTC().Equal(first) {
		t.Errorf("stored time = %v, want %v", got.UTC(), first)
	}

	// Recording again must update in place rather than conflict, so the
	// table holds one row per task however many times the collector runs.
	second := time.Now().UTC().Truncate(time.Second)
	if err := RecordMaintenanceRun(ctx, conn, task, second); err != nil {
		t.Fatalf("RecordMaintenanceRun (update): %v", err)
	}

	got, found, err = GetMaintenanceRun(ctx, conn, task)
	if err != nil || !found {
		t.Fatalf("GetMaintenanceRun after update: found=%v err=%v", found, err)
	}
	if !got.UTC().Equal(second) {
		t.Errorf("updated time = %v, want %v", got.UTC(), second)
	}

	var rows int
	if err := conn.QueryRow(ctx,
		"SELECT count(*) FROM maintenance_runs WHERE task = $1", task).
		Scan(&rows); err != nil {
		t.Fatalf("counting rows: %v", err)
	}
	if rows != 1 {
		t.Errorf("maintenance_runs holds %d rows for %q, want 1", rows, task)
	}

	if _, err := conn.Exec(ctx,
		"DELETE FROM maintenance_runs WHERE task = $1", task); err != nil {
		t.Logf("cleaning up %q: %v", task, err)
	}
}

// TestGetMaintenanceRunUnknownTask verifies that an unrecorded task is
// reported as absent rather than as an error, which is what lets the
// collector treat "never run" as "due now".
func TestGetMaintenanceRunUnknownTask(t *testing.T) {
	if err := setupTestDatabase(); err != nil {
		t.Skipf("test database not provisioned: %v", err)
	}

	pool, conn := getTestConnection(t)
	defer pool.Close()
	defer conn.Release()

	ctx := context.Background()

	sm := NewSchemaManager()
	if err := sm.Migrate(conn); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	at, found, err := GetMaintenanceRun(ctx, conn, "no_such_task_437")
	if err != nil {
		t.Fatalf("GetMaintenanceRun: %v", err)
	}
	if found {
		t.Error("found = true for a task that was never recorded")
	}
	if !at.IsZero() {
		t.Errorf("time = %v, want zero value", at)
	}
}

// TestGetMaintenanceRunReadFailure verifies that a genuine read failure
// is reported rather than being reported as "never run", since the two
// lead the collector to different decisions.
func TestGetMaintenanceRunReadFailure(t *testing.T) {
	if err := setupTestDatabase(); err != nil {
		t.Skipf("test database not provisioned: %v", err)
	}

	pool, conn := getTestConnection(t)
	defer pool.Close()
	defer conn.Release()

	ctx := context.Background()

	if _, err := conn.Exec(ctx, "DROP TABLE IF EXISTS maintenance_runs"); err != nil {
		t.Fatalf("dropping maintenance_runs: %v", err)
	}

	if _, _, err := GetMaintenanceRun(ctx, conn, PartitionRetentionTask); err == nil {
		t.Error("GetMaintenanceRun returned no error with the table missing")
	}

	if err := RecordMaintenanceRun(ctx, conn, PartitionRetentionTask, time.Now()); err == nil {
		t.Error("RecordMaintenanceRun returned no error with the table missing")
	}
}
