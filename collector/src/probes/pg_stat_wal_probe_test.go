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

func newPgStatWalProbeForTest() *PgStatWalProbe {
	return NewPgStatWalProbe(&ProbeConfig{
		Name:                      ProbeNamePgStatWAL,
		CollectionIntervalSeconds: 600,
		RetentionDays:             30,
		IsEnabled:                 true,
	})
}

func TestPgStatWalProbe_Surface(t *testing.T) {
	p := newPgStatWalProbeForTest()
	if p.GetName() != ProbeNamePgStatWAL {
		t.Errorf("GetName()")
	}
	if p.IsDatabaseScoped() {
		t.Error("pg_stat_wal is server-scoped")
	}
	if p.GetQuery() != "" {
		t.Error("GetQuery should be empty; query is built by Execute")
	}
}

func TestPgStatWalProbe_StoreEmpty(t *testing.T) {
	p := newPgStatWalProbeForTest()
	if err := p.Store(context.Background(), nil, 1, time.Now(),
		nil); err != nil {
		t.Errorf("Store(nil) = %v", err)
	}
}

func TestPgStatWalProbe_ExecuteAndStore(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatWalProbeForTest()
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	metrics, err := p.Execute(ctx, "wal-conn", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if pgVersion >= 14 && len(metrics) == 0 {
		t.Error("expected pg_stat_wal x archiver join row on PG14+")
	}
	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		metrics); err != nil {
		t.Fatalf("Store: %v", err)
	}
}
