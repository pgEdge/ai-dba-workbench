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
	"net"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/pgedge/ai-workbench/pkg/hostvalidation"
)

// lookupAll resolves host through cfg's (guarded) LookupFunc.
func lookupAll(t *testing.T, cfg *pgconn.Config, host string) ([]string, error) {
	t.Helper()
	return cfg.LookupFunc(context.Background(), host)
}

// TestGuardMonitoredConnConfig checks the policy selection: a nil
// Datastore and one with no policy apply the always-blocked floor, and a
// set policy replaces it.
func TestGuardMonitoredConnConfig(t *testing.T) {
	floorCases := map[string]*Datastore{"nil datastore": nil, "no policy": {}}
	for name, ds := range floorCases {
		t.Run(name, func(t *testing.T) {
			cfg := &pgconn.Config{}
			ds.GuardMonitoredConnConfig(cfg)
			if _, err := lookupAll(t, cfg, "169.254.169.254"); !errors.Is(err, hostvalidation.ErrNoPermittedAddress) {
				t.Errorf("metadata address: err = %v, want ErrNoPermittedAddress", err)
			}
			if got, err := lookupAll(t, cfg, "127.0.0.1"); err != nil || len(got) != 1 {
				t.Errorf("loopback: got %v, %v; the floor must permit it", got, err)
			}
		})
	}

	ds := &Datastore{}
	ds.SetDialPolicy(func(_ string, ip net.IP) error {
		if ip.IsLoopback() {
			return errors.New("refused")
		}
		return nil
	})
	cfg := &pgconn.Config{LookupFunc: func(context.Context, string) ([]string, error) {
		return []string{"127.0.0.1", "192.0.2.10"}, nil
	}}
	ds.GuardMonitoredConnConfig(cfg)
	got, err := lookupAll(t, cfg, "db.example.com")
	if err != nil || len(got) != 1 || got[0] != "192.0.2.10" {
		t.Errorf("custom policy: got %v, %v; want only 192.0.2.10", got, err)
	}

	ds.SetDialPolicy(nil)
	cfg = &pgconn.Config{}
	ds.GuardMonitoredConnConfig(cfg)
	if _, err := lookupAll(t, cfg, "0.0.0.0"); !errors.Is(err, hostvalidation.ErrNoPermittedAddress) {
		t.Errorf("reset policy: err = %v, want the floor's refusal", err)
	}
}

// TestClientConnectTo_DialGuard checks that a client made for a
// monitored connection refuses an always-blocked target before connecting,
// and that the error names no address.
func TestClientConnectTo_DialGuard(t *testing.T) {
	cm := NewClientManager(nil)
	_, err := cm.GetOrCreateClientWithConnString("k",
		"postgres://u@169.254.169.254:5432/x?sslmode=disable&connect_timeout=1")
	if !errors.Is(err, hostvalidation.ErrNoPermittedAddress) {
		t.Fatalf("err = %v, want ErrNoPermittedAddress", err)
	}
	if strings.Contains(err.Error(), "169.254") {
		t.Errorf("error %q names the address", err)
	}

	// A guard set on the manager replaces the floor.
	var called bool
	cm.SetMonitoredDialGuard(func(cfg *pgconn.Config) {
		called = true
		GuardConnConfig(cfg, func(string, net.IP) error { return errors.New("no") })
	})
	_, err = cm.GetOrCreateClientWithConnString("k",
		"postgres://u@127.0.0.1:1/x?sslmode=disable&connect_timeout=1")
	if !called || !errors.Is(err, hostvalidation.ErrNoPermittedAddress) {
		t.Fatalf("called = %v, err = %v; want the manager's guard to refuse", called, err)
	}

	// A client with no guard, as for the datastore, is not filtered: it
	// reaches the dial and fails there.
	c := NewClient(nil)
	err = c.ConnectTo("postgres://u@0.0.0.0:1/x?sslmode=disable&connect_timeout=1")
	if err == nil {
		t.Fatal("expected the dial to fail")
	}
	if errors.Is(err, hostvalidation.ErrNoPermittedAddress) {
		t.Errorf("an unguarded client was filtered: %v", err)
	}
}

// TestListDatabases_DialPolicy checks that ListDatabases dials through
// the datastore's dial policy.
func TestListDatabases_DialPolicy(t *testing.T) {
	ds, pool, cleanup := newNullDescriptionTestDatastore(t)
	defer cleanup()

	id := insertListDatabasesTestConnection(t, ds, pool, "guarded", 0, "")
	ds.SetDialPolicy(func(string, net.IP) error { return errors.New("refused") })
	defer ds.SetDialPolicy(nil)

	databases, err := ds.ListDatabases(context.Background(), id)
	if !errors.Is(err, hostvalidation.ErrNoPermittedAddress) {
		t.Fatalf("ListDatabases err = %v, want ErrNoPermittedAddress", err)
	}
	if databases != nil {
		t.Errorf("databases = %v, want nil", databases)
	}
}
