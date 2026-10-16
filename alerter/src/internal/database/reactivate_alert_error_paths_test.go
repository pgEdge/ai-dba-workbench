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
)

// TestReactivateAlert_UpdateFailureReturnsError covers the failed-UPDATE
// branch of ReactivateAlert, reached when the alerts table is absent. The
// deferred rollback must unwind the transaction without panicking.
func TestReactivateAlert_UpdateFailureReturnsError(t *testing.T) {
	ds, pool, cleanup := newReactivateAlertTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE alerts CASCADE`); err != nil {
		t.Fatalf("Failed to drop alerts table: %v", err)
	}

	if err := ds.ReactivateAlert(ctx, 1); err == nil {
		t.Fatal("ReactivateAlert without an alerts table returned nil; want error")
	}
}

// TestReactivateAlert_AckDeleteFailureRollsBack covers the failed-DELETE
// branch, where the status update succeeds but the acknowledgment rows
// cannot be cleared. The rollback must restore the acknowledged status so
// the alert is not left half-reactivated.
func TestReactivateAlert_AckDeleteFailureRollsBack(t *testing.T) {
	ds, pool, cleanup := newReactivateAlertTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertReactivateTestConnection(t, pool, "reactivate-delete-fails")
	alertID := insertAcknowledgedAlert(t, pool, connID)

	if _, err := pool.Exec(ctx, `DROP TABLE alert_acknowledgments CASCADE`); err != nil {
		t.Fatalf("Failed to drop alert_acknowledgments table: %v", err)
	}

	if err := ds.ReactivateAlert(ctx, alertID); err == nil {
		t.Fatal("ReactivateAlert without an acknowledgments table returned nil; want error")
	}

	var status string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM alerts WHERE id = $1`, alertID).Scan(&status); err != nil {
		t.Fatalf("Failed to read alert status: %v", err)
	}
	if status != "acknowledged" {
		t.Errorf("status after failed reactivation = %q, want \"acknowledged\": "+
			"the rollback must undo the status update", status)
	}
}
