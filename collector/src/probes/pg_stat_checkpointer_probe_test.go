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

func newPgStatCheckpointerProbeForTest() *PgStatCheckpointerProbe {
	return NewPgStatCheckpointerProbe(&ProbeConfig{
		Name:                      ProbeNamePgStatCheckpointer,
		CollectionIntervalSeconds: 600,
		RetentionDays:             30,
		IsEnabled:                 true,
	})
}

func TestPgStatCheckpointerProbe_Surface(t *testing.T) {
	p := newPgStatCheckpointerProbeForTest()
	if p.GetName() != ProbeNamePgStatCheckpointer {
		t.Errorf("GetName()")
	}
	if p.IsDatabaseScoped() {
		t.Error("checkpointer is server-scoped")
	}
	if p.GetQuery() != "" {
		t.Error("GetQuery should be empty; query is built by Execute")
	}
}

func TestPgStatCheckpointerProbe_StoreEmpty(t *testing.T) {
	p := newPgStatCheckpointerProbeForTest()
	if err := p.Store(context.Background(), nil, 1, time.Now(),
		nil); err != nil {
		t.Errorf("Store(nil) = %v", err)
	}
}

func TestPgStatCheckpointerProbe_ExecuteAndStore(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatCheckpointerProbeForTest()
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	metrics, err := p.Execute(ctx, "ckpt-conn", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(metrics) == 0 {
		t.Fatal("expected at least one row from checkpointer/bgwriter")
	}
	// Cached path on second call
	if _, err := p.Execute(ctx, "ckpt-conn", conn, pgVersion); err != nil {
		t.Fatalf("Execute cached: %v", err)
	}

	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		metrics); err != nil {
		t.Fatalf("Store: %v", err)
	}
}
