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

	"github.com/jackc/pgx/v5/pgxpool"
)

// insertWindowAlert inserts an alert on connID with the given trigger
// and clear times; a nil cleared leaves the alert open.
func insertWindowAlert(t *testing.T, pool *pgxpool.Pool, connID int, title string,
	triggered time.Time, cleared *time.Time) {
	t.Helper()
	status := "active"
	if cleared != nil {
		status = "cleared"
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO alerts (alert_type, connection_id, severity, title, description,
		                    status, triggered_at, cleared_at)
		VALUES ('threshold', $1, 'warning', $2, 'test', $3, $4, $5)`,
		connID, title, status, triggered, cleared); err != nil {
		t.Fatalf("insert alert %q: %v", title, err)
	}
}

// TestGetAlerts_ActiveWindow proves ActiveStart and ActiveEnd keep
// exactly the alerts that were active at some instant in the window,
// alone, together, and combined with pagination and other filters.
func TestGetAlerts_ActiveWindow(t *testing.T) {
	ds, pool, cleanup := newUnackAlertTestDatastore(t)
	t.Cleanup(cleanup)
	conn := insertUnackTestConnection(t, pool, "window-conn")
	ctx := context.Background()

	start := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	at := func(d time.Duration) time.Time { return start.Add(d) }
	ptr := func(t time.Time) *time.Time { return &t }

	// The listing orders by triggered_at descending, so each
	// expectation below lists titles in that order.
	insertWindowAlert(t, pool, conn, "after end", at(2*time.Hour), nil)
	insertWindowAlert(t, pool, conn, "inside", at(30*time.Minute), ptr(at(40*time.Minute)))
	insertWindowAlert(t, pool, conn, "triggered on end", end, nil)
	insertWindowAlert(t, pool, conn, "before; cleared inside", at(-time.Hour), ptr(at(10*time.Minute)))
	insertWindowAlert(t, pool, conn, "before; cleared on start", at(-90*time.Minute), ptr(start))
	insertWindowAlert(t, pool, conn, "before; still open", at(-2*time.Hour), nil)
	insertWindowAlert(t, pool, conn, "before; cleared before", at(-3*time.Hour), ptr(at(-time.Minute)))

	tests := []struct {
		name   string
		filter AlertListFilter
		want   string
		total  int64
	}{
		{
			name:   "both bounds",
			filter: AlertListFilter{ActiveStart: &start, ActiveEnd: &end},
			want: "triggered on end,inside,before; cleared inside," +
				"before; cleared on start,before; still open",
			total: 5,
		},
		{
			name:   "start only",
			filter: AlertListFilter{ActiveStart: &start},
			want: "after end,triggered on end,inside,before; cleared inside," +
				"before; cleared on start,before; still open",
			total: 6,
		},
		{
			name:   "end only",
			filter: AlertListFilter{ActiveEnd: &end},
			want: "triggered on end,inside,before; cleared inside," +
				"before; cleared on start,before; still open,before; cleared before",
			total: 6,
		},
		{
			name:   "paginated",
			filter: AlertListFilter{ActiveStart: &start, ActiveEnd: &end, Limit: 2, Offset: 2},
			want:   "before; cleared inside,before; cleared on start",
			total:  5,
		},
		{
			name: "with status filter",
			filter: AlertListFilter{ActiveStart: &start, ActiveEnd: &end,
				Status: strPtr("active")},
			want:  "triggered on end,before; still open",
			total: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.filter.ConnectionID = &conn
			result, err := ds.GetAlerts(ctx, tt.filter)
			if err != nil {
				t.Fatalf("GetAlerts: %v", err)
			}
			if got := alertTitles(result); got != tt.want {
				t.Errorf("alerts = %q, want %q", got, tt.want)
			}
			if result.Total != tt.total {
				t.Errorf("total = %d, want %d", result.Total, tt.total)
			}
		})
	}
}
