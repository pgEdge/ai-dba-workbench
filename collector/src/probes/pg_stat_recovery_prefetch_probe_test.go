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

func newPgStatRecoveryPrefetchProbeForTest() *PgStatRecoveryPrefetchProbe {
	return NewPgStatRecoveryPrefetchProbe(&ProbeConfig{
		Name:                      ProbeNamePgStatRecoveryPrefetch,
		CollectionIntervalSeconds: 600,
		RetentionDays:             30,
		IsEnabled:                 true,
	})
}

func TestPgStatRecoveryPrefetchProbe_Surface(t *testing.T) {
	p := newPgStatRecoveryPrefetchProbeForTest()
	if p.GetName() != ProbeNamePgStatRecoveryPrefetch {
		t.Errorf("GetName()")
	}
	if p.IsDatabaseScoped() {
		t.Error("recovery_prefetch is server-scoped")
	}
	q := p.GetQuery()
	for _, s := range []string{"prefetch", "wal_distance", "io_depth",
		"pg_stat_recovery_prefetch"} {
		if !strings.Contains(q, s) {
			t.Errorf("GetQuery missing %q", s)
		}
	}
}

func TestPgStatRecoveryPrefetchProbe_StoreEmpty(t *testing.T) {
	p := newPgStatRecoveryPrefetchProbeForTest()
	if err := p.Store(context.Background(), nil, 1, time.Now(),
		nil); err != nil {
		t.Errorf("Store(nil) = %v", err)
	}
}

func TestPgStatRecoveryPrefetchProbe_ExecuteWithoutView(t *testing.T) {
	// On a non-recovery primary the pg_stat_recovery_prefetch view
	// exists (PG15+) but returns no rows. Either way, Execute returns
	// empty metrics without error and the cached check is exercised.
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatRecoveryPrefetchProbeForTest()
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	if _, err := p.Execute(ctx, "rp-conn", conn, pgVersion); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Cached path
	if _, err := p.Execute(ctx, "rp-conn", conn, pgVersion); err != nil {
		t.Fatalf("Execute cached: %v", err)
	}
}

func TestPgStatRecoveryPrefetchProbe_StoreSynthetic(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatRecoveryPrefetchProbeForTest()

	// Store path with at least one synthetic row exercises the partition
	// creation and INSERT logic when pg_stat_recovery_prefetch is empty
	// or unavailable on the source.
	rows := []map[string]any{
		{
			"stats_reset": time.Now().UTC(),
			"prefetch":    int64(0), "hit": int64(0),
			"skip_init": int64(0), "skip_new": int64(0),
			"skip_fpw": int64(0), "skip_rep": int64(0),
			"wal_distance":   int64(0),
			"block_distance": int64(0),
			"io_depth":       int64(0),
		},
	}
	if err := p.Store(context.Background(), conn, 1, time.Now().UTC(),
		rows); err != nil {
		t.Fatalf("Store synthetic: %v", err)
	}
}
