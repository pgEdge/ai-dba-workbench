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
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/pkg/hostvalidation"
)

// TestCreateMonitoredPool_RefusesAlwaysBlockedAtDial checks that the
// address check runs on what pgx dials: a target in an always-blocked
// range fails at name resolution, before any socket is opened, with an
// error that names no address, whilst a loopback target reaches the
// dial (and fails there, as nothing listens on port 1).
func TestCreateMonitoredPool_RefusesAlwaysBlockedAtDial(t *testing.T) {
	for _, host := range []string{"169.254.169.254", "0.0.0.0", "::", "fe80::1"} {
		connStr := "host=" + host + " port=5432 user=u dbname=x sslmode=disable connect_timeout=1"
		_, err := createMonitoredPool(context.Background(), connStr, 1, 0)
		if !errors.Is(err, hostvalidation.ErrNoPermittedAddress) {
			t.Errorf("host %s: err = %v, want ErrNoPermittedAddress", host, err)
		}
	}

	connStr := "host=127.0.0.1 port=1 user=u dbname=x sslmode=disable connect_timeout=1"
	_, err := createMonitoredPool(context.Background(), connStr, 1, 0)
	if err == nil {
		t.Fatal("expected a dial failure on port 1")
	}
	if errors.Is(err, hostvalidation.ErrNoPermittedAddress) {
		t.Errorf("loopback refused by the always-blocked floor: %v", err)
	}
	if strings.Contains(err.Error(), "not permitted") {
		t.Errorf("unexpected refusal text: %v", err)
	}
}
