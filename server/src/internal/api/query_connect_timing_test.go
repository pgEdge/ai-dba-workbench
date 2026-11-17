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
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/pgedge/ai-workbench/server/internal/database"
)

// TestOpenQueryPoolHoldsFailedConnect checks that openQueryPool, which
// opens the connection for the query and validate endpoints, connects
// before returning and holds a refused connect to the floor, so that it
// takes as long to report as a filtered port would (issue #850). The
// timing is package state, so the test does not run in parallel.
func TestOpenQueryPoolHoldsFailedConnect(t *testing.T) {
	var (
		mu    sync.Mutex
		slept []time.Duration
	)
	restore := database.SetConnectTiming(database.ConnectTiming{
		Floor: database.MonitoredConnectTimeout,
		Now:   func() time.Time { return time.Unix(1_700_000_000, 0) },
		Sleep: func(_ context.Context, d time.Duration) {
			mu.Lock()
			defer mu.Unlock()
			slept = append(slept, d)
		},
	})
	defer restore()

	// Port 1 is tcpmux, which nothing on a test host listens on, so the
	// connect is refused at once. The guard lets the loopback address
	// through, so the failure comes from the dial itself.
	pool, err := openQueryPool(context.Background(),
		"postgres://nobody@127.0.0.1:1/none?sslmode=disable", "test",
		func(*pgconn.Config) {})
	if err == nil {
		pool.Close()
		t.Fatal("expected the connect to fail")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(slept) != 1 || slept[0] != database.MonitoredConnectTimeout {
		t.Errorf("slept %v, want one hold of %v", slept, database.MonitoredConnectTimeout)
	}
}
