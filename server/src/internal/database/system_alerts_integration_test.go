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
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// systemAlertsV18SQL brings the unack test schema's alerts table to its
// collector migration v18 shape, in which a system alert (GitHub issue
// #582) has a NULL connection_id.
const systemAlertsV18SQL = `
ALTER TABLE alerts ALTER COLUMN connection_id DROP NOT NULL;
ALTER TABLE alerts DROP CONSTRAINT IF EXISTS alerts_alert_type_check;
ALTER TABLE alerts ADD CONSTRAINT alerts_alert_type_check
    CHECK (alert_type IN ('threshold', 'anomaly', 'connection', 'system'));
ALTER TABLE alerts ADD CONSTRAINT alerts_system_connection_check
    CHECK ((alert_type = 'system') = (connection_id IS NULL));
`

// systemAlertsFixture holds two connections with one active alert each
// and one active system alert.
type systemAlertsFixture struct {
	ds       *Datastore
	pool     *pgxpool.Pool
	connA    int
	connB    int
	systemID int64
	alertAID int64
}

func newSystemAlertsFixture(t *testing.T) *systemAlertsFixture {
	t.Helper()
	ds, pool, cleanup := newUnackAlertTestDatastore(t)
	t.Cleanup(cleanup)
	if _, err := pool.Exec(context.Background(), systemAlertsV18SQL); err != nil {
		t.Fatalf("apply v18 alerts shape: %v", err)
	}
	f := &systemAlertsFixture{
		ds:    ds,
		pool:  pool,
		connA: insertUnackTestConnection(t, pool, "conn-a"),
		connB: insertUnackTestConnection(t, pool, "conn-b"),
	}
	f.alertAID = f.insert(t, "threshold", &f.connA, "alert on a")
	f.insert(t, "threshold", &f.connB, "alert on b")
	f.systemID = f.insert(t, AlertTypeSystem, nil, "provider failing")
	return f
}

func (f *systemAlertsFixture) insert(t *testing.T, alertType string, connID *int, title string) int64 {
	t.Helper()
	var id int64
	if err := f.pool.QueryRow(context.Background(), `
		INSERT INTO alerts (alert_type, connection_id, severity, title, description, status)
		VALUES ($1, $2, 'warning', $3, 'test', 'active') RETURNING id`,
		alertType, connID, title).Scan(&id); err != nil {
		t.Fatalf("insert alert %q: %v", title, err)
	}
	return id
}

func alertTitles(result *AlertListResult) string {
	titles := make([]string, 0, len(result.Alerts))
	for _, a := range result.Alerts {
		titles = append(titles, a.Title)
	}
	return strings.Join(titles, ",")
}

// TestGetAlerts_SystemAlerts covers every combination of the connection
// filters with IncludeSystem and SystemOnly.
func TestGetAlerts_SystemAlerts(t *testing.T) {
	f := newSystemAlertsFixture(t)
	ctx := context.Background()

	tests := []struct {
		name   string
		filter AlertListFilter
		want   string
	}{
		{"no filter leaves system alerts out", AlertListFilter{}, "alert on b,alert on a"},
		{"include system", AlertListFilter{IncludeSystem: true}, "provider failing,alert on b,alert on a"},
		{"allow-list without system", AlertListFilter{ConnectionIDs: []int{f.connA}}, "alert on a"},
		{"allow-list with system", AlertListFilter{ConnectionIDs: []int{f.connA}, IncludeSystem: true},
			"provider failing,alert on a"},
		{"single connection", AlertListFilter{ConnectionID: &f.connB}, "alert on b"},
		{"system only ignores connection filters",
			AlertListFilter{ConnectionIDs: []int{f.connA}, SystemOnly: true}, "provider failing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := f.ds.GetAlerts(ctx, tt.filter)
			if err != nil {
				t.Fatalf("GetAlerts: %v", err)
			}
			if got := alertTitles(result); got != tt.want {
				t.Errorf("titles = %q, want %q", got, tt.want)
			}
			if result.Total != int64(len(result.Alerts)) {
				t.Errorf("total %d, rows %d", result.Total, len(result.Alerts))
			}
		})
	}

	result, err := f.ds.GetAlerts(ctx, AlertListFilter{SystemOnly: true})
	if err != nil {
		t.Fatalf("GetAlerts: %v", err)
	}
	if len(result.Alerts) != 1 {
		t.Fatalf("want one system alert, got %d", len(result.Alerts))
	}
	sys := result.Alerts[0]
	if sys.ConnectionID != nil || sys.ServerName != "" || sys.AlertType != AlertTypeSystem {
		t.Errorf("system alert = connection %v, server %q, type %q",
			sys.ConnectionID, sys.ServerName, sys.AlertType)
	}
}

