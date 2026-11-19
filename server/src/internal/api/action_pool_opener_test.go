/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package api

import (
	"context"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/database"
)

// TestNewActionPoolOpener opens a pool through the action opener for a
// stored connection, and checks that a missing connection is an error.
func TestNewActionPoolOpener(t *testing.T) {
	_, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	connID := seedQueryExecConnection(t, pool, target, target.host, target.port)
	open := NewActionPoolOpener(
		database.NewTestDatastoreWithSecret(pool, queryExecTestSecret))
	ctx := context.Background()

	t.Run("default database", func(t *testing.T) {
		p, err := open(ctx, connID, nil)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer p.Close()
		var one int
		if err := p.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil || one != 1 {
			t.Fatalf("query on opened pool: %v (%d)", err, one)
		}
		// Actions write, so the session must not be read-only.
		var ro string
		if err := p.QueryRow(ctx, "SHOW transaction_read_only").Scan(&ro); err != nil {
			t.Fatalf("show: %v", err)
		}
		if ro != "off" {
			t.Errorf("transaction_read_only = %q, want off", ro)
		}
	})

	t.Run("named database", func(t *testing.T) {
		name := target.database
		p, err := open(ctx, connID, &name)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		p.Close()
	})

	t.Run("missing connection", func(t *testing.T) {
		if _, err := open(ctx, connID+100000, nil); err == nil {
			t.Error("expected an error for a missing connection")
		}
	})
}
