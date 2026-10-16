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

// Statements for the tests below, named for the same reason as the rest
// in this package: the Codacy/Semgrep go_sql_rule-concat-sqli rule flags
// inline multi-line SQL, and every value is still bound via $N.
const (
	unmonitorConnectionSQL = `
        UPDATE connections SET is_monitored = FALSE WHERE id = $1
    `

	retentionAlertInsertSQL = `
        INSERT INTO alerts
            (alert_type, connection_id, severity, title, description, status,
             triggered_at, cleared_at)
        VALUES ('threshold', $1, 'warning', $2, $2, $3, $4, $5)
    `

	retentionAlertTitlesSelectSQL = `
        SELECT title FROM alerts ORDER BY title
    `

	// Replacing the connections table with one whose id is text is the
	// only practical way to make the row scan fail rather than the query
	// itself, and the scan error is the branch that keeps a malformed row
	// from being reported as an empty set of unmonitored connections.
	mistypedConnectionsTableSQL = `
        DROP TABLE connections CASCADE;
        CREATE TABLE connections (
            id TEXT PRIMARY KEY,
            name VARCHAR(255) NOT NULL,
            is_monitored BOOLEAN NOT NULL DEFAULT TRUE
        );
        INSERT INTO connections (id, name, is_monitored)
        VALUES ('not-an-id', 'mistyped', FALSE)
    `
)

// TestGetUnmonitoredConnections covers the query the alert cleaner reads
// once per pass: it must name every connection an operator has stopped
// monitoring, and no other.
func TestGetUnmonitoredConnections(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	monitored := insertTestConnection(t, pool, "conn-monitored")
	unmonitored := insertTestConnection(t, pool, "conn-unmonitored")

	// With everything monitored the set is empty rather than an error.
	initial, err := ds.GetUnmonitoredConnections(ctx)
	if err != nil {
		t.Fatalf("GetUnmonitoredConnections: %v", err)
	}
	if len(initial) != 0 {
		t.Errorf("unmonitored set = %v, want it empty", initial)
	}

	if _, err := pool.Exec(ctx, unmonitorConnectionSQL, unmonitored); err != nil {
		t.Fatalf("failed to stop monitoring connection: %v", err)
	}

	got, err := ds.GetUnmonitoredConnections(ctx)
	if err != nil {
		t.Fatalf("GetUnmonitoredConnections: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("unmonitored set = %v, want exactly one entry", got)
	}
	if name := got[unmonitored]; name != "conn-unmonitored" {
		t.Errorf("unmonitored connection name = %q, want %q", name, "conn-unmonitored")
	}
	if _, found := got[monitored]; found {
		t.Errorf("monitored connection %d appeared in the unmonitored set", monitored)
	}
}

// TestDeleteOldAlertsReapsAcknowledged is the retention half of issue
// #500. AcknowledgeAlert sets the status and leaves cleared_at NULL, so
// keying the delete on cleared_at alone reaped no acknowledged alert
// however old it was; the age of a retired alert is now its clear time
// where it has one and its trigger time otherwise.
func TestDeleteOldAlertsReapsAcknowledged(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	connID := insertTestConnection(t, pool, "conn-retention")

	old := time.Now().Add(-48 * time.Hour)
	recent := time.Now()
	cutoff := time.Now().Add(-24 * time.Hour)

	cases := []struct {
		title       string
		status      string
		triggeredAt time.Time
		clearedAt   *time.Time
		survives    bool
	}{
		{"ack-old", "acknowledged", old, nil, false},
		{"ack-recent", "acknowledged", recent, nil, true},
		{"cleared-old", "cleared", old, &old, false},
		{"cleared-recent", "cleared", old, &recent, true},
		{"active-old", "active", old, nil, true},
	}

	for _, c := range cases {
		if _, err := pool.Exec(ctx, retentionAlertInsertSQL, connID, c.title,
			c.status, c.triggeredAt, c.clearedAt); err != nil {
			t.Fatalf("failed to insert alert %s: %v", c.title, err)
		}
	}

	deleted, err := ds.DeleteOldAlerts(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteOldAlerts: %v", err)
	}
	if deleted != 2 {
		t.Errorf("DeleteOldAlerts = %d, want 2", deleted)
	}

	rows, err := pool.Query(ctx, retentionAlertTitlesSelectSQL)
	if err != nil {
		t.Fatalf("failed to read remaining alerts: %v", err)
	}
	defer rows.Close()

	remaining := make(map[string]bool)
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			t.Fatalf("failed to scan alert title: %v", err)
		}
		remaining[title] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("row iteration error: %v", err)
	}

	for _, c := range cases {
		if remaining[c.title] != c.survives {
			t.Errorf("alert %q present = %v, want %v", c.title, remaining[c.title], c.survives)
		}
	}
}

// TestGetUnmonitoredConnectionsScanError covers the scan branch. A row
// the datastore cannot read must be reported as an error, because the
// alert cleaner treats an empty set as "everything is still monitored"
// and would otherwise carry on as though nothing had gone wrong.
func TestGetUnmonitoredConnectionsScanError(t *testing.T) {
	ds, pool, cleanup := newFullTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, mistypedConnectionsTableSQL); err != nil {
		t.Fatalf("failed to replace the connections table: %v", err)
	}

	if _, err := ds.GetUnmonitoredConnections(ctx); err == nil {
		t.Error("GetUnmonitoredConnections = nil error, want a scan failure")
	}
}
