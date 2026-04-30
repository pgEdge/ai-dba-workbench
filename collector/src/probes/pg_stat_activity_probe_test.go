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
	"strings"
	"testing"
	"time"
)

func newPgStatActivityProbeForTest() *PgStatActivityProbe {
	return NewPgStatActivityProbe(&ProbeConfig{
		Name:                      ProbeNamePgStatActivity,
		CollectionIntervalSeconds: 60,
		RetentionDays:             7,
		IsEnabled:                 true,
	})
}

func TestPgStatActivityProbe_Surface(t *testing.T) {
	p := newPgStatActivityProbeForTest()
	if p.GetName() != ProbeNamePgStatActivity {
		t.Errorf("GetName()")
	}
	if p.GetTableName() != ProbeNamePgStatActivity {
		t.Errorf("GetTableName()")
	}
	if p.IsDatabaseScoped() {
		t.Error("pg_stat_activity is server-scoped")
	}
	q := p.GetQuery()
	for _, s := range []string{"pid", "datname", "query", "backend_type",
		"pg_stat_activity"} {
		if !strings.Contains(q, s) {
			t.Errorf("GetQuery missing %q", s)
		}
	}
}

func TestPgStatActivityProbe_StoreEmpty(t *testing.T) {
	p := newPgStatActivityProbeForTest()
	if err := p.Store(context.Background(), nil, 1, time.Now(),
		nil); err != nil {
		t.Errorf("Store(nil) = %v", err)
	}
}

func TestPgStatActivityProbe_ExecuteAndStore(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatActivityProbeForTest()
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	metrics, err := p.Execute(ctx, "activity-conn", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// At least the wal-writer / autovacuum / etc backends should exist.
	if metrics == nil {
		t.Fatal("Execute returned nil metrics slice")
	}

	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		metrics); err != nil {
		t.Fatalf("Store: %v", err)
	}
}
