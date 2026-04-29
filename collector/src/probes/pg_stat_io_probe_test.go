/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package probes

import (
	"context"
	"testing"
	"time"
)

func newPgStatIOProbeForTest() *PgStatIOProbe {
	return NewPgStatIOProbe(&ProbeConfig{
		Name:                      ProbeNamePgStatIO,
		CollectionIntervalSeconds: 900,
		RetentionDays:             30,
		IsEnabled:                 true,
	})
}

func TestPgStatIOProbe_Surface(t *testing.T) {
	p := newPgStatIOProbeForTest()
	if p.GetName() != ProbeNamePgStatIO {
		t.Errorf("GetName()")
	}
	if p.IsDatabaseScoped() {
		t.Error("pg_stat_io is server-scoped")
	}
	if p.GetQuery() != "" {
		t.Error("GetQuery should be empty; query is built by Execute")
	}
}

func TestPgStatIOProbe_StoreEmpty(t *testing.T) {
	p := newPgStatIOProbeForTest()
	if err := p.Store(context.Background(), nil, 1, time.Now(),
		nil); err != nil {
		t.Errorf("Store(nil) = %v", err)
	}
}

func TestPgStatIOProbe_ExecuteAndStore(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatIOProbeForTest()
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	metrics, err := p.Execute(ctx, "io-conn", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// On PG16+ pg_stat_io should return rows; on PG13 it may be empty.
	// We do not require non-empty.
	if metrics == nil && pgVersion >= 16 {
		t.Fatal("expected non-nil metrics on PG16+")
	}
	// Cached path on second call.
	if _, err := p.Execute(ctx, "io-conn", conn, pgVersion); err != nil {
		t.Fatalf("Execute cached: %v", err)
	}

	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		metrics); err != nil {
		t.Fatalf("Store: %v", err)
	}
}

func TestPgStatIOProbe_CheckHelpers(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatIOProbeForTest()
	ctx := context.Background()

	if _, err := p.checkIOViewExists(ctx, conn); err != nil {
		t.Errorf("checkIOViewExists: %v", err)
	}
	if _, err := p.checkSLRUViewExists(ctx, conn); err != nil {
		t.Errorf("checkSLRUViewExists: %v", err)
	}
}
