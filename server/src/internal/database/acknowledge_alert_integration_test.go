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

	"github.com/jackc/pgx/v5/pgxpool"
)

// insertActiveTestAlert inserts an active alert and returns its ID.
func insertActiveTestAlert(t *testing.T, pool *pgxpool.Pool, connID int, title string) int64 {
	t.Helper()

	var alertID int64
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO alerts (
			alert_type, connection_id, severity, title, description,
			status, triggered_at
		) VALUES (
			'threshold', $1, 'warning', $2, 'description',
			'active', CURRENT_TIMESTAMP
		) RETURNING id
	`, connID, title).Scan(&alertID); err != nil {
		t.Fatalf("Failed to insert active alert: %v", err)
	}
	return alertID
}

// TestAcknowledgeAlert_MarksAcknowledgedAndRecordsAck covers the
// AcknowledgeAlert happy path: the alert flips to acknowledged and a
// matching alert_acknowledgments row is written in the same
// transaction.
func TestAcknowledgeAlert_MarksAcknowledgedAndRecordsAck(t *testing.T) {
	ds, pool, cleanup := newUnackAlertTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertUnackTestConnection(t, pool, "ack-happy-path")
	alertID := insertActiveTestAlert(t, pool, connID, "ack me")

	err := ds.AcknowledgeAlert(ctx, AcknowledgeAlertRequest{
		AlertID:        alertID,
		AcknowledgedBy: "tester",
		Message:        "looking into it",
		FalsePositive:  true,
	})
	if err != nil {
		t.Fatalf("AcknowledgeAlert returned error: %v", err)
	}

	var status string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM alerts WHERE id = $1`, alertID).Scan(&status); err != nil {
		t.Fatalf("Failed to read alert status: %v", err)
	}
	if status != "acknowledged" {
		t.Errorf("status after acknowledge = %q, want \"acknowledged\"", status)
	}

	var by, message string
	var falsePositive bool
	if err := pool.QueryRow(ctx, `
		SELECT acknowledged_by, message, false_positive
		FROM alert_acknowledgments WHERE alert_id = $1
	`, alertID).Scan(&by, &message, &falsePositive); err != nil {
		t.Fatalf("Failed to read acknowledgment row: %v", err)
	}
	if by != "tester" || message != "looking into it" || !falsePositive {
		t.Errorf("acknowledgment row = (%q, %q, %v), want (\"tester\", "+
			"\"looking into it\", true)", by, message, falsePositive)
	}
}

// TestAcknowledgeAlert_AlreadyAcknowledgedErrors covers the zero-rows
// branch, where the alert is missing or is no longer active. The
// transaction must roll back without writing an acknowledgment row.
func TestAcknowledgeAlert_AlreadyAcknowledgedErrors(t *testing.T) {
	ds, pool, cleanup := newUnackAlertTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertUnackTestConnection(t, pool, "ack-twice")
	alertID := insertUnackAcknowledgedAlert(t, pool, connID)

	err := ds.AcknowledgeAlert(ctx, AcknowledgeAlertRequest{
		AlertID:        alertID,
		AcknowledgedBy: "tester",
	})
	if err == nil {
		t.Fatal("AcknowledgeAlert on an acknowledged alert returned nil; want error")
	}

	var ackCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM alert_acknowledgments WHERE alert_id = $1`,
		alertID).Scan(&ackCount); err != nil {
		t.Fatalf("Failed to count ack rows: %v", err)
	}
	if ackCount != 1 {
		t.Errorf("ack rows = %d, want the single pre-existing row", ackCount)
	}
}

// TestAcknowledgeAlert_UpdateFailureRollsBack covers the failed-UPDATE
// branch. Dropping the alerts table makes the first statement in the
// transaction fail, so the function must return a wrapped error rather
// than panicking.
func TestAcknowledgeAlert_UpdateFailureRollsBack(t *testing.T) {
	ds, pool, cleanup := newUnackAlertTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE alerts CASCADE`); err != nil {
		t.Fatalf("Failed to drop alerts table: %v", err)
	}

	err := ds.AcknowledgeAlert(ctx, AcknowledgeAlertRequest{
		AlertID:        1,
		AcknowledgedBy: "tester",
	})
	if err == nil {
		t.Fatal("AcknowledgeAlert with no alerts table returned nil; want error")
	}
}

// TestAcknowledgeAlert_AckInsertFailureRollsBack covers the failed
// INSERT branch, where the status update succeeds but the
// acknowledgment record cannot be written. The rollback must undo the
// status change, which is the atomicity guarantee the transaction
// exists for.
func TestAcknowledgeAlert_AckInsertFailureRollsBack(t *testing.T) {
	ds, pool, cleanup := newUnackAlertTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertUnackTestConnection(t, pool, "ack-insert-fails")
	alertID := insertActiveTestAlert(t, pool, connID, "insert will fail")

	if _, err := pool.Exec(ctx, `DROP TABLE alert_acknowledgments CASCADE`); err != nil {
		t.Fatalf("Failed to drop alert_acknowledgments table: %v", err)
	}

	err := ds.AcknowledgeAlert(ctx, AcknowledgeAlertRequest{
		AlertID:        alertID,
		AcknowledgedBy: "tester",
	})
	if err == nil {
		t.Fatal("AcknowledgeAlert with no acknowledgments table returned nil; want error")
	}

	var status string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM alerts WHERE id = $1`, alertID).Scan(&status); err != nil {
		t.Fatalf("Failed to read alert status: %v", err)
	}
	if status != "active" {
		t.Errorf("status after failed acknowledge = %q, want \"active\": the "+
			"rollback must undo the status update", status)
	}
}

// TestUnacknowledgeAlert_AckDeleteFailureRollsBack covers the
// clear-acknowledgments failure branch of UnacknowledgeAlert. The status
// update succeeds first, so the rollback must undo it and leave the
// alert acknowledged.
func TestUnacknowledgeAlert_AckDeleteFailureRollsBack(t *testing.T) {
	ds, pool, cleanup := newUnackAlertTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertUnackTestConnection(t, pool, "unack-delete-fails")
	alertID := insertUnackAcknowledgedAlert(t, pool, connID)

	if _, err := pool.Exec(ctx, `DROP TABLE alert_acknowledgments CASCADE`); err != nil {
		t.Fatalf("Failed to drop alert_acknowledgments table: %v", err)
	}

	if err := ds.UnacknowledgeAlert(ctx, alertID); err == nil {
		t.Fatal("UnacknowledgeAlert without an acknowledgments table returned nil; want error")
	}

	var status string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM alerts WHERE id = $1`, alertID).Scan(&status); err != nil {
		t.Fatalf("Failed to read alert status: %v", err)
	}
	if status != "acknowledged" {
		t.Errorf("status after failed unacknowledge = %q, want \"acknowledged\": "+
			"the rollback must undo the status update", status)
	}
}

// TestAcknowledgeAlert_ClosedPoolReturnsWrappedError covers the
// begin-transaction failure branch.
func TestAcknowledgeAlert_ClosedPoolReturnsWrappedError(t *testing.T) {
	ds, pool, cleanup := newUnackAlertTestDatastore(t)
	if _, err := pool.Exec(context.Background(), unackAlertTestTeardown); err != nil {
		t.Fatalf("teardown: %v", err)
	}
	pool.Close()
	_ = cleanup

	err := ds.AcknowledgeAlert(context.Background(), AcknowledgeAlertRequest{
		AlertID:        1,
		AcknowledgedBy: "tester",
	})
	if err == nil {
		t.Fatal("AcknowledgeAlert against a closed pool returned nil; want error")
	}
}
