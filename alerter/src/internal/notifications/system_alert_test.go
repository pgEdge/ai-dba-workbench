/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package notifications

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// unreachableDSN names a loopback port nothing listens on, so a query
// fails at once without touching any real database.
const unreachableDSN = "postgres://postgres@127.0.0.1:1/none?connect_timeout=1"

func TestConnectionInfo_SystemAlert(t *testing.T) {
	orig := hostname
	t.Cleanup(func() { hostname = orig })

	m := &Manager{}
	alert := &database.Alert{AlertType: database.AlertTypeSystem}

	tests := []struct {
		name     string
		hostname func() (string, error)
		wantHost string
	}{
		{"hostname known", func() (string, error) { return "alerter-1", nil }, "alerter-1"},
		{"hostname fails", func() (string, error) { return "", errors.New("no") }, "localhost"},
		{"hostname empty", func() (string, error) { return "", nil }, "localhost"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hostname = tt.hostname
			name, host, port, err := m.connectionInfo(context.Background(), alert)
			if err != nil || name != SystemAlertServerName || host != tt.wantHost || port != 0 {
				t.Errorf("got %q %q %d %v", name, host, port, err)
			}
		})
	}
}

func TestConnectionInfo_ConnectionAlertLooksUpConnection(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), unreachableDSN)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	m := &Manager{datastore: database.NewTestDatastore(pool)}
	alert := &database.Alert{AlertType: "threshold", ConnectionID: 3}
	if _, _, _, err := m.connectionInfo(context.Background(), alert); err == nil {
		t.Error("a connection alert did not consult the datastore")
	}
}

func TestHistoryConnectionID(t *testing.T) {
	if got := historyConnectionID(&database.Alert{AlertType: database.AlertTypeSystem}); got != nil {
		t.Errorf("system alert history connection = %d, want nil", *got)
	}
	if got := historyConnectionID(&database.Alert{AlertType: "anomaly", ConnectionID: 5}); got == nil || *got != 5 {
		t.Errorf("anomaly alert history connection = %v, want 5", got)
	}
}
