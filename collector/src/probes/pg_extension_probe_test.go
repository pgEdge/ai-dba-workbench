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

func newPgExtensionProbeForTest() *PgExtensionProbe {
	return NewPgExtensionProbe(&ProbeConfig{
		Name:                      ProbeNamePgExtension,
		CollectionIntervalSeconds: 3600,
		RetentionDays:             30,
		IsEnabled:                 true,
	})
}

func TestPgExtensionProbe_Surface(t *testing.T) {
	p := newPgExtensionProbeForTest()
	if p.GetName() != ProbeNamePgExtension {
		t.Errorf("GetName() = %v", p.GetName())
	}
	if p.GetTableName() != ProbeNamePgExtension {
		t.Errorf("GetTableName() = %v", p.GetTableName())
	}
	if !p.IsDatabaseScoped() {
		t.Error("pg_extension is database-scoped")
	}
	q := p.GetQuery()
	for _, s := range []string{"extname", "extversion", "extrelocatable",
		"pg_extension", "pg_namespace"} {
		if !strings.Contains(q, s) {
			t.Errorf("GetQuery missing %q", s)
		}
	}
	if cfg := p.GetConfig(); cfg == nil ||
		cfg.RetentionDays != 30 {
		t.Errorf("GetConfig() = %+v", cfg)
	}
}

func TestPgExtensionProbe_StoreEmpty(t *testing.T) {
	p := newPgExtensionProbeForTest()
	if err := p.Store(context.Background(), nil, 1, time.Now(),
		nil); err != nil {
		t.Errorf("Store(nil) = %v", err)
	}
}

func TestPgExtensionProbe_ExecuteAndStore(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgExtensionProbeForTest()
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	metrics, err := p.Execute(ctx, "test-conn", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(metrics) == 0 {
		t.Fatal("expected at least plpgsql extension")
	}
	// Tag with database name as the scheduler would.
	for _, m := range metrics {
		m["_database_name"] = "testdb"
	}

	// First call: stores. Second call with identical data: no change.
	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		metrics); err != nil {
		t.Fatalf("Store first call: %v", err)
	}
	if err := p.Store(ctx, conn, 1, time.Now().UTC().Add(time.Minute),
		metrics); err != nil {
		t.Fatalf("Store unchanged data: %v", err)
	}
}