// TestGetAlertCounts_SystemAlerts proves system alerts are counted in
// System and Total, never in ByServer, and only on request.
func TestGetAlertCounts_SystemAlerts(t *testing.T) {
	f := newSystemAlertsFixture(t)
	ctx := context.Background()

	tests := []struct {
		name          string
		ids           []int
		includeSystem bool
		wantTotal     int64
		wantSystem    int64
		wantServers   int
	}{
		{"every connection without system", nil, false, 2, 0, 2},
		{"every connection with system", nil, true, 3, 1, 2},
		{"allow-list with system", []int{f.connA}, true, 2, 1, 1},
		{"no connections with system", []int{}, true, 1, 1, 0},
		{"no connections without system", []int{}, false, 0, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counts, err := f.ds.GetAlertCounts(ctx, tt.ids, tt.includeSystem)
			if err != nil {
				t.Fatalf("GetAlertCounts: %v", err)
			}
			if counts.Total != tt.wantTotal || counts.System != tt.wantSystem || len(counts.ByServer) != tt.wantServers {
				t.Errorf("counts = %+v, want total %d, system %d, %d servers",
					counts, tt.wantTotal, tt.wantSystem, tt.wantServers)
			}
		})
	}
}

// TestGetAlertConnectionID_SystemAlert proves a system alert resolves to
// a nil connection and a connection alert to its connection.
func TestGetAlertConnectionID_SystemAlert(t *testing.T) {
	f := newSystemAlertsFixture(t)
	ctx := context.Background()

	got, err := f.ds.GetAlertConnectionID(ctx, f.systemID)
	if err != nil || got != nil {
		t.Errorf("system alert: got %v, %v; want nil, nil", got, err)
	}
	got, err = f.ds.GetAlertConnectionID(ctx, f.alertAID)
	if err != nil || got == nil || *got != f.connA {
		t.Errorf("connection alert: got %v, %v; want %d", got, err, f.connA)
	}
	if _, err := f.ds.GetAlertConnectionID(ctx, 999999); err == nil {
		t.Error("missing alert: want an error")
	}
}

// TestGetAlertCounts_QueryError covers the query failure path.
func TestGetAlertCounts_QueryError(t *testing.T) {
	ds, pool, cleanup := newUnackAlertTestDatastore(t)
	defer cleanup()
	if _, err := pool.Exec(context.Background(), `DROP TABLE alerts CASCADE`); err != nil {
		t.Fatalf("drop alerts: %v", err)
	}
	if _, err := ds.GetAlertCounts(context.Background(), nil, true); err == nil {
		t.Error("want an error when the alerts table is missing")
	}
}

// TestGetAlerts_SystemAlertFilters combines the remaining list filters
// with the system alert conditions.
func TestGetAlerts_SystemAlertFilters(t *testing.T) {
	f := newSystemAlertsFixture(t)
	ctx := context.Background()
	active, warning, system := "active", "warning", AlertTypeSystem
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)

	result, err := f.ds.GetAlerts(ctx, AlertListFilter{
		IncludeSystem:  true,
		Status:         &active,
		Severity:       &warning,
		AlertType:      &system,
		StartTime:      &past,
		EndTime:        &future,
		ExcludeCleared: true,
		Limit:          -1,
		Offset:         -1,
	})
	if err != nil {
		t.Fatalf("GetAlerts: %v", err)
	}
	if got := alertTitles(result); got != "provider failing" {
		t.Errorf("titles = %q", got)
	}
}

// TestGetAlerts_SystemAlertQueryErrors covers the count and list query
// failures.
func TestGetAlerts_SystemAlertQueryErrors(t *testing.T) {
	f := newSystemAlertsFixture(t)
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `ALTER TABLE alerts DROP COLUMN ai_analysis`); err != nil {
		t.Fatalf("drop ai_analysis: %v", err)
	}
	if _, err := f.ds.GetAlerts(ctx, AlertListFilter{IncludeSystem: true}); err == nil {
		t.Error("want a list query error")
	}
	if _, err := f.pool.Exec(ctx, `DROP TABLE alerts CASCADE`); err != nil {
		t.Fatalf("drop alerts: %v", err)
	}
	if _, err := f.ds.GetAlerts(ctx, AlertListFilter{IncludeSystem: true}); err == nil {
		t.Error("want a count query error")
	}
}

// TestAcknowledgeAlert_SystemAlertTwice proves a system alert can be
// acknowledged, and that a repeat reports it as already acknowledged.
func TestAcknowledgeAlert_SystemAlertTwice(t *testing.T) {
	f := newSystemAlertsFixture(t)
	req := AcknowledgeAlertRequest{AlertID: f.systemID, AcknowledgedBy: "Test User"}
	if err := f.ds.AcknowledgeAlert(context.Background(), req); err != nil {
		t.Fatalf("first acknowledge: %v", err)
	}
	if err := f.ds.AcknowledgeAlert(context.Background(), req); err == nil {
		t.Error("second acknowledge: want an error")
	}
}
